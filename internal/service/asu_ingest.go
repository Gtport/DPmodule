package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
)

// Ошибки автозагрузки АСУ (хендлер маппит их в HTTP-коды).
var (
	ErrNoASUSource    = errors.New("источник АСУ (api_pull) не настроен или выключен")
	ErrSourceSkew     = errors.New("рассогласование меток формирования источников АСУ")
	ErrNoFormationTS  = errors.New("источник АСУ не вернул метку формирования")
	ErrSourceNotNewer = errors.New("данные АСУ не обновились — метка формирования не новее предыдущего обновления")
)

// ASUClientFactory строит транспорт под конкретный источник (у каждого свой base_url/
// auth). Инъекция фабрики, а не готового клиента, держит service вне зависимости от
// HTTP-адаптера и допускает несколько api_pull-источников с разными URL.
type ASUClientFactory func(cfg domain.DataSourceConfig) port.ASUClient

// ASUIngest — автозагрузка дислокации из АСУ-АСУ (ingest=api_pull). За один проход
// тянет всех клиентов включённых источников category=dislocation/ingest=api_pull,
// сверяет метки формирования (гард рассогласования, порог MaxSourceSkewMinutes) и,
// если срез согласован, склеивает записи и отдаёт их общему конвейеру
// LKProcessor.ProcessRecords (Stage 1–4 + атомарная подмена снимка). Полный снимок,
// как ЛК; отдельного слияния-дельты нет. Триггер журнала — scheduled.
type ASUIngest struct {
	cfg     *ConfigCache
	newCl   ASUClientFactory
	parser  *parser.JSONParser
	proc    *LKProcessor
	journal *Journal
	log     *zap.Logger
}

func NewASUIngest(cfg *ConfigCache, factory ASUClientFactory, proc *LKProcessor, log *zap.Logger) *ASUIngest {
	return &ASUIngest{cfg: cfg, newCl: factory, parser: parser.NewJSONParser(), proc: proc, log: log}
}

// SetJournal подключает журнал событий (nil-safe: без него запись пропускается).
func (a *ASUIngest) SetJournal(j *Journal) { a.journal = j }

// pulledSource — результат забора одного клиента провайдера.
type pulledSource struct {
	label string // "<источник>/<клиент>" для логов
	ts    *domain.LocalTime
	count int
}

// Pull — один проход автозагрузки: забор всех клиентов → гарды → общий конвейер
// обработки → журнал. trigger — кто запустил (domain.TriggerManual — кнопка,
// TriggerScheduled — крон); пишется в журнал. Любой отказ (гард, сбой забора/
// разбора/обработки) фиксируется событием disl_rejected с кодом гарда и причиной
// (какой поток некачественный) — снимок при этом НЕ трогается.
func (a *ASUIngest) Pull(ctx context.Context, trigger string) (LKProcessResult, error) {
	sources := a.apiPullSources()
	if len(sources) == 0 {
		return LKProcessResult{}, ErrNoASUSource
	}

	all := make([]domain.Dislocation, 0, 4096)
	files := make([]LKFileInfo, 0, len(sources))
	perFile := make(map[string]int, len(sources))
	pulled := make([]pulledSource, 0, len(sources))

	// reject фиксирует отклонённую попытку в журнале и возвращает ошибку вызывающему.
	reject := func(guard string, err error) (LKProcessResult, error) {
		a.journal.RecordDislRejected(ctx, "asu", trigger, files, guard, err.Error())
		return LKProcessResult{}, err
	}

	for _, ds := range sources {
		if len(ds.Config.Clients) == 0 {
			return LKProcessResult{}, fmt.Errorf("%w: источник %q без списка клиентов", ErrNoASUSource, ds.ID)
		}
		cl := a.newCl(ds.Config)
		for _, code := range ds.Config.Clients {
			raw, err := cl.Pull(ctx, code)
			if err != nil {
				return reject("fetch", fmt.Errorf("забор АСУ %s/%s: %w", ds.ID, code, err))
			}
			res, err := a.parser.Parse(raw)
			if err != nil {
				return reject("parse", fmt.Errorf("разбор АСУ %s/%s: %w", ds.ID, code, err))
			}
			// Контроль целостности: заявленный count против фактического (только предупреждение).
			if res.DeclaredCount != nil && *res.DeclaredCount != len(res.Records) {
				a.log.Warn("АСУ: count в теле не совпал с числом вагонов",
					zap.String("client", code), zap.Int("declared", *res.DeclaredCount), zap.Int("got", len(res.Records)))
			}

			all = append(all, res.Records...)
			perFile[code] = len(res.Records)
			files = append(files, a.fileInfo(code, res.FormationTS))
			pulled = append(pulled, pulledSource{label: ds.ID + "/" + code, ts: res.FormationTS, count: len(res.Records)})
		}
	}

	// Гард рассогласования меток формирования между источниками (то же правило, что
	// «совместный срез» у ЛК, но отдельный порог MaxSourceSkewMinutes).
	skew := a.cfg.Settings().IngestPolicy.Dislocation.MaxSourceSkewMinutes
	if err := a.checkSkew(pulled, skew); err != nil {
		guard := "skew"
		if errors.Is(err, ErrNoFormationTS) {
			guard = "no_formation_ts"
		}
		return reject(guard, err)
	}

	// Гарды свежести — та же политика, что у ЛК (max_staleness_minutes,
	// reject_older_than_current): АСУ при обрыве связи с РЖД продолжает отдавать
	// один и тот же устаревший срез — им снимок не пересобираем.
	if err := a.proc.guardFreshness(ctx, files); err != nil {
		guard := "stale"
		if errors.Is(err, ErrDislOlderThanCurrent) {
			guard = "older"
		}
		return reject(guard, err)
	}
	// Гард «данные не обновились»: у КАЖДОГО потока метка формирования должна быть
	// НОВЕЕ, чем в предыдущем обновлении; хотя бы один поток с той же (или более
	// старой) меткой → снимок не трогаем (перезапись теми же данными бессмысленна
	// и маскирует обрыв данных). Сравнение по потокам: единая worst-метка (doc_ts)
	// не ловит отставание одного потока при свежем другом.
	if err := a.checkNotNewer(ctx, files); err != nil {
		return reject("not_newer", err)
	}

	res, err := a.proc.ProcessRecords(ctx, all, len(files), perFile)
	if err != nil {
		return reject("process", err) // в т.ч. порог потери данных (max_data_loss_pct)
	}
	a.journal.RecordDislUpdate(ctx, "asu", trigger, files, res.Count)
	return res, nil
}

// checkSkew блокирует обработку, если метки формирования источников разъехались
// больше порога. Порог ≤ 0 или < 2 источников → гард не применяется. Отсутствующая
// метка при включённом гарде трактуется как невозможность сверить срез → стоп.
func (a *ASUIngest) checkSkew(pulled []pulledSource, limit int) error {
	if limit <= 0 || len(pulled) < 2 {
		return nil
	}
	var lo, hi domain.LocalTime
	first := true
	for _, p := range pulled {
		if p.ts == nil || p.ts.IsZero() {
			a.log.Warn("АСУ: источник без метки формирования — обработка прекращена", zap.String("source", p.label))
			return fmt.Errorf("%w: %s", ErrNoFormationTS, p.label)
		}
		t := *p.ts
		switch {
		case first:
			lo, hi, first = t, t, false
		case t.Time().Before(lo.Time()):
			lo = t
		case t.Time().After(hi.Time()):
			hi = t
		}
	}
	gap := int(hi.Time().Sub(lo.Time()).Minutes())
	if gap > limit {
		a.log.Warn("АСУ: рассогласование меток формирования — обработка прекращена",
			zap.Int("gap_min", gap), zap.Int("limit_min", limit),
			zap.String("oldest", lo.String()), zap.String("newest", hi.String()))
		return fmt.Errorf("%w: %d мин > %d", ErrSourceSkew, gap, limit)
	}
	return nil
}

// checkNotNewer сверяет метки формирования потоков с предыдущим обновлением
// дислокации (журнал): каждый уже известный поток обязан принести метку строго
// новее прежней. Нет журнала/прежних меток или поток новый → пропуск (не блокируем
// на неполных данных; свежесть относительно «сейчас» проверяет guardFreshness).
func (a *ASUIngest) checkNotNewer(ctx context.Context, files []LKFileInfo) error {
	prev, ok := a.journal.LastDislFormationTS(ctx)
	if !ok {
		return nil
	}
	for _, f := range files {
		if f.FormationTS.IsZero() {
			continue // отсутствие метки ловит checkSkew
		}
		pts, known := prev[f.Organisation]
		if !known {
			continue // новый поток — сравнивать не с чем
		}
		if !f.FormationTS.Time().After(pts.Time()) {
			a.log.Warn("АСУ: поток не принёс новых данных — обработка прекращена",
				zap.String("source", f.Organisation),
				zap.String("formation_ts", f.FormationTS.String()),
				zap.String("prev_ts", pts.String()))
			return fmt.Errorf("%w: %s (%s ≤ %s)",
				ErrSourceNotNewer, f.Organisation, f.FormationTS.String(), pts.String())
		}
	}
	return nil
}

// apiPullSources — включённые источники дислокации с ingest=api_pull (АСУ-АСУ).
func (a *ASUIngest) apiPullSources() []domain.DataSource {
	var out []domain.DataSource
	for _, ds := range a.cfg.EnabledByCategory(domain.CategoryDislocation) {
		if ds.Ingest == domain.IngestAPIPull {
			out = append(out, ds)
		}
	}
	return out
}

// fileInfo — ветка клиента для detail журнала (без ОКПО/файла: у АСУ их нет,
// ключ — код клиента провайдера). AgeMinutes — возраст среза относительно «сейчас».
func (a *ASUIngest) fileInfo(client string, ts *domain.LocalTime) LKFileInfo {
	fi := LKFileInfo{Organisation: client, Terminals: []string{client}}
	if ts != nil {
		fi.FormationTS = *ts
		fi.AgeMinutes = int(clock.Now().Time().Sub(ts.Time()).Minutes())
	}
	return fi
}

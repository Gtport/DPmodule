package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
)

// LKProcessor — шаг 2 двухшаговой загрузки ЛК: обработка staged-файлов в снимок
// дислокации. Читает контроль приёма (LKIntake.Status), парсит принятые файлы и
// атомарно заменяет снимок (ReplaceActual, «вариант B»). Обогащение Stage 1–4 —
// отдельными слоями (пока снимок «сырой»: коды без имён станций/портов).
type LKProcessor struct {
	intake   *LKIntake
	repo     port.DislocationRepository
	actual   *ActualCache
	status9  *Status9Cache
	status6  *Status6Cache
	history  port.HistoryRepository
	enricher *Enricher
	journal  *Journal   // единый журнал событий (может быть nil — cmd-утилиты)
	mu       sync.Mutex // сериализует пересборку снимка (ручной ЛК vs cron-АСУ идут через один proc)
}

func NewLKProcessor(intake *LKIntake, repo port.DislocationRepository, actual *ActualCache, status9 *Status9Cache, status6 *Status6Cache, history port.HistoryRepository) *LKProcessor {
	return &LKProcessor{intake: intake, repo: repo, actual: actual, status9: status9, status6: status6, history: history, enricher: NewEnricher(intake.dir)}
}

// SetJournal подключает журнал событий (nil-safe: без него запись пропускается).
func (p *LKProcessor) SetJournal(j *Journal) { p.journal = j }

var (
	ErrNotReady = errors.New("приём не готов к обработке")
	ErrDataLoss = errors.New("потеря данных превышает допустимый порог")
)

// LKProcessResult — итог обработки.
type LKProcessResult struct {
	Count            int            `json:"count"`              // записей в новом снимке
	Files            int            `json:"files"`              // обработано файлов
	PrevSnapshot     int            `json:"prev_snapshot"`      // размер прежнего снимка
	PerFile          map[string]int `json:"per_file"`           // имя файла → число записей
	NaznEnriched     int            `json:"nazn_enriched"`      // записей с заполненной станцией назначения (Stage 1)
	StationsNotFound []int          `json:"stations_not_found"` // коды станций вне справочника
	OpsNotFound      []int          `json:"ops_not_found"`      // коды операций вне справочника
	PortUnresolved   int            `json:"port_unresolved"`    // отброшено: (ОКПО+станция) не резолвится (Stage 2)
	PortDisabled     int            `json:"port_disabled"`      // отброшено: порт выключен (Stage 2)
	Status9Inserted  int            `json:"status9_inserted"`   // новых кандидатов статуса 9 (S2-1)
	Status9Removed   int            `json:"status9_removed"`    // снято кандидатов (вернулись в поток)
	Status8Missing   int            `json:"status8_missing"`    // пропавших → статус 8 (S2-1b)
	CarryMatched     int            `json:"carry_matched"`      // вагонов с carry-over из актуальной (S2-2)
	CarryNew         int            `json:"carry_new"`          // новых вагонов (S2-2)
	CarrySticky      int            `json:"carry_sticky"`       // статус удержан 4/5 (S2-2)
	Status6Donors    int            `json:"status6_donors"`     // переходов на статус 6 → доноры перегруза (§3.16)
	Status6Matched   int            `json:"status6_matched"`    // приёмников, добравших груз у донора (S2-3c)
	MarkaCandidates  int            `json:"marka_candidates"`   // записей без груза (нужна marka) (S2-3)
	MarkaFilled      int            `json:"marka_filled"`       // груз заполнен из marka (полное + частичное)
	MarkaMissed      int            `json:"marka_missed"`       // marka не нашла груз (кандидаты донорства S2-3c)
	NaznachOverride  int            `json:"naznach_override"`   // назначение из перестановки naznach_station
	ForecastComputed int            `json:"forecast_computed"`  // записей с расчётным прибытием RaschMsk (S2-5)
	ProgComputed     int            `json:"prog_computed"`      // записей с прогнозным прибытием ProgMsk (Stage 4)
	HistoryInserted  int            `json:"history_inserted"`   // новых рейсов в vagon_history (S2-6)
	HistoryUpdated   int            `json:"history_updated"`    // обновлённых строк истории по переходам
	StatusDist       map[int]int    `json:"status_dist"`        // распределение статусов (Stage 1b)
}

// Process проверяет готовность приёма, парсит все принятые файлы ЛК и заменяет
// снимок дислокации. Гарды: блокирующий контроль приёма (Status.ready) и порог
// потери данных (max_data_loss_pct) относительно текущего снимка.
func (p *LKProcessor) Process(ctx context.Context) (LKProcessResult, error) {
	st, err := p.intake.Status()
	if err != nil {
		return LKProcessResult{}, err
	}
	if !st.Ready {
		return LKProcessResult{}, fmt.Errorf("%w: %d блокирующих замечаний", ErrNotReady, countBlocking(st.Issues))
	}

	// Профиль парсера — из настроек источника 'lk' (формат файла).
	var profile parser.SourceProfile
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		profile = parser.SourceProfile{
			DateCutoffHour: ds.Config.DateCutoffHour,
			HeaderMarker:   ds.Config.HeaderMarker,
		}
	}
	lp := parser.NewLKParser(profile)

	dir := filepath.Join(p.intake.baseDir, "lk")
	perFile := make(map[string]int, len(st.Files))
	all := make([]domain.Dislocation, 0, 4096)
	for _, f := range st.Files {
		recs, err := lp.ParseFile(filepath.Join(dir, f.Filename))
		if err != nil {
			return LKProcessResult{}, fmt.Errorf("парсинг %s: %w", f.Filename, err)
		}
		perFile[f.Filename] = len(recs)
		all = append(all, recs...)
	}

	res, err := p.ProcessRecords(ctx, all, len(st.Files), perFile)
	if err != nil {
		return res, err
	}
	// Журнал: снимок дислокации пересобран. Триггер manual — обработка запущена
	// пользователем через UI (появится фоновый воркер — передаст TriggerScheduled).
	// doc_ts — самая старая метка формирования среди файлов. Best-effort.
	p.journal.RecordDislUpdate(ctx, "lk", domain.TriggerManual, st.Files, res.Count)
	return res, nil
}

// ProcessRecords — общий конвейер для уже распарсенного батча: Stage 1 → Stage 2/3
// (carry-over, marka, доноры, донорство, расчёт хода, vagon_history, кандидаты) →
// подмена снимка → перечитывание. Переиспользуется приёмом ЛК и будущим JSON-ingest.
// files/perFile — только для статистики результата.
func (p *LKProcessor) ProcessRecords(ctx context.Context, all []domain.Dislocation, files int, perFile map[string]int) (LKProcessResult, error) {
	// Пересборка снимка не должна пересекаться с другой пересборкой (ЛК ↔ АСУ):
	// carry-over/донорство/история читают прежний снимок, затем идёт атомарная подмена.
	p.mu.Lock()
	defer p.mu.Unlock()

	var err error

	// Stage 1: станции → идентификация порта + фильтр → операции → статусы.
	// Возвращает отфильтрованный обогащённый набор (только включённые порты).
	var cutoff int
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		cutoff = ds.Config.DateCutoffHour
	}
	sp := p.intake.cfg.Settings().Status
	var enr Stage1Stats
	all, enr = p.enricher.Stage1(all, Stage1Config{
		CutoffHour: cutoff, ProstDnMin: sp.ProstDnMin, ProstChMin: sp.ProstChMin,
	})

	// Контроль потери данных относительно текущего снимка (размер — из RAM ActualCache,
	// в БД за этим не ходим).
	prevSize := 0
	if p.actual != nil {
		prevSize = p.actual.Count()
	}
	pol := p.intake.cfg.Settings().IngestPolicy.Dislocation
	if lost := dataLossPct(prevSize, len(all)); pol.MaxDataLossPct > 0 && lost >= pol.MaxDataLossPct {
		return LKProcessResult{}, fmt.Errorf("%w: −%d%% (%d → %d) ≥ %d%%",
			ErrDataLoss, lost, prevSize, len(all), pol.MaxDataLossPct)
	}

	// Stage 2 (S2-2): carry-over из актуального снимка для существующих вагонов +
	// первичная установка index/invoice для новых. ДО reconcileCandidates (может
	// держать статус 4/5) и ДО подмены снимка (actual = прежний снимок).
	var co CarryOverStats
	if p.actual != nil {
		co = applyCarryOver(all, p.actual)
	}

	// Stage 2 (S2-3, §3.17): обогащение новых вагонов — груз из marka + перестановка
	// назначения. ПОСЛЕ carry-over (новые/пустые) и ДО донорства status6.
	mk := applyMarkaEnrichment(all, p.intake.dir)

	// Stage 2 (§3.16): доноры перегруза — при переходе на статус 6 (после carry-over,
	// у записи полные данные груза).
	var donors int
	if p.actual != nil && p.status6 != nil {
		if donors, err = applyStatus6Transition(ctx, all, p.actual, p.status6); err != nil {
			return LKProcessResult{}, fmt.Errorf("status6: %w", err)
		}
	}

	// Stage 2 (S2-3c, §3.17): донорство перегруза — новым вагонам без груза (marka не
	// нашла) переносим груз/назначение донора из status6 (матч по станции операции +
	// вес + срок); ПОСЛЕ applyStatus6Transition (доноры этого батча уже в кэше).
	var donorMatched int
	if p.status6 != nil {
		if donorMatched, err = applyStatus6Donorship(ctx, all, p.status6); err != nil {
			return LKProcessResult{}, fmt.Errorf("status6 донорство: %w", err)
		}
	}

	// Stage 3 (S2-5, §3.18): расчёт хода до порта (ToGo → RaschMsk → RaschJd) для
	// вагонов в пути (статус < 9). ПОСЛЕ донорства (приёмник берёт станцию донора).
	forecastN := applyForecast(all, p.intake.dir, cutoff)

	// Stage 4: прогноз прибытия на порт (ProgMsk) по ниткам станции. ПОСЛЕ Stage 3
	// (нужен RaschMsk) и carry-over (PlanMsk перенесён из актуального снимка).
	progN := applyStage4(all, p.intake.dir, p.intake.cfg, cutoff)

	// Stage 2 (S2-6, §3.19): запись вех рейса в vagon_history (INSERT новых + точечный
	// UPDATE по переходам). ДО подмены снимка (actual = пред. снимок для сравнения).
	var hist HistoryStats
	if p.actual != nil && p.history != nil {
		if hist, err = applyHistory(ctx, all, p.actual, p.history); err != nil {
			return LKProcessResult{}, fmt.Errorf("vagon_history: %w", err)
		}
	}

	// Stage 2 (S2-1): согласование таблицы кандидатов (статус 9 из живого батча +
	// статус 8 для пропавших) — ДО подмены снимка (actual = прежний снимок).
	var s9 Status9Stats
	if p.actual != nil && p.status9 != nil {
		if s9, err = reconcileCandidates(ctx, all, p.actual, p.status9); err != nil {
			return LKProcessResult{}, fmt.Errorf("status9: %w", err)
		}
	}

	if err := p.repo.ReplaceActual(ctx, all); err != nil {
		return LKProcessResult{}, fmt.Errorf("замена снимка: %w", err)
	}
	// Актуальная мапа сменилась — перечитываем (для следующего цикла).
	if p.actual != nil {
		if err := p.actual.Load(ctx); err != nil {
			return LKProcessResult{}, fmt.Errorf("перечитывание актуальной мапы: %w", err)
		}
	}
	return LKProcessResult{
		Count: len(all), Files: files, PrevSnapshot: prevSize, PerFile: perFile,
		NaznEnriched: enr.NaznEnriched, StationsNotFound: enr.StationsNotFound, OpsNotFound: enr.OperationsNotFound,
		PortUnresolved: enr.PortUnresolved, PortDisabled: enr.PortDisabled, StatusDist: enr.StatusDist,
		Status9Inserted: s9.Inserted, Status9Removed: s9.Removed, Status8Missing: s9.Missing8,
		CarryMatched: co.Matched, CarryNew: co.New, CarrySticky: co.Sticky,
		Status6Donors: donors, Status6Matched: donorMatched,
		MarkaCandidates: mk.Candidates, MarkaFilled: mk.FilledFull + mk.FilledPartial,
		MarkaMissed: mk.MissedMarka, NaznachOverride: mk.NaznachOverride,
		ForecastComputed: forecastN, ProgComputed: progN,
		HistoryInserted: hist.Inserted, HistoryUpdated: hist.Updated,
	}, nil
}

func countBlocking(issues []LKIssue) int {
	n := 0
	for _, i := range issues {
		if i.Level == LKIssueBlock {
			n++
		}
	}
	return n
}

// dataLossPct — процент сокращения набора относительно текущего снимка (0, если
// новый не меньше или снимок пуст). Целочисленно вниз.
func dataLossPct(current, next int) int {
	if current <= 0 || next >= current {
		return 0
	}
	return (current - next) * 100 / current
}

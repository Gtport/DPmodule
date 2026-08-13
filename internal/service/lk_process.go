package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/pkg/logger"
)

// LKProcessor — шаг 2 двухшаговой загрузки ЛК: обработка staged-файлов в снимок
// дислокации. Читает контроль приёма (LKIntake.Status), парсит принятые файлы и
// атомарно заменяет снимок (ReplaceActual, «вариант B»). Обогащение Stage 1–4 —
// отдельными слоями (пока снимок «сырой»: коды без имён станций/портов).
type LKProcessor struct {
	intake    *LKIntake
	repo      port.DislocationRepository
	actual    *ActualCache
	status9   *Status9Cache
	status6   *Status6Cache
	history   port.HistoryRepository
	unplanned port.UnplannedMoveRepository // «бесплановые в подходе» (nil — выключено/тесты)
	bros      port.BrosRepository          // снимок брошенных (nil — выключено/тесты)
	delays    port.VagonDelayRepository    // память о задержках вагонов (nil — выключено/тесты)
	vagonOps  *VagonOpService              // очередь запросов 601 (nil — выключено/тесты)
	enricher  *Enricher
	journal   *Journal             // единый журнал событий (может быть nil — cmd-утилиты)
	notif     *NotificationService // уведомления конвейера (nil — выключено/тесты)
	log       *zap.Logger          // запасной логгер: крон работает вне запроса (см. ProcessRecords)
	mu        sync.Mutex           // сериализует пересборку снимка (ручной ЛК vs cron-АСУ идут через один proc)
}

func NewLKProcessor(intake *LKIntake, repo port.DislocationRepository, actual *ActualCache, status9 *Status9Cache, status6 *Status6Cache, history port.HistoryRepository) *LKProcessor {
	return &LKProcessor{intake: intake, repo: repo, actual: actual, status9: status9, status6: status6, history: history,
		enricher: NewEnricher(intake.dir), log: zap.NewNop()}
}

// SetJournal подключает журнал событий (nil-safe: без него запись пропускается).
func (p *LKProcessor) SetJournal(j *Journal) { p.journal = j }

// SetLogger подключает логгер пересборки. Не задан — молчим (Nop): cmd-утилиты
// (jsonrun, planapply) печатают свой отчёт в stdout, второй поток им не нужен.
func (p *LKProcessor) SetLogger(l *zap.Logger) {
	if l != nil {
		p.log = l
	}
}

var (
	ErrNotReady             = errors.New("приём не готов к обработке")
	ErrDataLoss             = errors.New("потеря данных превышает допустимый порог")
	ErrDislTooStale         = errors.New("данные дислокации устарели — источник отдаёт старый срез")
	ErrDislOlderThanCurrent = errors.New("данные старше текущей дислокации — обновлять свежую дислокацию более старыми нельзя")
)

// guardFreshness блокирует пересборку дислокации из устаревших файлов ЛК (по метке
// формирования, самой старой в батче):
//   - max_staleness_minutes: метка старше N мин от «сейчас» → отказ (ErrDislTooStale);
//   - reject_older_than_current: батч старше текущей дислокации (doc_ts последнего
//     обновления из журнала) → отказ (ErrDislOlderThanCurrent), кроме роли-исключения.
//
// Порог ≤ 0 / флаг выкл / нет метки / нет журнала → соответствующий гард пропускается.
func (p *LKProcessor) guardFreshness(ctx context.Context, files []LKFileInfo) error {
	pol := p.intake.cfg.Settings().IngestPolicy.Dislocation
	current, _ := p.journal.LastDislDocTS(ctx)
	return checkFreshness(oldestFormation(files), clock.Now().Time(), pol, current,
		exemptRole(ctx, pol.RejectOlderRoleExempt))
}

// checkFreshness — чистое решение гарда (без кэша/журнала/часов), для тестируемости.
// oldest — самая старая метка формирования батча; current — doc_ts текущей дислокации.
func checkFreshness(oldest, now time.Time, pol domain.CategoryPolicy, current *domain.LocalTime, exempt bool) error {
	if oldest.IsZero() {
		return nil // нет метки — не блокируем
	}
	if pol.MaxStalenessMinutes > 0 {
		if age := int(now.Sub(oldest).Minutes()); age > pol.MaxStalenessMinutes {
			return fmt.Errorf("%w: сформированы %s (%d мин назад > %d)",
				ErrDislTooStale, oldest.Format("02.01 15:04"), age, pol.MaxStalenessMinutes)
		}
	}
	if pol.RejectOlderThanCurrent && current != nil && !current.IsZero() && oldest.Before(current.Time()) && !exempt {
		return fmt.Errorf("%w: файлы %s < текущей %s",
			ErrDislOlderThanCurrent, oldest.Format("02.01 15:04"), current.Time().Format("02.01 15:04"))
	}
	return nil
}

// oldestFormation — самая старая метка формирования батча (нулевые пропускаем).
func oldestFormation(files []LKFileInfo) time.Time {
	var oldest time.Time
	for _, f := range files {
		ts := time.Time(f.FormationTS)
		if ts.IsZero() {
			continue
		}
		if oldest.IsZero() || ts.Before(oldest) {
			oldest = ts
		}
	}
	return oldest
}

// exemptRole — есть ли у актора (из JWT) роль-исключение (пусто → нет
// исключения). Значение в настройке записано одним именем (исторически
// "administrator") — auth.AccessFor разворачивает его в пару списков, чтобы
// матчить пользователей и старой realm-схемы, и client-ролей.
func exemptRole(ctx context.Context, role string) bool {
	if role == "" {
		return false
	}
	c := auth.ClaimsFromContext(ctx)
	return c != nil && c.Allows(auth.AccessFor(auth.Role(role)))
}

// LKProcessResult — итог обработки.
type LKProcessResult struct {
	Count            int            `json:"count"`              // записей в новом снимке
	Files            int            `json:"files"`              // обработано файлов
	PrevSnapshot     int            `json:"prev_snapshot"`      // размер прежнего снимка
	PerFile          map[string]int `json:"per_file"`           // имя файла → число записей
	NaznEnriched     int            `json:"nazn_enriched"`      // записей с заполненной станцией назначения (Stage 1)
	StationsNotFound []int          `json:"stations_not_found"` // коды станций вне справочника
	OpsNotFound      []int          `json:"ops_not_found"`      // коды операций вне справочника
	OperExcluded     int            `json:"oper_excluded"`      // отброшено: нерабочий парк (exclude_oper_codes, напр. 72 «Списание»)
	PortUnresolved   int            `json:"port_unresolved"`    // отброшено: (ОКПО+станция) не резолвится (Stage 2)
	PortDisabled     int            `json:"port_disabled"`      // отброшено: порт выключен (Stage 2)
	Status9Inserted  int            `json:"status9_inserted"`   // новых кандидатов статуса 9 (S2-1)
	Status9Removed   int            `json:"status9_removed"`    // снято кандидатов (вернулись в поток)
	Status8Missing   int            `json:"status8_missing"`    // пропавших → статус 8 (S2-1b)
	Status8Purged    int            `json:"status8_purged"`     // автоочистка: снято пропавших старше TTL (S2-1c)
	CarryMatched     int            `json:"carry_matched"`      // вагонов с carry-over из актуальной (S2-2)
	CarryNew         int            `json:"carry_new"`          // новых вагонов (S2-2)
	CarryNewTrip     int            `json:"carry_new_trip"`     // вагон был в снимке, но уехал новым рейсом — без наследования (S2-2)
	CarrySticky      int            `json:"carry_sticky"`       // статус удержан 4/5 (S2-2)
	Status6Donors    int            `json:"status6_donors"`     // переходов на статус 6 → доноры перегруза (§3.16)
	Status6Matched   int            `json:"status6_matched"`    // приёмников, добравших груз у донора (S2-3c)
	MarkaCandidates  int            `json:"marka_candidates"`   // записей без груза (нужна marka) (S2-3)
	MarkaFilled      int            `json:"marka_filled"`       // груз заполнен из marka (полное + частичное)
	MarkaTrainFilled int            `json:"marka_train_filled"` // атрибуция унаследована по составу (S2-3d)
	MarkaMissed      int            `json:"marka_missed"`       // не нашли ни marka, ни состав (кандидаты донорства S2-3c)
	NaznachOverride  int            `json:"naznach_override"`   // назначение из перестановки naznach_station
	ForecastComputed int            `json:"forecast_computed"`  // записей с расчётным прибытием RaschMsk (S2-5)
	ProgComputed     int            `json:"prog_computed"`      // записей с прогнозным прибытием ProgMsk (Stage 4)
	HistoryInserted  int            `json:"history_inserted"`   // новых рейсов в vagon_history (S2-6)
	HistoryUpdated   int            `json:"history_updated"`    // обновлённых строк истории по переходам
	UnloadOnLeave    int            `json:"unload_on_leave"`    // авто-вех выгрузки по выбытию статуса-10
	VagonOpsQueued   int            `json:"vagon_ops_queued"`   // заявок 601 в очередь (прибытие/пропажа/выбытие)
	BrosNew          int            `json:"bros_new"`           // новых брошенных поездов (статус 5)
	BrosStopped      int            `json:"bros_stopped"`       // поднятых брошенных
	BrosActive       int            `json:"bros_active"`        // активных брошенных после пересбора
	BrosPurged       int            `json:"bros_purged"`        // автоочистка: удалено завершённых бросков старше TTL
	DelayOpened      int            `json:"delay_opened"`       // новых эпизодов задержек вагонов (статусы 4/5)
	DelayEscalated   int            `json:"delay_escalated"`    // эскалаций 4 → 5 (простой стал броском)
	DelayClosed      int            `json:"delay_closed"`       // закрытых эпизодов задержек
	DelayActive      int            `json:"delay_active"`       // открытых эпизодов после пересбора
	DelayPurged      int            `json:"delay_purged"`       // автоочистка: удалено закрытых эпизодов старше TTL
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

	// Гарды актуальности: не пересобирать дислокацию из устаревших файлов и не
	// затирать свежую дислокацию (напр. только что из АСУ) более старым ЛК.
	if err := p.guardFreshness(ctx, st.Files); err != nil {
		return LKProcessResult{}, err
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

// ProcessBatch — обработка батча, который пришёл НЕ файлами: робот ЛК читает
// дислокацию из ответа кабинета и в приём её не складывает. Порядок тот же, что
// у файлового Process: контроль комплекта (полнота/разрыв/устаревание) → гарды
// свежести → общий конвейер → журнал. files описывают потоки (ОКПО, организация,
// метка формирования) и нужны гардам и журналу; на диске их нет.
//
// Любой отказ фиксируется событием disl_rejected с кодом гарда — снимок при этом
// не трогается, а диспетчер видит в журнале, почему обновления не случилось.
func (p *LKProcessor) ProcessBatch(ctx context.Context, source string, all []domain.Dislocation, files []LKFileInfo, perFile map[string]int) (LKProcessResult, error) {
	reject := func(guard string, err error) (LKProcessResult, error) {
		p.journal.RecordDislRejected(ctx, source, domain.TriggerManual, files, guard, err.Error())
		return LKProcessResult{}, err
	}

	if issues := p.intake.checkBatch(files, "поток"); hasBlockingIssue(issues) {
		return reject("not_ready", fmt.Errorf("%w: %s", ErrNotReady, firstBlocking(issues)))
	}
	if err := p.guardFreshness(ctx, files); err != nil {
		guard := "stale"
		if errors.Is(err, ErrDislOlderThanCurrent) {
			guard = "older"
		}
		return reject(guard, err)
	}

	res, err := p.ProcessRecords(ctx, all, len(files), perFile)
	if err != nil {
		return reject("process", err) // в т.ч. порог потери данных (max_data_loss_pct)
	}
	p.journal.RecordDislUpdate(ctx, source, domain.TriggerManual, files, res.Count)
	return res, nil
}

// firstBlocking — текст первого блокирующего замечания (для сообщения диспетчеру:
// «почему не обновилось» важнее, чем их количество).
func firstBlocking(issues []LKIssue) string {
	for _, i := range issues {
		if i.Level == LKIssueBlock {
			return i.Message
		}
	}
	return ""
}

// MutateSnapshot — общий каркас операторской записи в снимок (перестановки,
// переадресация, подтверждение прибытия): одна операция = мьютекс конвейера →
// правка копии RAM-снимка → ОДИН пересчёт Stage 3–4 → атомарная подмена →
// перечитка RAM → одно событие журнала. Ничего не изменилось (updated=0) —
// снимок не трогается. mutate возвращает число реально изменённых записей.
func (p *LKProcessor) MutateSnapshot(ctx context.Context, journalSource string, detail map[string]any, mutate func([]domain.Dislocation) int) (updated, forecastN, progN int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.actual == nil {
		return 0, 0, 0, fmt.Errorf("%w: снимок дислокации не загружен", ErrNotReady)
	}
	all := p.actual.All()
	updated = mutate(all)
	if updated == 0 {
		return 0, 0, 0, nil
	}

	var cutoff int
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		cutoff = ds.Config.DateCutoffHour
	}
	forecastN = applyForecast(all, p.intake.dir, cutoff)
	progN = applyStage4(all, p.intake.dir, p.intake.cfg, cutoff)

	if err := p.repo.ReplaceActual(ctx, all); err != nil {
		return 0, 0, 0, fmt.Errorf("замена снимка: %w", err)
	}
	if err := p.actual.Load(ctx); err != nil {
		return 0, 0, 0, fmt.Errorf("перечитывание актуальной мапы: %w", err)
	}

	if p.journal != nil && detail != nil {
		detail["count"] = updated
		p.journal.RecordRearrangement(ctx, journalSource, updated, detail)
	}
	return updated, forecastN, progN, nil
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

	// Логгер запроса, если пересборку позвал человек («Обновить из АСУ», приём
	// ЛК) — тогда строки конвейера сшиваются с его request_id. Крон работает вне
	// запроса, там остаётся корневой.
	log := logger.FromContextOr(ctx, p.log)
	started := time.Now()

	var err error

	// Stage 1: станции → идентификация порта + фильтр → операции → статусы.
	// Возвращает отфильтрованный обогащённый набор (только включённые порты).
	var cutoff int
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		cutoff = ds.Config.DateCutoffHour
	}
	sp := p.intake.cfg.Settings().Status
	exclOper := sp.ExcludedOperSet() // нерабочий парк (дефолт 72 «Списание»)
	var enr Stage1Stats
	all, enr = p.enricher.Stage1(all, Stage1Config{
		CutoffHour: cutoff, ProstDnMin: sp.ProstDnMin, ProstChMin: sp.ProstChMin,
		ExcludeOper: exclOper,
	})

	// Уведомление dicts-ролям о станциях вне справочника (дедуп по коду —
	// каждый забор их приносит заново, а сигналить надо один раз).
	p.notif.NotifyStationsMissing(ctx, enr.StationsNotFound)

	// Дыры справочников — Warn, а не Info: вагон из-за них не теряется (решение
	// владельца 03.08.2026, неизвестная станция гасит только свои поля), но
	// пустой терминал уводит запись мимо прогнозов и отчётов молча. Именно так
	// пропадали 15 вагонов НМТП со станцией ШУШАРЫ. Коды даём выборкой: их
	// бывают сотни, а для правки справочника хватает первых.
	if len(enr.StationsNotFound)+len(enr.OperationsNotFound)+len(enr.CargoNotFound) > 0 {
		log.Warn("конвейер: коды вне справочников",
			zap.Int("stations", len(enr.StationsNotFound)), zap.Ints("stations_sample", sampleCodes(enr.StationsNotFound)),
			zap.Int("operations", len(enr.OperationsNotFound)), zap.Ints("operations_sample", sampleCodes(enr.OperationsNotFound)),
			zap.Int("cargo", len(enr.CargoNotFound)), zap.Ints("cargo_sample", sampleCodes(enr.CargoNotFound)))
	}
	if enr.PortUnresolved > 0 {
		log.Warn("конвейер: порт не резолвится — вагоны остались без терминала",
			zap.Int("vagons", enr.PortUnresolved))
	}

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

	// Уведомление dicts-ролям о дырах словаря marka (те же ключ и фильтры, что
	// экран «Без атрибуции» — уведомление зовёт к нему, не дублируя).
	if p.notif != nil {
		p.notif.NotifyMarkaMissing(ctx, collectMarkaMissing(all, p.intake.dir))
	}

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
		if hist, err = applyHistory(ctx, all, p.actual, p.history, cutoff); err != nil {
			return LKProcessResult{}, fmt.Errorf("vagon_history: %w", err)
		}
	}

	// Уведомление операторам о прибывших поездах: переходы <10 → 10 против
	// прежнего снимка, группировка по индексу + единому штампу прибытия
	// (sticky-10 повторно не сигналит). ДО подмены снимка.
	if p.notif != nil && p.actual != nil {
		p.notif.NotifyArrivals(ctx, collectArrivalGroups(all, p.actual, cutoff))
	}

	// Выбытие без выгрузки: статус-10 исчез из батча (перехода 10→12 не застали) —
	// авто-веха выгрузки в историю моментом выбытия. ДО подмены снимка.
	var unloadLeft int
	if p.actual != nil && p.history != nil {
		if unloadLeft, err = applyUnloadOnLeave(ctx, all, p.actual, p.history, p.intake.dir.PorozhInbound); err != nil {
			return LKProcessResult{}, fmt.Errorf("выбытие-10 без выгрузки: %w", err)
		}
	}

	// Снимок брошенных (reconcile статуса 5): новые/продолжающиеся/поднятые
	// броски. ПОСЛЕ Stage 4 (нужен ProgJd) и ДО подмены снимка (actual =
	// прежний, для определения подъёма). Пишет в таблицу bros.
	var brosStats BrosStats
	if p.actual != nil && p.bros != nil {
		if brosStats, err = applyBros(ctx, all, p.actual, p.bros); err != nil {
			return LKProcessResult{}, fmt.Errorf("bros: %w", err)
		}
		// Уведомления операторам «Брошен поезд!» / «Поезд восстановлен в
		// движении» — по событиям реконсиляции (дедуп на ключе + дате).
		p.notif.NotifyBrosEvents(ctx, brosStats.Events)
	}

	// Автоочистка завершённых бросков старше порога (status.bros_cleanup_days; 0 →
	// выключена). ⚠️ Ограничивает глубину отчёта — удалённые броски в него не попадут.
	var brosPurged int
	if p.bros != nil && sp.BrosCleanupDays > 0 {
		cutoff := domain.LocalTime(time.Time(clock.Now()).Add(-time.Duration(sp.BrosCleanupDays) * 24 * time.Hour))
		if brosPurged, err = p.bros.PurgeCompletedOlderThan(ctx, cutoff); err != nil {
			return LKProcessResult{}, fmt.Errorf("автоочистка bros: %w", err)
		}
	}

	// Память о задержках вагонов (эпизоды статусов 4/5): reconcile vagon_delay.
	// ПОСЛЕ Stage 4 (статусы и sticky финальные); прежний снимок не нужен —
	// состояние «до» несут открытые эпизоды в таблице.
	var delayStats DelayStats
	if p.delays != nil {
		if delayStats, err = applyDelays(ctx, all, p.delays); err != nil {
			return LKProcessResult{}, fmt.Errorf("vagon_delay: %w", err)
		}
	}

	// Автоочистка закрытых эпизодов задержек старше порога (status.delay_cleanup_days;
	// 0 → выключена, хранить бессрочно). ⚠️ Ограничивает глубину отчёта по простоям.
	var delayPurged int
	if p.delays != nil && sp.DelayCleanupDays > 0 {
		cutoffD := domain.LocalTime(time.Time(clock.Now()).Add(-time.Duration(sp.DelayCleanupDays) * 24 * time.Hour))
		if delayPurged, err = p.delays.PurgeClosedOlderThan(ctx, cutoffD); err != nil {
			return LKProcessResult{}, fmt.Errorf("автоочистка vagon_delay: %w", err)
		}
	}

	// Заявки на историю продвижения (601): прибытие / пропажа / выбытие-10 —
	// только постановка в очередь (HTTP — у фонового воркера). ДО подмены
	// снимка. Отказ очереди пересборку не валит: трейл — вторичные данные.
	var opQueued int
	if p.actual != nil && p.vagonOps != nil {
		opQueued, _ = p.vagonOps.EnqueueTransitions(ctx, all, p.actual) // отказ логирует сервис
	}

	// «Бесплановые в подходе» (Оперативка): смена станции без плана ближе порога —
	// ДО подмены снимка (сравнение с прежним). Ошибка не валит пересборку.
	if p.actual != nil && p.unplanned != nil {
		if _, _, uerr := trackUnplannedMoves(ctx, all, p.actual, p.unplanned, p.intake.dir, sp.UnplannedMoveKm); uerr != nil {
			return LKProcessResult{}, fmt.Errorf("unplanned_move: %w", uerr)
		}
	}

	// Stage 2 (S2-1): согласование таблицы кандидатов (статус 9 из живого батча +
	// статус 8 для пропавших) — ДО подмены снимка (actual = прежний снимок).
	var s9 Status9Stats
	if p.actual != nil && p.status9 != nil {
		if s9, err = reconcileCandidates(ctx, all, p.actual, p.status9, exclOper); err != nil {
			return LKProcessResult{}, fmt.Errorf("status9: %w", err)
		}
	}

	// Stage 2 (S2-1c): автоочистка пропавших (статус 8) старше TTL — таблица
	// кандидатов не разрастается записями, по которым уже не будет возврата.
	// Порог — client_settings.extra.status.missing8_ttl_days; 0 → выключена.
	var s8purged int
	if p.status9 != nil && sp.Missing8TTLDays > 0 {
		cutoffTTL := domain.LocalTime(time.Time(clock.Now()).Add(-time.Duration(sp.Missing8TTLDays) * 24 * time.Hour))
		if s8purged, err = p.status9.PurgeMissingOlderThan(ctx, cutoffTTL); err != nil {
			return LKProcessResult{}, fmt.Errorf("автоочистка status9: %w", err)
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

	// Итог пересборки одной строкой. Те же счётчики уходят в event_journal, но
	// журнал — экран диспетчера и живёт в базе: когда база и есть подозреваемый
	// (пересборка встала, снимок не подменился), смотреть больше некуда.
	// carry_new_trip здесь не для красоты: рейс, сменившийся под тем же вагоном,
	// сбрасывает назначение и план, и всплеск этого счётчика объясняет
	// «вагон вдруг без терминала» без раскопок.
	log.Info("снимок пересобран",
		zap.Duration("took", time.Since(started)),
		zap.Int("vagons", len(all)), zap.Int("prev", prevSize),
		zap.Int("carry_matched", co.Matched), zap.Int("carry_new", co.New),
		zap.Int("carry_new_trip", co.NewTrip), zap.Int("carry_sticky", co.Sticky),
		zap.Int("history_inserted", hist.Inserted), zap.Int("history_updated", hist.Updated),
		zap.Int("status9", s9.Inserted), zap.Int("status8_missing", s9.Missing8),
		zap.Int("bros_new", brosStats.New), zap.Int("bros_stopped", brosStats.Stopped),
		zap.Int("marka_missed", mk.MissedMarka),
	)

	return LKProcessResult{
		Count: len(all), Files: files, PrevSnapshot: prevSize, PerFile: perFile,
		NaznEnriched: enr.NaznEnriched, StationsNotFound: enr.StationsNotFound, OpsNotFound: enr.OperationsNotFound,
		PortUnresolved: enr.PortUnresolved, PortDisabled: enr.PortDisabled, StatusDist: enr.StatusDist,
		OperExcluded:    enr.OperExcluded,
		Status9Inserted: s9.Inserted, Status9Removed: s9.Removed, Status8Missing: s9.Missing8,
		Status8Purged: s8purged,
		CarryMatched:  co.Matched, CarryNew: co.New, CarryNewTrip: co.NewTrip, CarrySticky: co.Sticky,
		Status6Donors: donors, Status6Matched: donorMatched,
		MarkaCandidates: mk.Candidates, MarkaFilled: mk.FilledFull,
		MarkaTrainFilled: mk.FilledByTrain,
		MarkaMissed:      mk.MissedMarka, NaznachOverride: mk.NaznachOverride,
		ForecastComputed: forecastN, ProgComputed: progN,
		HistoryInserted: hist.Inserted, HistoryUpdated: hist.Updated,
		UnloadOnLeave: unloadLeft, VagonOpsQueued: opQueued,
		BrosNew: brosStats.New, BrosStopped: brosStats.Stopped, BrosActive: brosStats.Active,
		BrosPurged:  brosPurged,
		DelayOpened: delayStats.Opened, DelayEscalated: delayStats.Escalated,
		DelayClosed: delayStats.Closed, DelayActive: delayStats.Active,
		DelayPurged: delayPurged,
	}, nil
}

// sampleCodes — первые несколько кодов для строки лога. Дыр в справочниках
// бывают сотни (диапазон 00xxxx–05xxxx Октябрьской дороги отсутствует целиком),
// и вываливать их полным списком в каждую запись незачем: точное число рядом,
// а править справочник начинают с первого.
func sampleCodes(codes []int) []int {
	const maxSample = 10
	return codes[:min(len(codes), maxSample)]
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

// SetUnplannedRepo подключает таблицу «бесплановых в подходе» (Оперативка);
// nil — трекинг выключен (cmd-утилиты, тесты).
func (p *LKProcessor) SetUnplannedRepo(repo port.UnplannedMoveRepository) {
	p.unplanned = repo
}

// SetVagonOps подключает очередь запросов истории продвижения (601);
// nil — триггеры выключены (cmd-утилиты, тесты).
func (p *LKProcessor) SetVagonOps(svc *VagonOpService) { p.vagonOps = svc }

// SetBros подключает снимок брошенных (reconcile статуса 5 после Stage 4);
// nil — подсистема выключена (cmd-утилиты, тесты).
func (p *LKProcessor) SetBros(repo port.BrosRepository) { p.bros = repo }

// SetDelays подключает память о задержках вагонов (reconcile эпизодов статусов
// 4/5 после Stage 4); nil — подсистема выключена (cmd-утилиты, тесты).
func (p *LKProcessor) SetDelays(repo port.VagonDelayRepository) { p.delays = repo }

// SetNotifications подключает уведомления конвейера (брошенные/прибытия/дыры
// справочников); nil — подсистема выключена (cmd-утилиты, тесты). Эмиттеры
// best-effort: сбой записи уведомления пересборку снимка не валит.
func (p *LKProcessor) SetNotifications(svc *NotificationService) { p.notif = svc }

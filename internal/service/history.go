package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// HistoryStats — диагностика записи бизнес-истории (S2-6).
type HistoryStats struct {
	Inserted int // новых рейсов (INSERT)
	Updated  int // обновлённых строк (точечный UPDATE по переходу)
}

// applyHistory — Stage 2 (S2-6, §3.19): запись вех рейса в vagon_history. INSERT для
// новых рейсов, точечный UPDATE по переходам статуса/накладной для существующих
// (сравнение с actual = пред. снимок). Трейл vagon_operation — отдельно.
// Идёт ПОСЛЕ forecast и ДО подмены снимка (actual ещё прежний). Без заморозки на 10.
//
// ⚠️ Рейс опознаётся по trip_key (вагон + дата начала рейса) — тем же ключом, что
// стоит уникальным индексом в БД, а НЕ по id строки: в id входит ещё и станция
// отправления. Наступили на это 03.08.2026: у 15 вагонов станция отправления
// появилась (перестали терять её код), id сменился, рейсы показались новыми — и
// вставка упала на uq_vagon_history_trip_key, оборвав всю пересборку снимка.
// Строку с уже существующим рейсом обновляем по её настоящему id, каким бы он ни был.
func applyHistory(ctx context.Context, kept []domain.Dislocation, actual *ActualCache, repo port.HistoryRepository, cutoff int) (HistoryStats, error) {
	var st HistoryStats
	keys := make([]int64, 0, len(kept))
	for i := range kept {
		if key, ok := historyTripKey(&kept[i]); ok {
			keys = append(keys, key)
		}
	}
	existing, err := repo.ExistingTrips(ctx, keys)
	if err != nil {
		return HistoryStats{}, fmt.Errorf("existing trips: %w", err)
	}

	now := clock.Now()
	var toInsert []domain.VagonHistory
	seen := make(map[int64]struct{}, len(kept))
	for i := range kept {
		r := &kept[i]
		if r.Vagon == "" || r.ID == "" {
			continue
		}
		key, hasKey := historyTripKey(r)
		rowID, exists := "", false
		if hasKey {
			rowID, exists = existing[key]
		}
		if !exists {
			// Один рейс — одна строка: два вагона с одним trip_key в батче
			// невозможны, но подстраховка дешевле повторного падения вставки.
			if hasKey {
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
			}
			toInsert = append(toInsert, buildHistoryRow(r, now, cutoff))
			continue
		}
		prev, ok := actual.FindVagonInActual(r.Vagon)
		if !ok {
			continue // нет прежнего состояния — переходов не детектируем
		}
		fields := historyUpdateFields(&prev, r, cutoff)
		if len(fields) == 0 {
			continue
		}
		fields["updated_at"] = now
		if err := repo.UpdateFields(ctx, rowID, fields); err != nil {
			return HistoryStats{}, fmt.Errorf("update %s: %w", rowID, err)
		}
		st.Updated++
	}
	if err := repo.Insert(ctx, toInsert); err != nil {
		// Вставка пачкой: падает она целиком, а сообщение драйвера называет
		// только нарушенное ограничение — 06.08.2026 из-за этого «duplicate key
		// vagon_history_pkey» на 4890 строках пришлось разбирать вручную. Размер
		// пачки и несколько id (id рейса = вагон/станция/дата) сужают поиск до
		// понятного места. ⚠️ Виновник в выборку попадает не всегда: DETAIL с
		// конкретным ключом pgx в текст ошибки не кладёт.
		return HistoryStats{}, fmt.Errorf("insert history (%d рейсов, напр. %s): %w",
			len(toInsert), sampleTripIDs(toInsert), err)
	}
	st.Inserted = len(toInsert)
	return st, nil
}

// sampleTripIDs — несколько id рейсов из пачки для сообщения об отказе вставки.
func sampleTripIDs(rows []domain.VagonHistory) string {
	const maxSample = 3
	ids := make([]string, 0, maxSample)
	for i := 0; i < len(rows) && i < maxSample; i++ {
		ids = append(ids, rows[i].ID)
	}
	return strings.Join(ids, ", ")
}

// applyUnloadOnLeave — дополнение к S2-6 (решение владельца): вагон со статусом 10
// (прибыл, не выгружен) исчез из нового батча — штатное выбытие, но переход 10→12
// мы не застали (выгружен и уехал из зоны наблюдения между снимками). Без этого
// рейс навсегда остаётся «не выгружен» (случай АЭ 143/144). Дописываем веху
// выгрузки в историю: date_vigr = момент выбытия, ЖД-сутки по правилу «час ≥ 18 →
// +1», place_vigr = терминал назначения, статус 12 (рейс завершён). Точное время
// правится оператором в «Истории прибывших». Строки, где выгрузка уже внесена
// вручную (date_vigr заполнен — снимок держал 10 по sticky), НЕ трогаем: ручная
// веха вернее автоматической. Вызывается ДО подмены снимка (actual — прежний).
//
// porozhInbound (nil = никто) — предикат «порожний под погрузку»: такой вагон
// прибыл ЗА грузом, его выбытие — погрузка и отъезд, а не выгрузка; ложную веху
// выгрузки не пишем — иначе порожняк попадал бы в П/В, «Грузовую работу» и
// отчёты выгрузки (решение владельца 04.08.2026). Рейс в истории остаётся с
// прибытием без выгрузки — честное отражение порожнякового захода.
func applyUnloadOnLeave(ctx context.Context, kept []domain.Dislocation, actual *ActualCache, repo port.HistoryRepository, porozhInbound func(*domain.Dislocation) bool) (int, error) {
	seen := make(map[string]struct{}, len(kept))
	for i := range kept {
		if kept[i].Vagon != "" {
			seen[kept[i].Vagon] = struct{}{}
		}
	}
	naznachByKey := map[int64]string{}
	var keys []int64
	naznachByID := map[string]string{}
	var ids []string // фолбэк: рейс без trip_key (нет даты начала) ищем по id, как раньше
	for _, v := range actual.All() {
		if v.Vagon == "" || v.ID == "" || v.Status == nil || *v.Status != 10 {
			continue
		}
		if _, present := seen[v.Vagon]; present {
			continue
		}
		if porozhInbound != nil && porozhInbound(&v) {
			continue // порожний под погрузку: выбытие — погрузка, вехи выгрузки нет
		}
		if key, hasKey := historyTripKey(&v); hasKey {
			keys = append(keys, key)
			naznachByKey[key] = v.Naznach
			continue
		}
		naznachByID[v.ID] = v.Naznach
		ids = append(ids, v.ID)
	}
	if len(keys) == 0 && len(ids) == 0 {
		return 0, nil
	}
	// Строка рейса ищется по trip_key, как в applyHistory: её id мог разойтись
	// с id снимка (появилась станция отправления, временный id) — поиск по id
	// снимка молча писал бы веху в чужую строку либо терял её.
	if len(keys) > 0 {
		existing, err := repo.ExistingTrips(ctx, keys)
		if err != nil {
			return 0, fmt.Errorf("рейсы выбывших: %w", err)
		}
		for key, rowID := range existing {
			naznachByID[rowID] = naznachByKey[key]
			ids = append(ids, rowID)
		}
	}
	rows, err := repo.RowsByIDs(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("строки истории выбывших: %w", err)
	}
	now := clock.Now()
	jd := dateOnly(jdFromFact(&now))
	updates := map[string]map[string]any{}
	for i := range rows {
		if rows[i].DateVigr != nil {
			continue // выгрузка уже внесена (обычно вручную) — не перетирать
		}
		updates[rows[i].ID] = map[string]any{
			"status":      12,
			"date_vigr":   now,
			"date_vigr_d": jd,
			"place_vigr":  naznachByID[rows[i].ID],
			"updated_at":  now,
		}
	}
	if len(updates) == 0 {
		return 0, nil
	}
	if err := repo.UpdateFieldsBatch(ctx, updates); err != nil {
		return 0, fmt.Errorf("веха выгрузки выбывших: %w", err)
	}
	return len(updates), nil
}

// historyUpdateFields — точечные обновления по переходам (actual → new): накладная,
// статус, index_main (0→другой), выгрузка (→12), прибытие (→10). Пустая карта = нет
// изменений.
func historyUpdateFields(prev, r *domain.Dislocation, cutoff int) map[string]any {
	fields := map[string]any{}
	if prev.Invoice != r.Invoice {
		fields["invoice"] = r.Invoice // текущая накладная; invoice_main не трогаем
	}
	if prev.Owner != r.Owner && r.Owner != "" {
		fields["owner"] = r.Owner // owner вычислился/появился после вставки строки рейса
	}
	ps, ns := derefInt(prev.Status), derefInt(r.Status)
	if ps == ns {
		return fields
	}
	fields["status"] = ns
	if ps == 0 && ns != 0 {
		fields["index_main"] = r.IndexMain
	}
	if ns == 12 {
		fields["date_vigr"] = r.TimeOp
		fields["date_vigr_d"] = dateOnly(r.DateOpJd)
		fields["place_vigr"] = r.Naznach
	}
	if ns == 10 {
		prib := historyArrival(r, cutoff)
		fields["date_prib"] = prib
		fields["date_prib_d"] = dateOnly(prib)
		fields["delay"] = calculateHistoryDelay(dateOnly(prib), r.DateDostav)
		fields["otkl"] = calculateOtkl(prib, r.PlanMsk)
		fields["plan_msk"] = r.PlanMsk
		fields["plan_jd"] = r.PlanJd
		fields["naznach"] = r.Naznach
		// Индекс поезда на момент прибытия (решение владельца): в историю пишем
		// ТЕКУЩИЙ индекс дислокации (r.Index — фактический поезд, которым вагон
		// приехал), а не метку нитки плана. Строка истории создавалась при первом
		// появлении вагона, когда прибытийного индекса ещё не было.
		if r.Index != "" {
			fields["index_pp"] = r.Index
		}
	}
	return fields
}

// buildHistoryRow — полная строка истории для нового рейса. Поля прибытия/выгрузки
// проставляются, если запись впервые появилась уже со статусом 10/12.
func buildHistoryRow(r *domain.Dislocation, now domain.LocalTime, cutoff int) domain.VagonHistory {
	h := domain.VagonHistory{
		ID: r.ID, Vagon: r.Vagon, InvoiceMain: r.InvoiceMain, Invoice: r.Invoice,
		IndexMain: r.IndexMain, IndexPp: r.IndexPp, DateNachD: dateOnly(r.DateNach),
		StationNach: r.StationNach, Gruzotpr: r.Gruzotpr, Zayavka: r.Zayavka,
		StanNazn: r.StanNazn, GruzpolS: r.GruzpolS, Naznach: r.Naznach,
		CargoS: r.CargoS, CargoGroup: r.CargoGroup,
		FreightExactName: r.FreightExactName, GtdNumber: r.GtdNumber, Ves: r.Ves,
		Client: r.Client, RodVagUch: r.RodVagUch,
		CarOwnerName: r.CarOwnerName, CarOwnerOkpo: r.CarOwnerOkpo,
		CarTenantName: r.CarTenantName, CarTenantOkpo: r.CarTenantOkpo,
		CarTrustedName: r.CarTrustedName, CarTrustedOkpo: r.CarTrustedOkpo,
		Owner:       r.Owner,
		PereadrType: r.PereadrType, PereadrPort: r.PereadrPort,
		Status: r.Status, DateDostav: r.DateDostav, PlanMsk: r.PlanMsk, PlanJd: r.PlanJd,
		Frost: r.Frost, Shipments: r.Shipments, Peregruz: r.Peregruz,
		Info1: r.Info1, Info2: r.Info2, Sms1: r.Sms1, Sms2: r.Sms2, Sms3: r.Sms3,
		Color: r.Color, CreatedAt: &now, UpdatedAt: &now,
	}
	switch derefInt(r.Status) {
	case 10:
		prib := historyArrival(r, cutoff)
		h.DatePrib = prib
		h.DatePribD = dateOnly(prib)
		h.Delay = calculateHistoryDelay(dateOnly(prib), r.DateDostav)
		h.Otkl = calculateOtkl(prib, r.PlanMsk)
	case 12:
		h.DateVigr = r.TimeOp
		h.DateVigrD = dateOnly(r.DateOpJd)
		h.PlaceVigr = r.Naznach
	}
	return h
}

// historyArrival — что писать в vagon_history.date_prib при переходе в статус 10:
// момент прибытия ИЗ ПОТОКА (тот самый, по которому computeStatus и поставил 10),
// приведённый к ЖД-шкале хранения.
//
// Раньше сюда шёл date_kon (= date_op_jd = время ПОСЛЕДНЕЙ операции вагона, эталон
// gtport). От этого состав рассыпался на экране «Прибывшие»: ключ группы —
// index_pp + date_prib, а вагоны одного поезда переходят в статус 10 в разные
// заборы крона и приносят каждый своё время операции. Провайдер же отдаёт единый
// момент прибытия на весь поезд (сверено на боевом снимке 06.08.2026: 63 вагона —
// один штамп date_prib и два разных date_kon), поэтому пишем его.
//
// ⚠️ Сдвиг обязателен: date_kon нёс ЖД-шкалу (date_op_jd), а поток отдаёт сырой
// МСК-штамп. Инвариант «date_prib в истории — ЖД» держит грузовую работу
// (cargowork_analytics.go) и calculateOtkl, который сам возвращает факт в МСК.
// Правило то же, что у ручного ввода (jdFromFact) и deriveDates.
//
// Фолбэк на date_kon — страховка: статус 10 без даты прибытия невозможен по
// построению (computeStatus), но веху терять нельзя, если инвариант где-то нарушат.
func historyArrival(r *domain.Dislocation, cutoff int) *domain.LocalTime {
	if prib := arrivalJd(r.DatePrib, cutoff); prib != nil {
		return prib
	}
	return r.DateKon
}

// arrivalJd — момент прибытия в ЖД-шкалу: «час ≥ cutoff → +1 сутки». cutoff ≤ 0 → 18
// (как EnrichConfig.CutoffHour).
func arrivalJd(t *domain.LocalTime, cutoff int) *domain.LocalTime {
	if t == nil || time.Time(*t).IsZero() {
		return nil
	}
	if cutoff <= 0 {
		cutoff = 18
	}
	tt := time.Time(*t)
	if tt.Hour() >= cutoff {
		tt = tt.AddDate(0, 0, 1)
	}
	lt := domain.LocalTime(tt)
	return &lt
}

// calculateHistoryDelay — просрочка доставки в сутках: дата прибытия vs норматив.
// Прибыл раньше срока → 0; нет одной из дат → nil.
func calculateHistoryDelay(pribD, dostav *domain.LocalTime) *int {
	if pribD == nil || dostav == nil {
		return nil
	}
	p, d := time.Time(*pribD), time.Time(*dostav)
	if p.IsZero() || d.IsZero() {
		return nil
	}
	if p.Before(d) {
		z := 0
		return &z
	}
	days := int(p.Sub(d).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return &days
}

// calculateOtkl — отклонение факта прибытия от плана «±hh:mm». Час факта ≥ 18 → факт
// сдвигается на сутки назад (как в gtlogic). Нет плана → пусто (появится со Stage 4).
func calculateOtkl(fact, plan *domain.LocalTime) string {
	if fact == nil || plan == nil {
		return ""
	}
	f, p := time.Time(*fact), time.Time(*plan)
	if f.IsZero() || p.IsZero() {
		return ""
	}
	if f.Hour() >= 18 {
		f = f.Add(-24 * time.Hour)
	}
	diff := f.Sub(p)
	sign := "+"
	if diff < 0 {
		sign = "-"
		diff = -diff
	}
	return fmt.Sprintf("%s%02d:%02d", sign, int(diff.Hours()), int(diff.Minutes())%60)
}

// dateOnly — только дата (H:M:S=0), nil для nil/нулевого времени.
func dateOnly(t *domain.LocalTime) *domain.LocalTime {
	if t == nil || time.Time(*t).IsZero() {
		return nil
	}
	tt := time.Time(*t)
	d := domain.LocalTime(time.Date(tt.Year(), tt.Month(), tt.Day(), 0, 0, 0, 0, time.UTC))
	return &d
}

// historyTripKey — ключ рейса в том же виде, в каком его считает БД
// (vagon::bigint * 100000 + дни от 1970-01-01 по date_nach_d). Считаем на нашей
// стороне, чтобы искать существующие рейсы одним запросом по списку ключей.
// false — рейс без номера вагона или без даты начала: такой ключ база не построит.
func historyTripKey(r *domain.Dislocation) (int64, bool) {
	if r.Vagon == "" || r.DateNach == nil || r.DateNach.IsZero() {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(r.Vagon), 10, 64)
	if err != nil {
		return 0, false
	}
	d := r.DateNach.Time()
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	days := int64(day.Sub(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	return v*100000 + days, true
}

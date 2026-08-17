package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/pkg/logger"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/report"
)

// lastUpdateLayout — формат курсора провайдера: "YYYY-MM-DD HH:MM:SS.sss".
const lastUpdateLayout = "2006-01-02 15:04:05.000"

// docFetchPause — пауза между заборами сырых бланков внутри одной пачки
// инкремента: документов в тике десятки, очередь без пауз давила бы провайдера.
const docFetchPause = 200 * time.Millisecond

// ReferenceService — памятки ГУ-45 на подачу/уборку: крон-инкремент у внешнего
// провайдера и разнос вех подачи/уборки по рейсам в vagon_history.
//
// В gtport эти вехи вбивали руками — Excel-файлом «Журнал движения». Здесь их
// ведёт крон: раз в pull_interval по каждому клиенту забираем изменения с
// сохранённого курсора, разбираем и раскладываем движком domain.ApplyPamyatki
// (правила выбора рейса и перезаписи — там же, с тестами).
//
// Сами памятки не храним: нужны не документы, а вехи рейса. Единственное
// служебное состояние — курсор last_update по клиентам (таблица pamyatka_cursor).
type ReferenceService struct {
	cl      port.ReferenceClient
	history port.HistoryRepository
	cursors port.PamyatkaCursorRepository
	journal port.JournalRepository
	clients []string
	timing  ReferenceTiming
	log     *zap.Logger
}

// ReferenceTiming — тайминги инкремента (config reference.*). Отдельной
// структурой, а не четырьмя аргументами подряд: длительности одного типа
// слишком легко перепутать местами, а цена ошибки здесь — молчащий крон.
type ReferenceTiming struct {
	Interval        time.Duration // период крон-тика (pull_interval)
	InitialLookback time.Duration // глубина окна первого захода, когда курсора нет
	CursorOverlap   time.Duration // запас: курсор храним на столько раньше LAST_UPDATE
	StaleAfter      time.Duration // курсор не двигался дольше — пустой проход идёт в журнал
}

func NewReferenceService(
	cl port.ReferenceClient,
	history port.HistoryRepository,
	cursors port.PamyatkaCursorRepository,
	journal port.JournalRepository,
	clients []string,
	timing ReferenceTiming,
	log *zap.Logger,
) *ReferenceService {
	return &ReferenceService{
		cl: cl, history: history, cursors: cursors, journal: journal,
		clients: clients, timing: timing, log: log,
	}
}

// PamyatkaPullResult — итог одного прохода по клиенту (для ручки и журнала).
type PamyatkaPullResult struct {
	Client      string         `json:"client"`
	Pamyatki    int            `json:"pamyatki"`     // разобрано памяток
	Vagons      int            `json:"vagons"`       // строк вагонов в них (после подмены составом бланка)
	DocOK       int            `json:"doc_ok"`       // памяток с составом из сырого бланка
	DocFallback int            `json:"doc_fallback"` // бланк не достался — вагоны из выжимки
	Applied     int            `json:"applied"`      // рейсов истории обновлено
	Skipped     int            `json:"skipped"`      // строк не легло ни на один рейс
	Reasons     map[string]int `json:"reasons"`      // причины пропуска: код → сколько
}

// FetchByNumber — ручной забор памятки по тройке «клиент + номер + дата
// создания» у конкретного клиента. Пустой client → первый из настроенных
// (reference.clients). Одного номера НЕ достаточно: он повторяется у разных
// портов и переиспользуется внутри одного (11224 был и 01.07, и 24.07), поэтому
// провайдер требует dateCreate — дословное DATE_CREATE из инкремента.
//
// Отдаёт сырой ответ провайдера: это иной контракт, чем у инкремента (обёртка
// ГУ-45 с полем _decoded_doc), в vagon_history он не раскладывается. Ручка
// диагностическая — посмотреть исходный документ целиком.
func (s *ReferenceService) FetchByNumber(ctx context.Context, client, number, dateCreate string) ([]byte, error) {
	client, body, err := s.fetchRawByNumber(ctx, client, number, dateCreate)
	if err != nil {
		return nil, err
	}
	s.log.Info("памятка по номеру получена", logger.Comp(logger.CompPamyatka),
		zap.String("client", client), zap.String("number", number),
		zap.String("date_create", dateCreate), zap.Int("bytes", len(body)))
	return body, nil
}

// FetchExcelByNumber — та же памятка, но бланком ГУ-45 в Excel: сырой ответ
// разбирается [parser.ParseReferenceDocByNumber] (только искомый документ,
// попутчики по пачке не трогаются) и раскладывается по печатной форме.
// Возвращает содержимое книги и имя файла для выгрузки.
func (s *ReferenceService) FetchExcelByNumber(ctx context.Context, client, number, dateCreate string) ([]byte, string, error) {
	client, body, err := s.fetchRawByNumber(ctx, client, number, dateCreate)
	if err != nil {
		return nil, "", err
	}
	doc, err := parser.ParseReferenceDocByNumber(body, client, number)
	if err != nil {
		return nil, "", err
	}
	xlsx, err := report.PamyatkaGU45XLSX(doc)
	if err != nil {
		return nil, "", err
	}
	s.log.Info("памятка выгружена бланком ГУ-45", logger.Comp(logger.CompPamyatka),
		zap.String("client", client), zap.String("number", number),
		zap.Int("vagons", len(doc.Vagons)), zap.Int("bytes", len(xlsx)))
	return xlsx, report.FileName(client, number), nil
}

// fetchRawByNumber — общий забор по номеру: подстановка клиента по умолчанию
// и запрос к провайдеру. Возвращает выбранного клиента (он нужен для разбора:
// номер уникален лишь в его пределах) и тело ответа.
func (s *ReferenceService) fetchRawByNumber(ctx context.Context, client, number, dateCreate string) (string, []byte, error) {
	if client == "" {
		if len(s.clients) == 0 {
			return "", nil, errors.New("reference: клиент не задан и список reference.clients пуст")
		}
		client = s.clients[0]
	}
	body, err := s.cl.ByNumber(ctx, client, number, dateCreate)
	if err != nil {
		return "", nil, err
	}
	return client, body, nil
}

// PullUpdates — крон-инкремент по всем клиентам. Клиенты независимы: ошибка
// одного не прерывает остальных (warn + продолжаем), сводная ошибка возвращается
// после полного прохода.
func (s *ReferenceService) PullUpdates(ctx context.Context) error {
	_, err := s.PullUpdatesDetailed(ctx)
	return err
}

// PullUpdatesDetailed — то же, но с разбором по клиентам (для ручного триггера).
func (s *ReferenceService) PullUpdatesDetailed(ctx context.Context) ([]PamyatkaPullResult, error) {
	var (
		results []PamyatkaPullResult
		failed  []string
	)
	for _, cl := range s.clients {
		res, err := s.pullClient(ctx, cl)
		if err != nil {
			// Утверждение о результате: памятки этого клиента за проход НЕ
			// разобраны — вехи рейсов остались незаполненными.
			s.log.Warn("памятки клиента не забраны", logger.Comp(logger.CompPamyatka),
				zap.String("client", cl), zap.Error(err))
			failed = append(failed, cl)
			continue
		}
		results = append(results, res)
	}
	if len(failed) > 0 {
		return results, fmt.Errorf("reference update: ошибки по клиентам: %s", strings.Join(failed, ", "))
	}
	return results, nil
}

// pullClient — один клиент: забрать с курсора, разобрать, разнести по рейсам,
// сдвинуть курсор.
//
// Курсор двигаем ТОЛЬКО после успешной записи в БД, только на непустое
// LAST_UPDATE (на пустой пачке провайдер отдаёт пустую строку, и запись её в
// курсор потеряла бы позицию) и только вперёд.
func (s *ReferenceService) pullClient(ctx context.Context, client string) (PamyatkaPullResult, error) {
	res := PamyatkaPullResult{Client: client, Reasons: map[string]int{}}

	cursor, err := s.cursors.Get(ctx, client)
	if err != nil {
		return res, fmt.Errorf("reference (%s): чтение курсора: %w", client, err)
	}
	if cursor == "" {
		// Первый заход: курсора нет — берём окно initial_lookback назад.
		//
		// Раньше здесь стоял pull_interval (час), и это заклинивало холодный
		// старт наглухо (бой 27–30.07.2026: 80 тиков подряд по нулям, ни одной
		// вехи в vagon_history). Провайдер публикует памятки ПОЗЖЕ их
		// собственного LAST_UPDATE — на часовом окне ответ пуст всегда, пустой
		// ответ не даёт LAST_UPDATE, курсор не сохраняется, следующий тик снова
		// первый заход. Окно первого захода должно быть заметно глубже
		// задержки публикации провайдера.
		cursor = clock.Now().Time().Add(-s.lookback()).Format(lastUpdateLayout)
	}

	body, err := s.cl.Update(ctx, client, cursor)
	if err != nil {
		return res, err
	}
	upd, err := parser.ParseReferenceUpdate(body, client)
	if err != nil {
		return res, err
	}
	res.Pamyatki = len(upd.Pamyatki)
	if len(upd.Pamyatki) > 0 {
		s.vagonsFromDocs(ctx, client, upd.Pamyatki, &res)
	}
	for _, p := range upd.Pamyatki {
		res.Vagons += len(p.Vagons)
	}

	if len(upd.Pamyatki) > 0 {
		if err := s.applyToHistory(ctx, client, upd.Pamyatki, &res); err != nil {
			return res, err
		}
	}

	// Сдвиг только вперёд: нахлёст возвращает ту же пачку каждый тик, и запись
	// того же значения ничего не меняет, зато advanced отличает «пришло новое»
	// от «повтор нахлёста» — для журнала это разные события.
	var advanced bool
	stored := s.cursorWithOverlap(upd.LastUpdate)
	if stored != "" && stored > cursor {
		if err := s.cursors.Set(ctx, client, stored); err != nil {
			return res, fmt.Errorf("reference (%s): сохранение курсора: %w", client, err)
		}
		advanced = true
	}

	s.log.Info("инкремент памяток разнесён по рейсам", logger.Comp(logger.CompPamyatka),
		zap.String("client", client), zap.String("cursor", cursor),
		zap.String("next_cursor", upd.LastUpdate), zap.String("stored_cursor", stored),
		zap.Bool("advanced", advanced), zap.Int("pamyatki", res.Pamyatki),
		zap.Int("vagons", res.Vagons), zap.Int("doc_ok", res.DocOK),
		zap.Int("doc_fallback", res.DocFallback), zap.Int("applied", res.Applied),
		zap.Int("skipped", res.Skipped), zap.Any("reasons", res.Reasons))

	s.appendJournal(ctx, res, cursor, advanced)
	return res, nil
}

// lookback — глубина окна первого захода. Конфиг подставляет дефолт (48h);
// страховка на нулевое значение — сутки, лишь бы не скатиться к pull_interval.
func (s *ReferenceService) lookback() time.Duration {
	if s.timing.InitialLookback > 0 {
		return s.timing.InitialLookback
	}
	return 24 * time.Hour
}

// cursorWithOverlap — курсор с нахлёстом: храним не сам LAST_UPDATE, а на
// cursor_overlap раньше него. Провайдер публикует записи не в порядке их
// штампов (пачка со штампом 22:50 становится видна после пачки с 23:08) —
// сохрани мы верхний штамп встык, запоздавшая памятка не попала бы в окно уже
// никогда. Перечитать памятку не вредно: разнос идемпотентен (тот же документ
// обновляется на месте, движок решает так же).
func (s *ReferenceService) cursorWithOverlap(lastUpdate string) string {
	if lastUpdate == "" || s.timing.CursorOverlap <= 0 {
		return lastUpdate
	}
	t, err := time.Parse(lastUpdateLayout, lastUpdate)
	if err != nil {
		// Формат провайдера сменился — храним как пришло, инкремент не рвём.
		s.log.Warn("курсор сохранён без нахлёста: LAST_UPDATE не разобран", logger.Comp(logger.CompPamyatka),
			zap.String("last_update", lastUpdate), zap.Error(err))
		return lastUpdate
	}
	return t.Add(-s.timing.CursorOverlap).Format(lastUpdateLayout)
}

// cursorAge — сколько прошло с момента, на котором стоит курсор. Обе величины
// московские naive и однородны: clock.Now() отдаёт то же представление, что
// time.Parse без зоны.
func cursorAge(cursor string) (time.Duration, bool) {
	t, err := time.Parse(lastUpdateLayout, cursor)
	if err != nil {
		return 0, false
	}
	return clock.Now().Time().Sub(t), true
}

// vagonsFromDocs — состав вагонов каждой памятки пачки подменяется составом из
// СЫРОГО бланка (решение владельца 17.08.2026). Выжимка reference/update
// склеивает одноимённые памятки: номера у клиента переиспользуются, и строки
// разных документов с одним номером приходят одной записью — на боевой выборке
// 28.07 чужие строки несли 23% памяток и 18% вагоно-строк, замок по прибытию
// гасил их лишь частично. Бланк адресуется тройкой «клиент + номер +
// DATE_CREATE дословно» и несёт чистый состав того самого документа. Инкремент
// остаётся сигналом «что изменилось» и источником шапки (тип операции, место,
// станция) — движку вех из бланка нужны только вагоны с временами.
//
// Бланк не достался (провайдер не ответил, документа нет в ответе, пустой
// состав) — памятка идёт с вагонами выжимки, как до этой доработки: часть
// чужих строк лучше потерянной памятки. Итог прохода виден в счётчиках
// doc_ok/doc_fallback журнала и лога.
func (s *ReferenceService) vagonsFromDocs(ctx context.Context, client string, pamyatki []domain.Pamyatka, res *PamyatkaPullResult) {
	for i := range pamyatki {
		if i > 0 {
			select {
			case <-ctx.Done():
				return // остаток пачки уйдёт с вагонами выжимки; applyToHistory увидит тот же ctx
			case <-time.After(docFetchPause):
			}
		}
		p := &pamyatki[i]
		body, err := s.cl.ByNumber(ctx, client, p.Number, p.DateCreateRaw)
		if err != nil {
			res.DocFallback++
			s.log.Warn("бланк памятки не достался — вагоны из выжимки", logger.Comp(logger.CompPamyatka),
				zap.String("client", client), zap.String("number", p.Number),
				zap.String("date_create", p.DateCreateRaw), zap.Error(err))
			continue
		}
		doc, err := parser.ParseReferenceDocByNumber(body, client, p.Number)
		if err != nil {
			res.DocFallback++
			s.log.Warn("бланк памятки не разобрался — вагоны из выжимки", logger.Comp(logger.CompPamyatka),
				zap.String("client", client), zap.String("number", p.Number),
				zap.String("date_create", p.DateCreateRaw), zap.Error(err))
			continue
		}
		p.Vagons = domain.PamyatkaVagonsFromDoc(&doc)
		res.DocOK++
	}
}

// applyToHistory — сердце разноса: собрать вагоны пачки, поднять их рейсы,
// решить движком и записать одной транзакцией.
func (s *ReferenceService) applyToHistory(ctx context.Context, client string, pamyatki []domain.Pamyatka, res *PamyatkaPullResult) error {
	vagons := uniqueVagons(pamyatki)
	trips, err := s.history.TripsForPamyatki(ctx, vagons)
	if err != nil {
		return fmt.Errorf("reference (%s): чтение рейсов: %w", client, err)
	}

	// 0 → граница ЖД-суток 18, как date_cutoff_hour обоих источников дислокации
	// (lk: 18 явно, asu: пусто → 18). Появится источник с иным порогом —
	// пробросить сюда его значение, иначе замок разойдётся с записью прибытий.
	applies, skips := domain.ApplyPamyatki(pamyatki, trips, 0)

	updates := make(map[string]map[string]any, len(applies))
	for _, a := range applies {
		updates[a.TripID] = a.Fields
	}
	if err := s.history.UpdateFieldsBatch(ctx, updates); err != nil {
		return fmt.Errorf("reference (%s): запись вех в историю: %w", client, err)
	}

	res.Applied = len(applies)
	res.Skipped = len(skips)
	for _, sk := range skips {
		res.Reasons[sk.Reason]++
	}
	return nil
}

// appendJournal — след в едином журнале событий. Пишем три разных случая
// по-разному, чтобы журнал не забился тиками крона (памятки приходят далеко не
// каждый час), но и молчание не осталось незамеченным:
//   - пришло новое (курсор сдвинулся) — обычная запись прохода;
//   - повтор пачки нахлёста (курсор на месте) — не пишем, нового нет;
//   - пусто И курсор не двигается дольше stale_after — пишем тревогу с полем
//     silent_hours. Именно её не хватило в бою 27–30.07.2026: трое суток
//     пустых тиков выглядели в журнале как тишина, а не как поломка.
func (s *ReferenceService) appendJournal(ctx context.Context, res PamyatkaPullResult, cursor string, advanced bool) {
	if s.journal == nil {
		return
	}
	fields := map[string]any{
		"pamyatki": res.Pamyatki, "vagons": res.Vagons,
		"doc_ok": res.DocOK, "doc_fallback": res.DocFallback,
		"applied": res.Applied, "skipped": res.Skipped, "reasons": res.Reasons,
	}
	switch {
	case res.Pamyatki > 0 && advanced:
		// Обычный проход с новыми данными.
	case res.Pamyatki == 0:
		age, ok := cursorAge(cursor)
		if !ok || s.timing.StaleAfter <= 0 || age < s.timing.StaleAfter {
			return
		}
		fields["cursor"] = cursor
		fields["silent_hours"] = int(age.Hours())
	default:
		return // тот же LAST_UPDATE: пачка нахлёста, писать нечего
	}
	detail, err := json.Marshal(fields)
	if err != nil {
		s.log.Warn("проход памяток не записан в журнал событий", logger.Comp(logger.CompPamyatka), zap.Error(err))
		return
	}
	ev := domain.JournalEvent{
		EventType: domain.EventPamyatkaPull,
		Source:    res.Client,
		Trigger:   domain.TriggerScheduled,
		Actor:     "cron",
		Detail:    detail,
	}
	if err := s.journal.Append(ctx, ev); err != nil {
		// Журнал — не критичный путь: вехи уже записаны, ронять проход из-за
		// него нельзя.
		s.log.Warn("проход памяток не записан в журнал событий", logger.Comp(logger.CompPamyatka), zap.Error(err))
	}
}

// uniqueVagons — номера вагонов пачки без повторов, отсортированы для
// предсказуемого запроса (вагон легко попадает в несколько памяток).
func uniqueVagons(pamyatki []domain.Pamyatka) []string {
	seen := map[string]struct{}{}
	for _, p := range pamyatki {
		for _, v := range p.Vagons {
			if v.Vagon != "" {
				seen[v.Vagon] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

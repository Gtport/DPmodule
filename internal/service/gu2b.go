package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/pkg/logger"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
)

// GU2BService — приём уведомлений ГУ-2б (факт выгрузки) от провайдера:
// крон-инкремент по контракту docs/GU2B.md, накопление в gu2b_notification/
// gu2b_car с контролем полноты сквозной нумерации и — при включённом
// gu2b.apply — уточнение вех выгрузки в vagon_history движком domain.ApplyGU2B.
//
// Устройство — по образцу памяток (ReferenceService): курсор в БД на клиента,
// сдвиг только вперёд и только на непустой LAST_UPDATE, нахлёст cursor_overlap
// на запоздавшие записи, порог тишины stale_after в журнал. Отличия:
//   - уведомления ХРАНИМ (дедуп 72 ч и контроль полноты требуют истории);
//   - первый заход — since=0, полная перезаливка: провайдер копит уведомления
//     с 01.07.2026, и для контроля нумерации нужна вся лента;
//   - ответ страничный (limit) — большая пачка дочитывается в том же тике.
type GU2BService struct {
	cl      port.GU2BClient
	repo    port.GU2BRepository
	history port.HistoryRepository
	journal port.JournalRepository
	dir     *DirectoryCache
	clients []string
	timing  GU2BTiming
	log     *zap.Logger
}

// GU2BTiming — настройки инкремента (config gu2b.*). Отдельной структурой по
// той же причине, что ReferenceTiming: длительности одного типа легко
// перепутать местами, а цена ошибки — молчащий крон.
type GU2BTiming struct {
	Interval      time.Duration // период крон-тика (pull_interval)
	CursorOverlap time.Duration // запас: курсор храним на столько раньше LAST_UPDATE
	StaleAfter    time.Duration // курсор не двигался дольше — пустой проход идёт в журнал
	Limit         int           // уведомлений на страницу инкремента
	Apply         bool          // перезапись вех выгрузки (шаг 3); false — только копим
}

// gu2bMaxPages — потолок страниц за один тик: страховка от зацикливания, если
// провайдер перестанет двигать LAST_UPDATE. Полная перезаливка годового корпуса
// (~900 документов) укладывается в единицы страниц.
const gu2bMaxPages = 50

// gu2bSinceFull — значение since первого захода: полная перезаливка.
const gu2bSinceFull = "0"

func NewGU2BService(
	cl port.GU2BClient,
	repo port.GU2BRepository,
	history port.HistoryRepository,
	journal port.JournalRepository,
	dir *DirectoryCache,
	clients []string,
	timing GU2BTiming,
	log *zap.Logger,
) *GU2BService {
	if log == nil {
		log = zap.NewNop()
	}
	return &GU2BService{
		cl: cl, repo: repo, history: history, journal: journal, dir: dir,
		clients: clients, timing: timing, log: log,
	}
}

// GU2BPullResult — итог одного прохода по клиенту (для ручки и журнала).
type GU2BPullResult struct {
	Client        string         `json:"client"`
	Notifications int            `json:"notifications"` // получено и сохранено уведомлений
	Cars          int            `json:"cars"`          // вагоно-строк в них
	Applied       int            `json:"applied"`       // рейсов истории уточнено (apply включён)
	Skipped       int            `json:"skipped"`       // строк не легло (см. reasons)
	Reasons       map[string]int `json:"reasons"`
	Gaps          []int64        `json:"gaps,omitempty"` // дыры сквозной нумерации (обрезано)
}

// PullUpdates — крон-инкремент по всем клиентам. Клиенты независимы: ошибка
// одного не прерывает остальных.
func (s *GU2BService) PullUpdates(ctx context.Context) error {
	_, err := s.PullUpdatesDetailed(ctx)
	return err
}

// PullUpdatesDetailed — то же, но с разбором по клиентам (для ручного триггера).
func (s *GU2BService) PullUpdatesDetailed(ctx context.Context) ([]GU2BPullResult, error) {
	var (
		results []GU2BPullResult
		failed  []string
	)
	for _, cl := range s.clients {
		res, err := s.pullClient(ctx, cl)
		if err != nil {
			s.log.Warn("уведомления ГУ-2б клиента не забраны", logger.Comp(logger.CompGU2B),
				zap.String("client", cl), zap.Error(err))
			failed = append(failed, cl)
			continue
		}
		results = append(results, res)
	}
	if len(failed) > 0 {
		return results, fmt.Errorf("gu2b update: ошибки по клиентам: %s", strings.Join(failed, ", "))
	}
	return results, nil
}

// pullClient — один клиент: страницы инкремента с курсора до исчерпания,
// сохранение, контроль полноты, движок вех, сдвиг курсора, журнал.
func (s *GU2BService) pullClient(ctx context.Context, client string) (GU2BPullResult, error) {
	res := GU2BPullResult{Client: client, Reasons: map[string]int{}}

	cursor, err := s.repo.GetCursor(ctx, client)
	if err != nil {
		return res, fmt.Errorf("gu2b (%s): чтение курсора: %w", client, err)
	}
	startCursor := cursor
	if cursor == "" {
		// Первый заход — полная перезаливка: контроль нумерации требует всей
		// ленты, а провайдер копит уведомления с 01.07.2026 (~30/сутки — объём
		// смешной). Окно-«lookback», как у памяток, здесь было бы дырой.
		cursor = gu2bSinceFull
	}

	var batch []domain.GU2BNotification
	lastUpdate := ""
	for page := 0; page < gu2bMaxPages; page++ {
		body, err := s.cl.Update(ctx, client, cursor, s.timing.Limit)
		if err != nil {
			return res, err
		}
		upd, err := parser.ParseGU2BUpdate(body, client)
		if err != nil {
			return res, err
		}
		if len(upd.Notifications) == 0 {
			break
		}
		batch = append(batch, upd.Notifications...)
		lastUpdate = upd.LastUpdate
		// Страница неполная — лента дочитана; полная — идём дальше В ЭТОМ тике
		// (иначе перезаливка растянулась бы на часы часовых тиков), курсор
		// страницы двигаем без нахлёста, он внутритиковый.
		if len(upd.Notifications) < s.timing.Limit {
			break
		}
		if upd.LastUpdate == "" || upd.LastUpdate <= cursor {
			// Полная страница без продвижения курсора — контракт сломан;
			// останавливаемся, иначе крутились бы до потолка страниц.
			s.log.Warn("инкремент ГУ-2б остановлен: полная страница не сдвинула LAST_UPDATE",
				logger.Comp(logger.CompGU2B), zap.String("client", client),
				zap.String("cursor", cursor), zap.String("last_update", upd.LastUpdate))
			break
		}
		cursor = upd.LastUpdate
	}

	res.Notifications = len(batch)
	for i := range batch {
		res.Cars += len(batch[i].Cars)
	}

	if len(batch) > 0 {
		if err := s.repo.Upsert(ctx, batch); err != nil {
			return res, fmt.Errorf("gu2b (%s): сохранение уведомлений: %w", client, err)
		}
		s.checkGaps(ctx, client, &res)
		if s.timing.Apply {
			if err := s.applyToHistory(ctx, client, batch, &res); err != nil {
				return res, err
			}
		}
	}

	// Сдвиг курсора — только после успешной записи, только вперёд и с нахлёстом
	// (правила курсора памяток; разнос идемпотентен: upsert + движок скипает
	// повторы и поздние уведомления).
	var advanced bool
	stored := s.cursorWithOverlap(lastUpdate)
	if stored != "" && stored > startCursor {
		if err := s.repo.SetCursor(ctx, client, stored); err != nil {
			return res, fmt.Errorf("gu2b (%s): сохранение курсора: %w", client, err)
		}
		advanced = true
	}

	s.log.Info("инкремент ГУ-2б обработан", logger.Comp(logger.CompGU2B),
		zap.String("client", client), zap.String("cursor", startCursor),
		zap.String("stored_cursor", stored), zap.Bool("advanced", advanced),
		zap.Int("notifications", res.Notifications), zap.Int("cars", res.Cars),
		zap.Bool("apply", s.timing.Apply), zap.Int("applied", res.Applied),
		zap.Int("skipped", res.Skipped), zap.Any("reasons", res.Reasons),
		zap.Int("gaps", len(res.Gaps)))

	s.appendJournal(ctx, res, startCursor, advanced)
	return res, nil
}

// cursorWithOverlap — курсор с нахлёстом (та же мотивировка, что у памяток:
// провайдер публикует записи не в порядке их штампов).
func (s *GU2BService) cursorWithOverlap(lastUpdate string) string {
	if lastUpdate == "" || s.timing.CursorOverlap <= 0 {
		return lastUpdate
	}
	t, err := time.Parse(lastUpdateLayout, lastUpdate)
	if err != nil {
		s.log.Warn("курсор ГУ-2б сохранён без нахлёста: LAST_UPDATE не разобран",
			logger.Comp(logger.CompGU2B), zap.String("last_update", lastUpdate), zap.Error(err))
		return lastUpdate
	}
	return t.Add(-s.timing.CursorOverlap).Format(lastUpdateLayout)
}

// checkGaps — контроль полноты сквозной нумерации: дыра = уведомление, которое
// провайдер так и не отдал (в его собственном корпусе такая была — №1293–1316
// attis, сбор стоял неделю). Дыры не чинятся сами: о них надо просить провайдера
// дозапросить документы из ЭТРАН, поэтому — Warn и поле в журнале.
func (s *GU2BService) checkGaps(ctx context.Context, client string, res *GU2BPullResult) {
	const gapLimit = 20
	gaps, err := s.repo.MissingNumbers(ctx, client, gapLimit)
	if err != nil {
		s.log.Warn("контроль полноты ГУ-2б не выполнен", logger.Comp(logger.CompGU2B),
			zap.String("client", client), zap.Error(err))
		return
	}
	if len(gaps) == 0 {
		return
	}
	res.Gaps = gaps
	nums := make([]string, len(gaps))
	for i, g := range gaps {
		nums[i] = strconv.FormatInt(g, 10)
	}
	s.log.Warn("дыры сквозной нумерации ГУ-2б — уведомления не получены от провайдера",
		logger.Comp(logger.CompGU2B), zap.String("client", client),
		zap.String("numbers", strings.Join(nums, ",")),
		zap.String("подробности", "сами не закроются: просить провайдера дозапросить их из ЭТРАН"))
}

// applyToHistory — движок вех: поднять рейсы вагонов пачки и прошлые события
// (дедуп 72 ч), решить domain.ApplyGU2B, записать одной транзакцией.
func (s *GU2BService) applyToHistory(ctx context.Context, client string, batch []domain.GU2BNotification, res *GU2BPullResult) error {
	vagons := gu2bUniqueVagons(batch)
	if len(vagons) == 0 {
		return nil
	}
	trips, err := s.history.TripsForGU2B(ctx, vagons)
	if err != nil {
		return fmt.Errorf("gu2b (%s): чтение рейсов: %w", client, err)
	}
	prior, err := s.priorEvents(ctx, vagons, batch)
	if err != nil {
		return fmt.Errorf("gu2b (%s): чтение прошлых событий: %w", client, err)
	}

	// 0 → граница ЖД-суток 18, как у замка памяток и date_cutoff_hour источников.
	applies, skips := domain.ApplyGU2B(batch, trips, prior, s.resolveTerminal, 0)

	updates := make(map[string]map[string]any, len(applies))
	for _, a := range applies {
		updates[a.TripID] = a.Fields
	}
	if err := s.history.UpdateFieldsBatch(ctx, updates); err != nil {
		return fmt.Errorf("gu2b (%s): запись вех в историю: %w", client, err)
	}

	res.Applied = len(applies)
	res.Skipped = len(skips)
	for _, sk := range skips {
		res.Reasons[sk.Reason]++
	}
	return nil
}

// priorEvents — принятые ранее события вагонов пачки: окно — 72 ч до самого
// раннего момента выгрузки пачки (раньше дедупу ничего не нужно). Текущая пачка
// уже сохранена Upsert'ом — свои же уведомления отфильтрует движок по
// NotificationID (совпавший ID — не дубль).
func (s *GU2BService) priorEvents(ctx context.Context, vagons []string, batch []domain.GU2BNotification) ([]domain.GU2BPriorEvent, error) {
	var min *domain.LocalTime
	for i := range batch {
		if t := batch[i].EventTime(); t != nil && (min == nil || t.Time().Before(min.Time())) {
			min = t
		}
	}
	if min == nil {
		return nil, nil
	}
	from := domain.LocalTime(min.Time().Add(-72 * time.Hour))
	return s.repo.PriorUnloadEvents(ctx, vagons, from)
}

// resolveTerminal — «ОКПО + станция по имени» → краткое имя причала (как в
// place_vigr). ОКПО выбирает организацию (реестр ports), станция ПО ИМЕНИ
// разводит её терминалы: коды станций документа 5-значные и с настроечными не
// совпадают (боевой вывод анализа корпуса), а ports.location несёт имя станции.
// Кандидат один — станция не проверяется (перепроверять нечем и незачем);
// несколько и станция не разводит — пусто: движок запишет времена, место не
// тронет (лучше без терминала, чем с чужим).
func (s *GU2BService) resolveTerminal(okpo, stationName string) string {
	if s.dir == nil {
		return ""
	}
	n, err := strconv.ParseInt(strings.TrimSpace(okpo), 10, 64)
	if err != nil {
		return ""
	}
	ports, ok := s.dir.PortsByOkpo(n)
	if !ok {
		return ""
	}
	var enabled []domain.Ports
	for _, p := range ports {
		if p.Enabled && p.NameS != "" {
			enabled = append(enabled, p)
		}
	}
	switch len(enabled) {
	case 0:
		return ""
	case 1:
		return enabled[0].NameS
	}
	want := normStation(stationName)
	var found string
	for _, p := range enabled {
		if normStation(p.Location) == want {
			if found != "" {
				return "" // две площадки на одной станции — не угадываем
			}
			found = p.NameS
		}
	}
	return found
}

// normStation — имя станции к сравнимому виду: регистр, обрезка, схлопывание
// внутренних пробелов («МЫС  АСТАФЬЕВА» из свободного текста документа).
func normStation(s string) string {
	return strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(s))), " ")
}

// appendJournal — след в едином журнале (правила трёх случаев — как у памяток:
// новое / повтор нахлёста / тревога тишины).
func (s *GU2BService) appendJournal(ctx context.Context, res GU2BPullResult, cursor string, advanced bool) {
	if s.journal == nil {
		return
	}
	fields := map[string]any{
		"notifications": res.Notifications, "cars": res.Cars,
		"applied": res.Applied, "skipped": res.Skipped, "reasons": res.Reasons,
	}
	if len(res.Gaps) > 0 {
		fields["gaps"] = res.Gaps
	}
	switch {
	case res.Notifications > 0 && advanced:
		// Обычный проход с новыми данными.
	case res.Notifications == 0:
		age, ok := cursorAge(cursor)
		if !ok || s.timing.StaleAfter <= 0 || age < s.timing.StaleAfter {
			return
		}
		fields["cursor"] = cursor
		fields["silent_hours"] = int(age.Hours())
	default:
		return // повтор пачки нахлёста, писать нечего
	}
	detail, err := json.Marshal(fields)
	if err != nil {
		s.log.Warn("проход ГУ-2б не записан в журнал событий", logger.Comp(logger.CompGU2B), zap.Error(err))
		return
	}
	ev := domain.JournalEvent{
		EventType: domain.EventGU2BPull,
		Source:    res.Client,
		Trigger:   domain.TriggerScheduled,
		Actor:     "cron",
		Detail:    detail,
	}
	if err := s.journal.Append(ctx, ev); err != nil {
		s.log.Warn("проход ГУ-2б не записан в журнал событий", logger.Comp(logger.CompGU2B), zap.Error(err))
	}
}

// gu2bUniqueVagons — номера вагонов пачки без повторов, отсортированы для
// предсказуемых запросов.
func gu2bUniqueVagons(batch []domain.GU2BNotification) []string {
	seen := map[string]struct{}{}
	for i := range batch {
		for _, c := range batch[i].Cars {
			if c.Vagon != "" {
				seen[c.Vagon] = struct{}{}
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

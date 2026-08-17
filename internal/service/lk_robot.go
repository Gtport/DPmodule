package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/pkg/logger"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
)

// ErrNoLKAccounts — в таблице lk_account нет включённых аккаунтов: выгружать
// некому (робот не настроен).
var ErrNoLKAccounts = errors.New("не заведены аккаунты ЛК РЖД")

// ErrRobotBusy — забор уже идёт. Второй запуск отклоняем: кабинет РЖД не любит
// параллельных заказов одного отчёта, а два батча пересобирали бы снимок наперегонки.
var ErrRobotBusy = errors.New("забор из ЛК уже идёт")

// LKTable — готовый отчёт кабинета: имена полей АСОУП (head), строки значений
// (body) и метка формирования среза как её отдал кабинет («03.08.2026 02:21»).
type LKTable struct {
	Head      []string
	Body      [][]json.RawMessage
	CreatedAt string
}

// LKFetcher — забор дислокации из личного кабинета РЖД по одному аккаунту.
// Реализация — adapter/lkrobot; сервис про HTTP ничего не знает.
type LKFetcher interface {
	Login(ctx context.Context, login, password string) error
	FetchTable(ctx context.Context, okpo string) (LKTable, error)
}

// Состояния одного потока в ходе запуска (для живого прогресса в модалке).
const (
	LKRobotWait = "wait" // пароль введён, очередь ещё не дошла
	LKRobotRun  = "run"  // сейчас работаем с этим кабинетом
	LKRobotOK   = "ok"   // данные получены и разобраны
	LKRobotFail = "fail" // поток отвалился (см. Error)
)

// Стадии запуска целиком.
const (
	LKRobotStageIdle    = "idle"    // запусков не было (или сервер перезапущен)
	LKRobotStageFetch   = "fetch"   // ходим по кабинетам
	LKRobotStageProcess = "process" // данные собраны, пересобираем дислокацию
	LKRobotStageDone    = "done"    // всё закончилось (успехом или нет)
)

// LKRobot — автовыгрузка дислокации из ЛК вместо ручной работы диспетчера.
//
// Логины берём из настроечной таблицы (lk_account), пароль приходит в запросе
// от диспетчера и нигде не сохраняется. Дислокацию читаем прямо из ответа
// кабинета (решение владельца 03.08.2026): xlsx не качаем и в приём ничего не
// кладём, записи идут в общий конвейер одним батчем. Ручная загрузка файлов
// работает как прежде — это отдельный путь для человека.
//
// ⚠️ Запуск ФОНОВЫЙ (решение владельца 01.08.2026). Забор по двум кабинетам
// занимает около минуты, а по медленному — до нескольких: держать всё это время
// открытым HTTP-запрос нельзя. Любой обратный прокси (nginx на VPS, ingress в
// будущем контейнерном стенде) рвёт долгий ответ по своему таймауту, и запуск
// выглядел «упавшим», хотя файлы уже лежали в приёме. Поэтому ручка запуска
// отвечает сразу, работа идёт в горутине, а модалка опрашивает состояние.
// Требований к таймаутам прокси у нас больше нет.
type LKRobot struct {
	accounts port.LKAccountRepository
	intake   *LKIntake
	newFetch func() (LKFetcher, error)
	timeout  time.Duration
	log      *zap.Logger

	// Шаг 2 (пересборка снимка). Появляется только при наличии БД, поэтому
	// ставится отдельно, после сборки процессора (см. server.go).
	proc *LKProcessor

	mu  sync.Mutex
	job *lkRobotJob
}

func NewLKRobot(accounts port.LKAccountRepository, intake *LKIntake, newFetch func() (LKFetcher, error), timeout time.Duration, log *zap.Logger) *LKRobot {
	return &LKRobot{accounts: accounts, intake: intake, newFetch: newFetch, timeout: timeout, log: log}
}

// SetProcessor подключает шаг 2: после успешного забора робот сам пересобирает
// дислокацию (решение владельца — диспетчеру остаётся один клик вместо двух).
func (s *LKRobot) SetProcessor(p *LKProcessor) { s.proc = p }

// lkRobotJob — состояние текущего (или последнего) запуска. Живёт в памяти:
// перезапуск сервера состояние теряет, и это честно — сам забор он тоже
// прерывает (и снимок в этом случае просто остаётся прежним).
type lkRobotJob struct {
	running     bool
	stage       string
	startedAt   domain.LocalTime
	finishedAt  *domain.LocalTime
	actor       string
	items       []LKRobotItem
	processed   *LKProcessResult
	processSkip string
	processErr  string
}

// lkRobotTask — один поток к выгрузке: аккаунт + введённый на этот запуск пароль.
type lkRobotTask struct {
	acc domain.LKAccount
	pwd string
}

// LKRobotAccount — аккаунт для фронта: по нему модалка рисует поле пароля.
// Логин отдаём — диспетчер должен видеть, под кем пойдёт запрос.
type LKRobotAccount struct {
	OKPO  int64  `json:"okpo"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// LKRobotItem — результат по одному потоку. Ошибка одного порта не отменяет
// остальные, поэтому она едет в состоянии строкой, а не рушит весь запуск.
type LKRobotItem struct {
	OKPO         int64             `json:"okpo"`
	Name         string            `json:"name"`
	State        string            `json:"state"`
	Organisation string            `json:"organisation,omitempty"`
	Terminals    []string          `json:"terminals,omitempty"`
	Rows         int               `json:"rows,omitempty"`
	FormationTS  *domain.LocalTime `json:"formation_ts,omitempty"` // метка среза кабинета
	Error        string            `json:"error,omitempty"`
}

// LKRobotState — полный снимок для модалки: чем занят робот, что вышло по
// каждому потоку и чем кончилось обновление дислокации. Одним ответом, чтобы
// опрос был одним запросом. Файлов здесь нет — робот их не создаёт (данные
// берутся из ответа кабинета), а файлы ручного приёма показывает своя модалка.
type LKRobotState struct {
	Running    bool              `json:"running"`
	Stage      string            `json:"stage"`
	StartedAt  *domain.LocalTime `json:"started_at,omitempty"`
	FinishedAt *domain.LocalTime `json:"finished_at,omitempty"`
	Actor      string            `json:"actor,omitempty"`
	Items      []LKRobotItem     `json:"items"`
	OK         int               `json:"ok"`
	Failed     int               `json:"failed"`
	// Итог шага 2: либо сводка пересборки, либо причина, почему её не было.
	Processed    *LKProcessResult `json:"processed,omitempty"`
	ProcessSkip  string           `json:"process_skip,omitempty"`
	ProcessError string           `json:"process_error,omitempty"`
}

// Accounts — включённые аккаунты (для модалки ввода паролей).
func (s *LKRobot) Accounts(ctx context.Context) ([]LKRobotAccount, error) {
	accs, err := s.accounts.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	if len(accs) == 0 {
		return nil, ErrNoLKAccounts
	}
	out := make([]LKRobotAccount, 0, len(accs))
	for _, a := range accs {
		out = append(out, LKRobotAccount{OKPO: a.OKPO, Login: a.Login, Name: a.Name})
	}
	return out, nil
}

// Start ставит забор в работу и СРАЗУ возвращает состояние: HTTP-запрос не ждёт
// ни кабинета РЖД, ни пересборки снимка. passwords: ОКПО → пароль; аккаунты без
// пароля пропускаются молча (диспетчер мог обновить только один поток).
func (s *LKRobot) Start(ctx context.Context, passwords map[int64]string) (LKRobotState, error) {
	accs, err := s.accounts.Accounts(ctx)
	if err != nil {
		return LKRobotState{}, err
	}
	if len(accs) == 0 {
		return LKRobotState{}, ErrNoLKAccounts
	}

	tasks := make([]lkRobotTask, 0, len(accs))
	items := make([]LKRobotItem, 0, len(accs))
	for _, acc := range accs {
		pwd, ok := passwords[acc.OKPO]
		if !ok || pwd == "" {
			continue
		}
		tasks = append(tasks, lkRobotTask{acc: acc, pwd: pwd})
		items = append(items, LKRobotItem{OKPO: acc.OKPO, Name: acc.Name, State: LKRobotWait})
	}
	if len(tasks) == 0 {
		return LKRobotState{}, ErrNoLKAccounts
	}

	s.mu.Lock()
	if s.job != nil && s.job.running {
		s.mu.Unlock()
		return LKRobotState{}, ErrRobotBusy
	}
	s.job = &lkRobotJob{
		running:   true,
		stage:     LKRobotStageFetch,
		startedAt: clock.Now(),
		actor:     actorFromContext(ctx),
		items:     items,
	}
	s.mu.Unlock()

	// Контекст запуска ОТВЯЗАН от HTTP-запроса: тот завершится через миллисекунды,
	// а его отмена оборвала бы забор на середине. Claims переносим руками — иначе
	// журнал обновления дислокации потеряет «кто» и напишет пустого актора.
	runCtx, cancel := context.WithTimeout(
		auth.WithClaims(context.Background(), auth.ClaimsFromContext(ctx)), s.timeout)
	go func() {
		defer cancel()
		s.run(runCtx, tasks)
	}()

	return s.State(), nil
}

// State — снимок состояния для модалки (копия: горутина продолжает править своё).
func (s *LKRobot) State() LKRobotState {
	st := LKRobotState{Stage: LKRobotStageIdle, Items: []LKRobotItem{}}

	s.mu.Lock()
	if j := s.job; j != nil {
		st.Running, st.Stage, st.Actor = j.running, j.stage, j.actor
		started := j.startedAt
		st.StartedAt = &started
		if j.finishedAt != nil {
			fin := *j.finishedAt
			st.FinishedAt = &fin
		}
		st.Items = append([]LKRobotItem(nil), j.items...)
		st.Processed, st.ProcessSkip, st.ProcessError = j.processed, j.processSkip, j.processErr
	}
	s.mu.Unlock()

	for _, it := range st.Items {
		switch it.State {
		case LKRobotOK:
			st.OK++
		case LKRobotFail:
			st.Failed++
		}
	}
	return st
}

// run — весь запуск целиком в фоне: потоки по очереди, затем обновление снимка.
// Записи всех кабинетов копятся в памяти и идут в конвейер ОДНИМ батчем: снимок
// дислокации полный, а не собирается по кускам.
func (s *LKRobot) run(ctx context.Context, tasks []lkRobotTask) {
	all := make([]domain.Dislocation, 0, 8192)
	files := make([]LKFileInfo, 0, len(tasks))
	perStream := make(map[string]int, len(tasks))

	for i, t := range tasks {
		s.update(func(j *lkRobotJob) { j.items[i].State = LKRobotRun })

		item, recs := s.runOne(ctx, t.acc, t.pwd)
		if item.Error == "" {
			item.State = LKRobotOK
			all = append(all, recs...)
			files = append(files, lkFileInfoFromItem(item))
			perStream[item.Organisation] = len(recs)
		} else {
			item.State = LKRobotFail
		}
		s.update(func(j *lkRobotJob) { j.items[i] = item })
	}

	if len(files) > 0 {
		s.processSnapshot(ctx, all, files, perStream)
	} else {
		s.update(func(j *lkRobotJob) {
			j.processSkip = "ни один кабинет не отдал данные — обновлять нечем"
		})
	}

	s.update(func(j *lkRobotJob) {
		j.running = false
		j.stage = LKRobotStageDone
		fin := clock.Now()
		j.finishedAt = &fin
	})
}

// lkFileInfoFromItem — описание потока для гардов и журнала. Файла на диске нет,
// поэтому Filename пустой: ключ потока — ОКПО.
func lkFileInfoFromItem(it LKRobotItem) LKFileInfo {
	fi := LKFileInfo{
		Okpo:         strconv.FormatInt(it.OKPO, 10),
		Organisation: it.Organisation,
		Terminals:    it.Terminals,
	}
	if it.FormationTS != nil {
		fi.FormationTS = *it.FormationTS
		fi.AgeMinutes = int(clock.Now().Time().Sub(it.FormationTS.Time()).Minutes())
	}
	return fi
}

// processSnapshot — шаг 2 сразу за забором: тот же конвейер, что по кнопке
// «Обновить дислокацию» в ручном приёме, но над записями из памяти. Контроль
// комплекта и гарды свежести — внутри ProcessBatch: не прошло — причина едет
// в состояние словами, снимок не трогается.
func (s *LKRobot) processSnapshot(ctx context.Context, all []domain.Dislocation, files []LKFileInfo, perStream map[string]int) {
	if s.proc == nil {
		s.update(func(j *lkRobotJob) { j.processSkip = "обновление дислокации недоступно" })
		return
	}

	s.update(func(j *lkRobotJob) { j.stage = LKRobotStageProcess })
	res, err := s.proc.ProcessBatch(ctx, "lk_robot", all, files, perStream)
	if err != nil {
		s.update(func(j *lkRobotJob) { j.processErr = err.Error() })
		if s.log != nil {
			s.log.Warn("дислокация не обновлена по итогам работы робота", logger.Comp(logger.CompDislocation), zap.Error(err))
		}
		return
	}
	s.update(func(j *lkRobotJob) { j.processed = &res })
}

// update — правка состояния под мьютексом (горутина пишет, ручка читает).
func (s *LKRobot) update(fn func(j *lkRobotJob)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil {
		fn(s.job)
	}
}

// runOne — один поток целиком: вход в кабинет, забор таблицы, разбор в записи
// дислокации. Файл не качаем и в приём ничего не кладём (решение владельца
// 03.08.2026): ответ кабинета уже несёт все строки и все нужные поля. Ручная
// загрузка xlsx живёт своей жизнью и этим путём не затрагивается.
func (s *LKRobot) runOne(ctx context.Context, acc domain.LKAccount, password string) (LKRobotItem, []domain.Dislocation) {
	item := LKRobotItem{OKPO: acc.OKPO, Name: acc.Name}

	fetcher, err := s.newFetch()
	if err != nil {
		item.Error = err.Error()
		return item, nil
	}
	if err := fetcher.Login(ctx, acc.Login, password); err != nil {
		item.Error = err.Error()
		s.logf("вход в ЛК не выполнен", acc, err)
		return item, nil
	}
	// ОКПО юрлица — восьмизначный, и кабинет требует его именно так: у НМТП
	// (01126022) без ведущего нуля заказ отчёта отвергается с 422. В базе ОКПО
	// хранится числом (связь с ports.okpo), поэтому дополняем нулём здесь.
	// Десятизначные ОКПО (ИП) длиннее восьми — их дополнение не трогает.
	okpo := fmt.Sprintf("%08d", acc.OKPO)
	table, err := fetcher.FetchTable(ctx, okpo)
	if err != nil {
		item.Error = err.Error()
		s.logf("выгрузка из ЛК не удалась", acc, err)
		return item, nil
	}

	recs, err := parser.NewJSONParser().ParseRows(table.Head, table.Body)
	if err != nil {
		item.Error = err.Error()
		s.logf("разбор ответа ЛК не удался", acc, err)
		return item, nil
	}
	if len(recs) == 0 {
		item.Error = "кабинет вернул отчёт без вагонов"
		return item, nil
	}
	item.Rows = len(recs)

	// Метка формирования среза — та же, что приём вычитывал из шапки xlsx.
	// Без неё гарды свежести работать не смогут, поэтому её отсутствие — отказ
	// потока, а не молчаливый ноль.
	ts, ok := parseFormationTS(table.CreatedAt)
	if !ok {
		item.Error = fmt.Sprintf("кабинет не вернул метку формирования (%q)", table.CreatedAt)
		return item, nil
	}
	item.FormationTS = &ts

	// «Чей поток»: ОКПО должен быть в справочнике ports (как при приёме файла).
	terminals, ok := s.intake.dir.PortsByOkpo(acc.OKPO)
	if !ok || len(terminals) == 0 {
		item.Error = fmt.Sprintf("%v: %d", ErrUnknownOkpo, acc.OKPO)
		return item, nil
	}
	item.Organisation = terminals[0].Organisation
	for _, t := range terminals {
		item.Terminals = append(item.Terminals, t.NameS)
	}
	return item, recs
}

func (s *LKRobot) logf(msg string, acc domain.LKAccount, err error) {
	if s.log == nil {
		return
	}
	// Пароль не логируем нигде и никогда — только кто и по какому потоку.
	s.log.Warn(msg, logger.Comp(logger.CompDislocation),
		zap.Int64("okpo", acc.OKPO),
		zap.String("login", acc.Login),
		zap.Error(err))
}

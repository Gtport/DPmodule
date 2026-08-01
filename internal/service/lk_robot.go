package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// ErrNoLKAccounts — в таблице lk_account нет включённых аккаунтов: выгружать
// некому (робот не настроен).
var ErrNoLKAccounts = errors.New("не заведены аккаунты ЛК РЖД")

// ErrRobotBusy — забор уже идёт. Второй запуск отклоняем: кабинет РЖД не любит
// параллельных заказов одного отчёта, да и два потока писали бы в одну папку.
var ErrRobotBusy = errors.New("забор из ЛК уже идёт")

// LKFetcher — забор дислокации из личного кабинета РЖД по одному аккаунту.
// Реализация — adapter/lkrobot; сервис про HTTP ничего не знает.
type LKFetcher interface {
	Login(ctx context.Context, login, password string) error
	Fetch(ctx context.Context, okpo string) ([]byte, int, error)
}

// Состояния одного потока в ходе запуска (для живого прогресса в модалке).
const (
	LKRobotWait = "wait" // пароль введён, очередь ещё не дошла
	LKRobotRun  = "run"  // сейчас работаем с этим кабинетом
	LKRobotOK   = "ok"   // файл принят
	LKRobotFail = "fail" // поток отвалился (см. Error)
)

// Стадии запуска целиком.
const (
	LKRobotStageIdle    = "idle"    // запусков не было (или сервер перезапущен)
	LKRobotStageFetch   = "fetch"   // ходим по кабинетам
	LKRobotStageProcess = "process" // файлы приняты, пересобираем дислокацию
	LKRobotStageDone    = "done"    // всё закончилось (успехом или нет)
)

// LKRobot — автовыгрузка дислокации из ЛК вместо ручной работы диспетчера.
//
// Логины берём из настроечной таблицы (lk_account), пароль приходит в запросе
// от диспетчера и нигде не сохраняется. Полученный файл идёт в тот же приём,
// что и ручная загрузка (LKIntake), а следом — то же обновление дислокации,
// что диспетчер жал руками вторым шагом.
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
// прерывает. Файлы при этом остаются в приёме, их видно обычным списком.
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
	OKPO         int64  `json:"okpo"`
	Name         string `json:"name"`
	State        string `json:"state"`
	Organisation string `json:"organisation,omitempty"`
	Filename     string `json:"filename,omitempty"`
	Rows         int    `json:"rows,omitempty"`
	Error        string `json:"error,omitempty"`
}

// LKRobotState — полный снимок для модалки: чем занят робот, что вышло по
// каждому потоку, что лежит в приёме и чем кончилось обновление дислокации.
// Одним ответом, чтобы опрос был одним запросом.
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
	// Приём как он есть сейчас (файлы + контроль) — тот же, что у ручной загрузки.
	Files LKStatus `json:"files"`
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
	// Приём читаем вне мьютекса: это папка на диске, к состоянию запуска отношения
	// не имеет и нужна модалке всегда — в том числе когда запусков не было.
	if files, err := s.intake.Status(); err == nil {
		st.Files = files
	}
	return st
}

// run — весь запуск целиком в фоне: потоки по очереди, затем обновление снимка.
func (s *LKRobot) run(ctx context.Context, tasks []lkRobotTask) {
	okCount := 0
	for i, t := range tasks {
		s.update(func(j *lkRobotJob) { j.items[i].State = LKRobotRun })

		item := s.runOne(ctx, t.acc, t.pwd)
		if item.Error == "" {
			item.State = LKRobotOK
			okCount++
		} else {
			item.State = LKRobotFail
		}
		s.update(func(j *lkRobotJob) { j.items[i] = item })
	}

	if okCount > 0 {
		s.processSnapshot(ctx)
	} else {
		s.update(func(j *lkRobotJob) { j.processSkip = "ни один файл не получен — обновлять нечем" })
	}

	s.update(func(j *lkRobotJob) {
		j.running = false
		j.stage = LKRobotStageDone
		fin := clock.Now()
		j.finishedAt = &fin
	})
}

// processSnapshot — шаг 2 сразу за забором: та же обработка, что по кнопке
// «Обновить дислокацию» в ручном приёме. Контроль приёма (свежесть, полнота,
// разрыв срезов) проверяем ДО запуска: не готово — говорим об этом словами,
// а не ошибкой обработки.
func (s *LKRobot) processSnapshot(ctx context.Context) {
	if s.proc == nil {
		s.update(func(j *lkRobotJob) { j.processSkip = "обновление дислокации недоступно" })
		return
	}
	st, err := s.intake.Status()
	if err != nil {
		s.update(func(j *lkRobotJob) { j.processErr = err.Error() })
		return
	}
	if !st.Ready {
		s.update(func(j *lkRobotJob) { j.processSkip = "приём не прошёл контроль — дислокация не обновлена" })
		return
	}

	s.update(func(j *lkRobotJob) { j.stage = LKRobotStageProcess })
	res, err := s.proc.Process(ctx)
	if err != nil {
		s.update(func(j *lkRobotJob) { j.processErr = err.Error() })
		if s.log != nil {
			s.log.Warn("ЛК-робот: обновление дислокации не выполнено", zap.Error(err))
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

// runOne — один поток целиком: вход в кабинет, выгрузка, приём файла.
func (s *LKRobot) runOne(ctx context.Context, acc domain.LKAccount, password string) LKRobotItem {
	item := LKRobotItem{OKPO: acc.OKPO, Name: acc.Name}

	fetcher, err := s.newFetch()
	if err != nil {
		item.Error = err.Error()
		return item
	}
	if err := fetcher.Login(ctx, acc.Login, password); err != nil {
		item.Error = err.Error()
		s.logf("вход в ЛК не выполнен", acc, err)
		return item
	}
	// ОКПО юрлица — восьмизначный, и кабинет требует его именно так: у НМТП
	// (01126022) без ведущего нуля заказ отчёта отвергается с 422. В базе ОКПО
	// хранится числом (связь с ports.okpo), поэтому дополняем нулём здесь.
	// Десятизначные ОКПО (ИП) длиннее восьми — их дополнение не трогает.
	okpo := fmt.Sprintf("%08d", acc.OKPO)
	file, rows, err := fetcher.Fetch(ctx, okpo)
	if err != nil {
		item.Error = err.Error()
		s.logf("выгрузка из ЛК не удалась", acc, err)
		return item
	}
	item.Rows = rows

	// Имя роли не играет: приём определяет ОКПО и метку формирования из самого
	// файла. Расширение обязано быть разрешённым (allowed_ext источника 'lk').
	stored, err := s.intake.Store(okpo+".xlsx", file)
	if err != nil {
		item.Error = err.Error()
		s.logf("приём файла из ЛК отклонён", acc, err)
		return item
	}
	item.Filename = stored.Filename
	item.Organisation = stored.Organisation
	return item
}

func (s *LKRobot) logf(msg string, acc domain.LKAccount, err error) {
	if s.log == nil {
		return
	}
	// Пароль не логируем нигде и никогда — только кто и по какому потоку.
	s.log.Warn(msg,
		zap.Int64("okpo", acc.OKPO),
		zap.String("login", acc.Login),
		zap.Error(err))
}

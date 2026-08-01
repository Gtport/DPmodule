package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// ErrNoLKAccounts — в таблице lk_account нет включённых аккаунтов: выгружать
// некому (робот не настроен).
var ErrNoLKAccounts = errors.New("не заведены аккаунты ЛК РЖД")

// LKFetcher — забор дислокации из личного кабинета РЖД по одному аккаунту.
// Реализация — adapter/lkrobot; сервис про HTTP ничего не знает.
type LKFetcher interface {
	Login(ctx context.Context, login, password string) error
	Fetch(ctx context.Context, okpo string) ([]byte, int, error)
}

// LKRobot — автовыгрузка дислокации из ЛК вместо ручной работы диспетчера.
//
// Логины берём из настроечной таблицы (lk_account), пароль приходит в запросе
// от диспетчера и нигде не сохраняется. Полученный файл идёт в тот же приём,
// что и ручная загрузка (LKIntake), поэтому дальше всё работает как раньше:
// диспетчер видит файлы в модалке «Приём ЛК» и жмёт «Обработать».
type LKRobot struct {
	accounts port.LKAccountRepository
	intake   *LKIntake
	newFetch func() (LKFetcher, error)
	log      *zap.Logger
}

func NewLKRobot(accounts port.LKAccountRepository, intake *LKIntake, newFetch func() (LKFetcher, error), log *zap.Logger) *LKRobot {
	return &LKRobot{accounts: accounts, intake: intake, newFetch: newFetch, log: log}
}

// LKRobotAccount — аккаунт для фронта: по нему модалка рисует поле пароля.
// Логин отдаём — диспетчер должен видеть, под кем пойдёт запрос.
type LKRobotAccount struct {
	OKPO  int64  `json:"okpo"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// LKRobotItem — результат по одному потоку. Ошибка одного порта не отменяет
// остальные, поэтому она едет в ответе строкой, а не рушит весь запуск.
type LKRobotItem struct {
	OKPO         int64  `json:"okpo"`
	Name         string `json:"name"`
	Organisation string `json:"organisation,omitempty"`
	Filename     string `json:"filename,omitempty"`
	Rows         int    `json:"rows,omitempty"`
	Error        string `json:"error,omitempty"`
}

// LKRobotResult — сводка запуска.
type LKRobotResult struct {
	Items  []LKRobotItem `json:"items"`
	OK     int           `json:"ok"`
	Failed int           `json:"failed"`
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

// Run — выгрузка по всем аккаунтам, для которых диспетчер ввёл пароль.
// passwords: ОКПО → пароль. Аккаунты без пароля пропускаются молча (диспетчер
// мог обновить только один поток).
func (s *LKRobot) Run(ctx context.Context, passwords map[int64]string) (LKRobotResult, error) {
	accs, err := s.accounts.Accounts(ctx)
	if err != nil {
		return LKRobotResult{}, err
	}
	if len(accs) == 0 {
		return LKRobotResult{}, ErrNoLKAccounts
	}

	res := LKRobotResult{Items: make([]LKRobotItem, 0, len(accs))}
	for _, acc := range accs {
		pwd, ok := passwords[acc.OKPO]
		if !ok || pwd == "" {
			continue
		}
		item := s.runOne(ctx, acc, pwd)
		if item.Error == "" {
			res.OK++
		} else {
			res.Failed++
		}
		res.Items = append(res.Items, item)
	}
	if len(res.Items) == 0 {
		return res, ErrNoLKAccounts
	}
	return res, nil
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

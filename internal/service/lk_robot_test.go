package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// Тесты фонового запуска робота ЛК. Проверяем ровно то, ради чего он стал
// фоновым: ручка отвечает НЕ ДОЖИДАЯСЬ кабинета, работа доходит до конца сама,
// а прогресс и итог видны через State().

// fakeLKAccounts — таблица lk_account в памяти.
type fakeLKAccounts struct{ accs []domain.LKAccount }

func (f *fakeLKAccounts) Accounts(context.Context) ([]domain.LKAccount, error) {
	return f.accs, nil
}

// fakeFetcher — кабинет РЖД: отдаёт готовую таблицу отчёта по своему ОКПО.
// release держит выдачу, пока тест не разрешит — так проверяется, что запуск
// не ждёт кабинета.
type fakeFetcher struct {
	table    service.LKTable
	release  <-chan struct{}
	loginErr error
}

func (f *fakeFetcher) Login(context.Context, string, string) error { return f.loginErr }

func (f *fakeFetcher) FetchTable(ctx context.Context, _ string) (service.LKTable, error) {
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return service.LKTable{}, ctx.Err()
		}
	}
	return f.table, nil
}

// lkTable — ответ кабинета в том виде, в каком его отдаёт живой ЛК: имена полей
// АСОУП, значения строками, метка формирования среза в created_at.
func lkTable(okpo, createdAt, vagon string) service.LKTable {
	return service.LKTable{
		Head: []string{"NOM_VAG", "GRUZPOL_OKPO", "STAN_NAZN", "STAN_OP", "DATE_OP", "DATE_NACH", "STAN_NACH"},
		Body: [][]json.RawMessage{{
			json.RawMessage(`"` + vagon + `"`),
			json.RawMessage(`"` + okpo + `"`),
			json.RawMessage(`"985702"`),
			json.RawMessage(`"985702"`),
			json.RawMessage(`"2026-07-02T05:30:00.000"`),
			json.RawMessage(`"2026-06-28T10:00:00.000"`),
			json.RawMessage(`"984700"`),
		}},
		CreatedAt: createdAt,
	}
}

// newRobot собирает робота поверх того же процессора, что и ручной приём: данные,
// которые робот забрал из кабинета, обязаны пройти те же гарды и тот же конвейер.
func newRobot(t *testing.T, repo *fakeDislRepo, fetchers map[int64]*fakeFetcher) (*service.LKRobot, string) {
	t.Helper()
	intake, dir := newIntake(t)
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))
	proc := service.NewLKProcessor(intake, repo, actual, s9c(t, newFakeStatus9()), s6c(t, newFakeStatus6()), newFakeHistory())

	accounts := &fakeLKAccounts{accs: []domain.LKAccount{
		{OKPO: 10230304, Login: "ae", Name: "АЭ"},
		{OKPO: 1126022, Login: "nmtp", Name: "НМТП"},
	}}
	// Очередь выдачи клиентов: потоки идут строго по порядку аккаунтов.
	order := []int64{10230304, 1126022}
	i := 0
	newFetch := func() (service.LKFetcher, error) {
		f := fetchers[order[i]]
		i++
		return f, nil
	}

	robot := service.NewLKRobot(accounts, intake, newFetch, time.Minute, nil)
	robot.SetProcessor(proc)
	return robot, dir
}

// waitDone ждёт завершения фонового запуска (или падает по таймауту теста).
func waitDone(t *testing.T, robot *service.LKRobot) service.LKRobotState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := robot.State()
		if !st.Running && st.Stage == service.LKRobotStageDone {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("запуск не завершился: stage=%s", st.Stage)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Оба кабинета отдали файлы → приём полный → робот сам обновляет дислокацию.
func TestLKRobot_RunFetchesAndProcesses(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	repo := &fakeDislRepo{}
	robot, _ := newRobot(t, repo, map[int64]*fakeFetcher{
		10230304: {table: lkTable("10230304", "02.07.2026 06:00", "52275476")},
		1126022:  {table: lkTable("01126022", "02.07.2026 06:05", "52275477")},
	})

	_, err := robot.Start(context.Background(), map[int64]string{10230304: "p1", 1126022: "p2"})
	require.NoError(t, err)

	st := waitDone(t, robot)
	assert.Equal(t, 2, st.OK)
	assert.Equal(t, 0, st.Failed)
	require.NotNil(t, st.Processed, "дислокация должна обновиться сразу за забором: %s%s", st.ProcessSkip, st.ProcessError)
	assert.Equal(t, 2, st.Processed.Files)
	assert.Equal(t, 2, st.Processed.Count)
	assert.Equal(t, 1, repo.calls) // снимок заменён один раз
	// Метка формирования среза едет из ответа кабинета (created_at) — на ней
	// стоят гарды свежести и doc_ts журнала.
	require.NotNil(t, st.Items[0].FormationTS)
	assert.Equal(t, "2026-07-02T06:00:00", st.Items[0].FormationTS.String())
}

// Ручка запуска не ждёт кабинета: Start возвращается мгновенно, пока выгрузка висит.
func TestLKRobot_StartDoesNotWait(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	release := make(chan struct{})
	repo := &fakeDislRepo{}
	robot, _ := newRobot(t, repo, map[int64]*fakeFetcher{
		10230304: {table: lkTable("10230304", "02.07.2026 06:00", "52275476"), release: release},
		1126022:  {table: lkTable("01126022", "02.07.2026 06:05", "52275477")},
	})

	start := time.Now()
	st, err := robot.Start(context.Background(), map[int64]string{10230304: "p1", 1126022: "p2"})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second, "запуск обязан отвечать сразу, не дожидаясь ЛК")
	assert.True(t, st.Running)
	assert.Equal(t, service.LKRobotStageFetch, st.Stage)

	// Пока кабинет молчит — второй запуск отклоняем.
	_, err = robot.Start(context.Background(), map[int64]string{10230304: "p1"})
	require.True(t, errors.Is(err, service.ErrRobotBusy))

	close(release)
	st = waitDone(t, robot)
	assert.Equal(t, 2, st.OK)
	require.NotNil(t, st.Processed)
}

// Один кабинет не пустил: его поток помечен ошибкой, комплект неполон → снимок
// не трогаем и говорим словами, почему (полснимка — это не «частичное обновление»).
func TestLKRobot_PartialFailureKeepsSnapshot(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	repo := &fakeDislRepo{}
	robot, _ := newRobot(t, repo, map[int64]*fakeFetcher{
		10230304: {table: lkTable("10230304", "02.07.2026 06:00", "52275476")},
		1126022:  {loginErr: errors.New("ЛК РЖД: вход не выполнен")},
	})

	_, err := robot.Start(context.Background(), map[int64]string{10230304: "p1", 1126022: "bad"})
	require.NoError(t, err)

	st := waitDone(t, robot)
	assert.Equal(t, 1, st.OK)
	assert.Equal(t, 1, st.Failed)
	assert.Nil(t, st.Processed)
	assert.NotEmpty(t, st.ProcessError+st.ProcessSkip, "причина, почему дислокация не обновлена, должна быть видна")
	assert.Equal(t, 0, repo.calls) // снимок цел
	assert.Equal(t, service.LKRobotFail, st.Items[1].State)
}

// До первого запуска (и после перезапуска сервера) состояние пустое. Файлов
// ручного приёма здесь БОЛЬШЕ НЕ ПОКАЗЫВАЕМ: робот их не создаёт, и чужие файлы
// в окне автозабора читались бы как его результат.
func TestLKRobot_IdleStateIsEmpty(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	repo := &fakeDislRepo{}
	robot, dir := newRobot(t, repo, nil)
	stageWorkbook(t, dir, "10230304", "02.07.2026 06:00") // файл ручного приёма

	st := robot.State()
	assert.False(t, st.Running)
	assert.Equal(t, service.LKRobotStageIdle, st.Stage)
	assert.Empty(t, st.Items)
}

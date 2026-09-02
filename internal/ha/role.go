// Package ha — роль узла в схеме active/standby.
//
// Два сервера держат не ради нагрузки, а чтобы пережить падение одного:
// работает всегда active, standby стоит поднятым и трафика не получает.
// Кто есть кто — решает НЕ приложение: источник правды это etcd
// (/iqport/ha/active_server), а на каждом узле рядом крутится role_observer,
// который эту роль оттуда читает и отдаёт нам. Иначе при разрыве связи оба узла
// сочли бы себя главными и начали писать в одну базу одновременно.
//
// Роль спрашивается ОДИН раз при старте — по договорённости с DevOps
// автоматического переключения (failover) на этой площадке не будет. Появится —
// сюда добавится опрос и подписка на смену роли; всё остальное менять не
// придётся, наружу отсюда торчит только Role.
//
// HTTP-часть модуля роль не касается: балансировщик спрашивает хелсчек
// role_observer, а не наши /health и /ready, и на standby запросы просто не
// приходят. Значение имеют ФОНОВЫЕ задачи — обе ноды ходят в одну базу, и на
// standby всё расписание отработало бы вторым экземпляром: двойной забор
// дислокации из АСУ каждые 10 минут, второй проход памяток с гонкой за курсором
// pamyatka_cursor, двойной разбор очереди 601, вторая суточная фиксация журнала
// брошенных.
//
// Ручной запуск робота ЛК под это правило не подпадает: его дёргает диспетчер
// кнопкой, а на standby запрос не доедет — балансировщик туда не направляет.
package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Role — что этот узел делает прямо сейчас.
type Role string

const (
	RoleActive  Role = "active"
	RoleStandby Role = "standby"
)

// IsActive — можно ли поднимать фоновые задачи.
func (r Role) IsActive() bool { return r == RoleActive }

// ModeStandalone — узел один, спрашивать не у кого.
const ModeStandalone = "standalone"

// ModeCluster — узлов два, роль берётся у role_observer.
const ModeCluster = "cluster"

// observerTimeout — ждать дольше нечего: наблюдатель живёт на том же хосте.
const observerTimeout = 5 * time.Second

// Наблюдатель и наш контейнер поднимаются независимо, и при выкатке легко
// попасть в секунду, когда он ещё не готов. Одна неудачная попытка означала бы
// тихий standby до следующего рестарта — то есть смену без забора дислокации.
// Три попытки закрывают этот случай, а настоящую поломку узла всё равно не
// спрячут: там не ответят и с тридцатой.
// Переменные, а не константы: тестам нужны те же ветки без шести секунд сна.
var (
	observerAttempts = 3
	observerRetryIn  = 2 * time.Second
)

// Resolve — роль этого узла.
//
// Режим не кластерный (в том числе не задан вовсе) — active: одиночный узел
// обязан работать полностью, и именно так модуль жил до появления кластера.
// Этим же путём идут оба наших боевых VPS-инстанса и машина разработчика —
// конфиги там не меняются.
//
// В кластере при любой заминке — standby. Лучше не сделает никто, чем сделают
// двое: два забора АСУ в один снимок и гонка за курсором памяток чинятся руками
// и не всегда полностью.
//
// ⚠️ Обратная сторона этой осторожности: НЕВЕРНЫЙ адрес наблюдателя выглядит
// точно так же, как честный standby, — узел поднимается, отдаёт HTTP и молчит,
// а фоновых задач нет. Самая частая причина — 127.0.0.1 в конфиге контейнера
// (см. Config.App.RoleObserver). Поэтому ошибка Resolve пишется в лог Error'ом,
// а не проглатывается.
func Resolve(ctx context.Context, mode, observer string) (Role, error) {
	if !strings.EqualFold(strings.TrimSpace(mode), ModeCluster) {
		return RoleActive, nil
	}
	url := normalizeURL(observer)
	if url == "" {
		return RoleStandby, fmt.Errorf("app.mode=cluster, но app.role_observer пуст")
	}

	var role Role
	var err error
	for attempt := 1; ; attempt++ {
		role, err = ask(ctx, url)
		if err == nil || attempt == observerAttempts {
			return role, err
		}
		select {
		case <-ctx.Done():
			return RoleStandby, ctx.Err()
		case <-time.After(observerRetryIn):
		}
	}
}

// ask — один поход к наблюдателю.
func ask(ctx context.Context, url string) (Role, error) {
	ctx, cancel := context.WithTimeout(ctx, observerTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return RoleStandby, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return RoleStandby, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return RoleStandby, fmt.Errorf("role_observer ответил %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return RoleStandby, err
	}
	return parseRole(string(body))
}

// normalizeURL — в конфиге адрес записан без схемы (172.17.0.1:9040/role).
// Подставляем http: наблюдатель на том же хосте, TLS внутри моста ни к чему.
func normalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	return "http://" + s
}

// parseRole принимает и голый текст, и JSON.
//
// Форму ответа нам не назвали, а угадывать в одну сторону — значит однажды
// молча уйти в standby на пустом месте. Разобрать оба вида дешевле, чем
// договариваться, и переживёт смену формата у соседей.
//
// Всё, что не опознано, — standby: неизвестный ответ это не разрешение писать.
func parseRole(body string) (Role, error) {
	s := strings.TrimSpace(body)
	if s == "" {
		return RoleStandby, fmt.Errorf("role_observer вернул пустой ответ")
	}

	if strings.HasPrefix(s, "{") {
		var payload struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal([]byte(s), &payload); err != nil {
			return RoleStandby, fmt.Errorf("не разобрал ответ role_observer: %w", err)
		}
		s = payload.Role
	} else if strings.HasPrefix(s, `"`) {
		// Ответ строкой в кавычках — тоже валидный JSON.
		var quoted string
		if err := json.Unmarshal([]byte(s), &quoted); err != nil {
			return RoleStandby, fmt.Errorf("не разобрал ответ role_observer: %w", err)
		}
		s = quoted
	}

	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(RoleActive):
		return RoleActive, nil
	case string(RoleStandby):
		return RoleStandby, nil
	default:
		return RoleStandby, fmt.Errorf("role_observer вернул неизвестную роль %q", strings.TrimSpace(s))
	}
}

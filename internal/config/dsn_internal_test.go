package config

import (
	"strings"
	"testing"
)

// Кластер Patroni: мастер переезжает между узлами, поэтому в конфиге стоят оба
// адреса и драйвер обязан выбрать тот, где сейчас можно писать.
func TestBuildDSN_НесколькоАдресов(t *testing.T) {
	p := Postgres{Host: "10.0.0.1,10.0.0.2", Port: 5000, DBName: "dpport", User: "gtport_app", SSLMode: "disable"}
	dsn := p.BuildDSN()

	if !strings.Contains(dsn, "target_session_attrs=read-write") {
		t.Errorf("без условия драйвер подключится к реплике и запись упадёт: %s", dsn)
	}
	if !strings.Contains(dsn, "host='10.0.0.1,10.0.0.2'") {
		t.Errorf("адреса потерялись: %s", dsn)
	}
	// Порт один на оба адреса — так conninfo и понимает.
	if strings.Count(dsn, "port=") != 1 {
		t.Errorf("порт должен быть один: %s", dsn)
	}
}

// Боевой конфиг пишут руками, и после запятой ставят пробел. Драйвер режет
// список по запятой БЕЗ обрезки пробелов — второй адрес превратился бы в
// « 31.130.132.99» и не зарезолвился, причём ровно в момент переезда мастера.
func TestBuildDSN_ПробелПослеЗапятой(t *testing.T) {
	p := Postgres{Host: "147.45.97.83, 31.130.132.99", Port: 5000, DBName: "ma", User: "ma_admin", SSLMode: "disable"}
	dsn := p.BuildDSN()

	if !strings.Contains(dsn, "host='147.45.97.83,31.130.132.99'") {
		t.Errorf("пробел после запятой не убран, второй узел не зарезолвится: %s", dsn)
	}
	if !strings.Contains(dsn, "target_session_attrs=read-write") {
		t.Errorf("список адресов не распознан: %s", dsn)
	}
}

// Одиночный узел трогать нельзя: к реплике подключаются НАМЕРЕННО (порт чтения,
// отчёты), и условие read-write запретило бы это вовсе.
func TestBuildDSN_ОдинАдресБезУсловия(t *testing.T) {
	p := Postgres{Host: "176.53.160.9", Port: 5432, DBName: "kgdm", User: "gtport_app", SSLMode: "prefer"}

	if dsn := p.BuildDSN(); strings.Contains(dsn, "target_session_attrs") {
		t.Errorf("на одиночном узле условия быть не должно: %s", dsn)
	}
}

// Пароль со спецсимволами не должен разваливать строку подключения: без
// экранирования разбор conninfo уезжает по границам слов.
func TestBuildDSN_ЭкранируетПароль(t *testing.T) {
	p := Postgres{Host: "h", Port: 5432, DBName: "d", User: "u", Password: `pa'ss\word`, SSLMode: "disable"}

	if dsn := p.BuildDSN(); !strings.Contains(dsn, `password='pa\'ss\\word'`) {
		t.Errorf("пароль экранирован неверно: %s", dsn)
	}
}

// Пароль с пробелом — самый частый живой случай: без кавычек всё, что после
// пробела, разбирается как следующий параметр.
func TestBuildDSN_ПарольСПробелом(t *testing.T) {
	p := Postgres{Host: "h", Port: 5432, DBName: "d", User: "u", Password: "two words", SSLMode: "disable"}

	if dsn := p.BuildDSN(); !strings.Contains(dsn, `password='two words'`) {
		t.Errorf("пароль с пробелом не закавычен: %s", dsn)
	}
}

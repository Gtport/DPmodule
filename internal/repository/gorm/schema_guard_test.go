package gormrepo

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Известные таблицы приложения: любые обращения к ним в SQL обязаны нести схему
// dpport (см. schema.go). Список пополняется вместе с миграциями.
var appTables = []string{
	"gt_forecast_snapshot", "nmtp_vagon_column", "notification_read",
	"bros_reason_codes", "vagon_op_request", "cargo_operations",
	"client_settings", "cargo_work_load", "naznach_station", "pamyatka_cursor",
	"port_cargo_line", "vagon_operation", "nitka_schedule", "unplanned_move",
	"vagon_history", "notifications", "report_preset", "event_journal",
	"bros_journal", "plan_profile", "porozh_cargo", "data_source",
	"list_tables", "nmtp_column", "route_speed", "cargo_work", "lk_account",
	"vagon_delay", "plan_nitka", "nmtp_mark", "max_route", "max_chat",
	"stations", "status6", "status9", "cargo", "marka", "ports", "bros",
	"plan", "sf", "dislocation", "dislocation_new",
}

// TestSQLSchemaQualified сторожит инвариант schema.go: прод живёт за пулером
// соединений, search_path сессии не гарантирован, поэтому каждое имя таблицы
// в коде пакета — с явной схемой. Ловит и сырой SQL (FROM/INTO/JOIN/UPDATE/
// TABLE без dpport.), и TableName()/Table() с голым именем.
func TestSQLSchemaQualified(t *testing.T) {
	tblAlt := strings.Join(appTables, "|")
	reSQL := regexp.MustCompile(`(?i)(FROM|INTO|JOIN|UPDATE|TABLE)[\s]+(` + tblAlt + `)\b`)
	reName := regexp.MustCompile(`(TableName\(\) string \{ return|\.Table\()\s*"(` + tblAlt + `)"`)

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // тесты ходят в локальную базу, там search_path задан
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if m := reSQL.FindString(line); m != "" && !strings.Contains(line, "dpport.") {
				t.Errorf("%s:%d: таблица без схемы в SQL: %q — нужно dpport.<имя>", f, i+1, m)
			}
			if m := reName.FindString(line); m != "" {
				t.Errorf("%s:%d: имя таблицы без схемы: %q — нужно dpport.<имя>", f, i+1, m)
			}
		}
	}
}

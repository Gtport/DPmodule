package service

import (
	"context"
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Подпись ветки дислокации в статус-панели сводится к короткому имени
// организации (ports.org_short) независимо от того, как источник записал
// ветку в журнал: ЛК/робот — ОКПО (в т.ч. с ведущим нулём), АСУ — код
// клиента провайдера в organisation.
func TestStatusTermLabel_UnifiedAcrossSources(t *testing.T) {
	dir := NewDirectoryCache(&unplDirStub{ports: []domain.Ports{
		{Okpo: 10230304, NameS: "АЭ", ProviderClient: "attis", OrgShort: "АТТИС", Enabled: true},
		{Okpo: 1126022, NameS: "ГУТ-2", ProviderClient: "nmtp", OrgShort: "НМТП", Enabled: true},
		{Okpo: 1126022, NameS: "УТ-1", ProviderClient: "nmtp", OrgShort: "НМТП", Enabled: true},
	}})
	if err := dir.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := &StatusService{dir: dir}

	cases := []struct {
		name string
		tm   dislTermJournal
		want string
	}{
		{"робот: ОКПО числом", dislTermJournal{Okpo: "10230304", Terminals: []string{"АЭ"}}, "АТТИС"},
		{"ЛК: ОКПО с ведущим нулём", dislTermJournal{Okpo: "01126022", Terminals: []string{"ГУТ-2", "УТ-1"}}, "НМТП"},
		{"АСУ: код клиента в organisation", dislTermJournal{Organisation: "nmtp"}, "НМТП"},
		{"АСУ: другой клиент", dislTermJournal{Organisation: "attis"}, "АТТИС"},
		{"неизвестная ветка — пусто, фронт подпишет по-старому", dislTermJournal{Organisation: "кто-то"}, ""},
	}
	for _, c := range cases {
		if got := s.termLabel(c.tm); got != c.want {
			t.Errorf("%s: termLabel = %q, ждали %q", c.name, got, c.want)
		}
	}
}

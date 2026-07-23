package service

import (
	"context"
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

// fakeMaxChatRepo — репозиторий в памяти для проверки логики разрешения маршрутов.
type fakeMaxChatRepo struct {
	chats  []domain.MaxChat
	routes map[string][]domain.MaxRoute // ключ report+"|"+terminal
}

func (f *fakeMaxChatRepo) Chats(context.Context) ([]domain.MaxChat, error) {
	return f.chats, nil
}

func (f *fakeMaxChatRepo) Routes(_ context.Context, report, terminal string) ([]domain.MaxRoute, error) {
	return f.routes[report+"|"+terminal], nil
}

func newFake() *fakeMaxChatRepo {
	return &fakeMaxChatRepo{
		chats: []domain.MaxChat{
			{Name: "at", ChatID: "-1", Description: "Аттис справки", IsActive: true},
			{Name: "gut", ChatID: "-2", Description: "ГУТ-2 справки", IsActive: true},
			{Name: "oper", ChatID: "-3", Description: "Оперативный", IsActive: true},
			{Name: "off", ChatID: "-4", Description: "Выключенный", IsActive: false},
			{Name: "noid", ChatID: "", Description: "Без id", IsActive: true},
		},
		routes: map[string][]domain.MaxRoute{
			"spravki|АЭ": {
				{Report: "spravki", Terminal: "АЭ", ChatName: "at", SortOrder: 10, Enabled: true},
			},
			"plan|": {
				{Report: "plan", Terminal: "", ChatName: "oper", SortOrder: 10, Enabled: true},
			},
			"oper|АЭ": { // маршрут ссылается на выключенный и на «без id» чат
				{Report: "oper", Terminal: "АЭ", ChatName: "off", SortOrder: 10, Enabled: true},
				{Report: "oper", Terminal: "АЭ", ChatName: "noid", SortOrder: 20, Enabled: true},
			},
			"dup|АЭ": { // один чат в двух маршрутах — дедуп
				{Report: "dup", Terminal: "АЭ", ChatName: "at", SortOrder: 10, Enabled: true},
				{Report: "dup", Terminal: "АЭ", ChatName: "at", SortOrder: 20, Enabled: true},
			},
		},
	}
}

func TestResolveChats_Terminal(t *testing.T) {
	svc := NewMaxChatService(newFake())
	got, err := svc.ResolveChats(context.Background(), "spravki", "АЭ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "at" || got[0].ChatID != "-1" {
		t.Fatalf("ждали [at/-1], получили %+v", got)
	}
}

func TestResolveChats_Composite(t *testing.T) {
	svc := NewMaxChatService(newFake())
	got, err := svc.ResolveChats(context.Background(), "plan", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "oper" {
		t.Fatalf("сводная форма: ждали [oper], получили %+v", got)
	}
}

func TestResolveChats_SkipsInactiveAndNoID(t *testing.T) {
	svc := NewMaxChatService(newFake())
	got, err := svc.ResolveChats(context.Background(), "oper", "АЭ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("выключенный чат и чат без id должны пропускаться, получили %+v", got)
	}
}

func TestResolveChats_Dedup(t *testing.T) {
	svc := NewMaxChatService(newFake())
	got, err := svc.ResolveChats(context.Background(), "dup", "АЭ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("один чат в двух маршрутах — ждали 1 после дедупа, получили %d", len(got))
	}
}

func TestResolveChats_NoRoutes(t *testing.T) {
	svc := NewMaxChatService(newFake())
	got, err := svc.ResolveChats(context.Background(), "spravki", "НЕТ")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("нет маршрутов — ждали nil, получили %+v", got)
	}
}

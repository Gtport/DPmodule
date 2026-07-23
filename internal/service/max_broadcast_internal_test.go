package service

import (
	"context"
	"errors"
	"testing"
)

// fakeSender — транспорт MAX в памяти: пишет отправленное, может валить заданные чаты.
type fakeSender struct {
	sent    map[string]string // chatID → text
	failIDs map[string]bool   // chatID, по которым SendText возвращает ошибку
}

func newFakeSender() *fakeSender {
	return &fakeSender{sent: map[string]string{}, failIDs: map[string]bool{}}
}

func (f *fakeSender) Ping(context.Context) error { return nil }
func (f *fakeSender) SendText(_ context.Context, chatID, text string) error {
	if f.failIDs[chatID] {
		return errors.New("отказ провайдера")
	}
	f.sent[chatID] = text
	return nil
}
func (f *fakeSender) SendImage(context.Context, string, []byte, string, string) error { return nil }
func (f *fakeSender) SendFile(context.Context, string, []byte, string, string) error  { return nil }

func newBroadcast(sender *fakeSender) *MaxBroadcastService {
	// delay=0 — тесты не ждут паузу между чатами.
	return NewMaxBroadcastService(NewMaxChatService(newFake()), sender, 0)
}

func TestBroadcastText_Terminal(t *testing.T) {
	sender := newFakeSender()
	res, err := newBroadcast(sender).SendText(context.Background(), "spravki", "АЭ", "привет")
	if err != nil {
		t.Fatal(err)
	}
	if res.Chats != 1 || len(res.Sent) != 1 || res.Sent[0] != "at" {
		t.Fatalf("ждали отправку в [at], получили %+v", res)
	}
	if sender.sent["-1"] != "привет" {
		t.Errorf("в чат -1 должен уйти текст 'привет', got %q", sender.sent["-1"])
	}
}

func TestBroadcastText_Composite(t *testing.T) {
	sender := newFakeSender()
	res, err := newBroadcast(sender).SendText(context.Background(), "plan", "", "сводка")
	if err != nil {
		t.Fatal(err)
	}
	if res.Chats != 1 || len(res.Sent) != 1 || res.Sent[0] != "oper" {
		t.Fatalf("сводная форма: ждали [oper], получили %+v", res)
	}
}

func TestBroadcastText_NoRoute(t *testing.T) {
	sender := newFakeSender()
	res, err := newBroadcast(sender).SendText(context.Background(), "spravki", "НЕТ", "x")
	if err != nil {
		t.Fatalf("пустой маршрут — не ошибка: %v", err)
	}
	if res.Chats != 0 || len(res.Sent) != 0 || res.AllFailed() {
		t.Fatalf("нет маршрута → Chats=0, ничего не отправлено, AllFailed=false; got %+v", res)
	}
}

func TestBroadcastText_SendFailure(t *testing.T) {
	sender := newFakeSender()
	sender.failIDs["-1"] = true // чат 'at' (spravki/АЭ) валится
	res, err := newBroadcast(sender).SendText(context.Background(), "spravki", "АЭ", "x")
	if err != nil {
		t.Fatal(err)
	}
	if !res.AllFailed() {
		t.Fatalf("единственный чат упал → AllFailed=true; got %+v", res)
	}
	if res.Failed["at"] == "" {
		t.Errorf("ожидали текст ошибки по чату 'at', got %+v", res.Failed)
	}
}

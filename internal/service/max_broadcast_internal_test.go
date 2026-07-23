package service

import (
	"context"
	"errors"
	"testing"
)

// fakeSender — транспорт MAX в памяти: пишет отправленное, может валить заданные чаты.
type fakeSender struct {
	sent    map[string]string // chatID → text
	images  map[string]int    // chatID → размер отправленной картинки, байт
	failIDs map[string]bool   // chatID, по которым отправка возвращает ошибку
}

func newFakeSender() *fakeSender {
	return &fakeSender{sent: map[string]string{}, images: map[string]int{}, failIDs: map[string]bool{}}
}

func (f *fakeSender) Ping(context.Context) error { return nil }
func (f *fakeSender) SendText(_ context.Context, chatID, text string) error {
	if f.failIDs[chatID] {
		return errors.New("отказ провайдера")
	}
	f.sent[chatID] = text
	return nil
}
func (f *fakeSender) SendImage(_ context.Context, chatID string, image []byte, _, _ string) error {
	if f.failIDs[chatID] {
		return errors.New("отказ провайдера")
	}
	f.images[chatID] = len(image)
	return nil
}
func (f *fakeSender) SendFile(context.Context, string, []byte, string, string) error { return nil }

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

func TestBroadcastImage_Composite(t *testing.T) {
	sender := newFakeSender()
	png := []byte("PNGDATA")
	res, err := newBroadcast(sender).SendImage(context.Background(), "plan", "", png, "plan.png", "форма")
	if err != nil {
		t.Fatal(err)
	}
	if res.Chats != 1 || len(res.Sent) != 1 || res.Sent[0] != "oper" {
		t.Fatalf("сводная картинка: ждали [oper], получили %+v", res)
	}
	if sender.images["-3"] != len(png) {
		t.Errorf("в чат -3 (oper) должна уйти картинка %d байт, got %d", len(png), sender.images["-3"])
	}
}

func TestBroadcastImage_SendFailure(t *testing.T) {
	sender := newFakeSender()
	sender.failIDs["-3"] = true // чат 'oper' валится
	res, err := newBroadcast(sender).SendImage(context.Background(), "plan", "", []byte("x"), "p.png", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.AllFailed() || res.Failed["oper"] == "" {
		t.Fatalf("единственный чат упал → AllFailed + ошибка по 'oper'; got %+v", res)
	}
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeRefClient — заглушка port.ReferenceClient: помнит опрошенных клиентов,
// возвращает ошибку для перечисленных в failFor.
type fakeRefClient struct {
	called  []string
	failFor map[string]bool
}

func (f *fakeRefClient) ByNumber(_ context.Context, number string) ([]byte, error) {
	return []byte(`{"PAMYATKI":{}}`), nil
}

func (f *fakeRefClient) Update(_ context.Context, client, _ string) ([]byte, error) {
	f.called = append(f.called, client)
	if f.failFor[client] {
		return nil, errors.New("404")
	}
	return []byte(`{"PAMYATKI":[]}`), nil
}

// Ошибка одного клиента не прерывает опрос остальных, но даёт сводную ошибку.
func TestPullUpdates_ResilientAcrossClients(t *testing.T) {
	fc := &fakeRefClient{failFor: map[string]bool{"nmtp": true}}
	svc := NewReferenceService(fc, []string{"attis", "nmtp"}, time.Hour, zap.NewNop())

	err := svc.PullUpdates(context.Background())
	if err == nil {
		t.Fatal("ждали сводную ошибку из-за упавшего nmtp")
	}
	if len(fc.called) != 2 || fc.called[0] != "attis" || fc.called[1] != "nmtp" {
		t.Fatalf("оба клиента должны быть опрошены, получили %v", fc.called)
	}
}

// Все клиенты успешны → ошибки нет.
func TestPullUpdates_AllOK(t *testing.T) {
	fc := &fakeRefClient{}
	svc := NewReferenceService(fc, []string{"attis"}, time.Hour, zap.NewNop())
	if err := svc.PullUpdates(context.Background()); err != nil {
		t.Fatalf("ошибок быть не должно: %v", err)
	}
}

func TestFetchByNumber(t *testing.T) {
	fc := &fakeRefClient{}
	svc := NewReferenceService(fc, nil, time.Hour, zap.NewNop())
	n, err := svc.FetchByNumber(context.Background(), "10272")
	if err != nil {
		t.Fatalf("FetchByNumber: %v", err)
	}
	if n == 0 {
		t.Fatal("ждали ненулевой размер тела")
	}
}

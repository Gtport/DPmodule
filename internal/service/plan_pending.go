package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// pendingPlan — отложенная загрузка плана между prepare и confirm (в памяти).
// Храним байты файла: на confirm заново разбираем и матчим против ТЕКУЩЕГО снимка,
// чтобы закрыть окно рассогласования (снимок мог пересобраться LK/JSON/другим планом).
type pendingPlan struct {
	planCode string
	filename string
	data     []byte
	created  time.Time
}

// pendingStore — потокобезопасное хранилище отложенных планов по токену с TTL.
// Single-instance silo → достаточно памяти; при рестарте pending теряется (пользователь
// перезагрузит план). См. память plan-sf-human-in-the-loop (решение C).
type pendingStore struct {
	mu    sync.Mutex
	items map[string]pendingPlan
	ttl   time.Duration
}

func newPendingStore(ttl time.Duration) *pendingStore {
	return &pendingStore{items: map[string]pendingPlan{}, ttl: ttl}
}

// put сохраняет отложенный план (чистит просроченные) и возвращает новый токен.
func (s *pendingStore) put(p pendingPlan) string {
	tok := newToken()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	p.created = time.Now()
	s.items[tok] = p
	return tok
}

// take забирает и удаляет отложенный план по токену (ok=false — нет или просрочен).
func (s *pendingStore) take(tok string) (pendingPlan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	p, ok := s.items[tok]
	if ok {
		delete(s.items, tok)
	}
	return p, ok
}

func (s *pendingStore) gcLocked() {
	now := time.Now()
	for k, v := range s.items {
		if now.Sub(v.created) > s.ttl {
			delete(s.items, k)
		}
	}
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

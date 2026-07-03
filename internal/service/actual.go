package service

import (
	"context"
	"sync"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// ActualCache — текущий снимок дислокации («актуальная мапа») в оперативной памяти,
// ключ — номер вагона. Основа Stage 2 (перенос actualMap из gtlogic): обработка
// нового батча сравнивает вагоны с актуальными (FindVagonInActual) для carry-over,
// восстановления пропавших и переходов статусов. Грузится на старте из disl_actual;
// после подмены снимка перезагружается. Доступ под RWMutex. Источник-агностична
// (работает с domain.Dislocation независимо от LK/JSON).
type ActualCache struct {
	repo port.DislocationRepository

	mu      sync.RWMutex
	byVagon map[string]domain.Dislocation
}

func NewActualCache(repo port.DislocationRepository) *ActualCache {
	return &ActualCache{repo: repo, byVagon: map[string]domain.Dislocation{}}
}

// Load читает текущий снимок из хранилища и атомарно заменяет мапу. Вызывать на
// старте и после каждой подмены снимка (ReplaceActual).
func (c *ActualCache) Load(ctx context.Context) error {
	items, err := c.repo.LoadActual(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]domain.Dislocation, len(items))
	for _, it := range items {
		if it.Vagon == "" {
			continue
		}
		m[it.Vagon] = it // при дубле вагона побеждает последний (снимок — по одной записи на вагон)
	}
	c.mu.Lock()
	c.byVagon = m
	c.mu.Unlock()
	return nil
}

// FindVagonInActual возвращает актуальную запись вагона (для carry-over в Stage 2).
func (c *ActualCache) FindVagonInActual(vagon string) (domain.Dislocation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.byVagon[vagon]
	return r, ok
}

// Count — число вагонов в актуальной мапе (для логов/диагностики).
func (c *ActualCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byVagon)
}

// All возвращает копию всех актуальных записей (для восстановления пропавших, S2-1).
func (c *ActualCache) All() []domain.Dislocation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]domain.Dislocation, 0, len(c.byVagon))
	for _, r := range c.byVagon {
		out = append(out, r)
	}
	return out
}

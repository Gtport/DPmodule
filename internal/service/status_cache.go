package service

import (
	"context"
	"sync"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// Status9Cache — write-through RAM-кэш таблицы кандидатов прибытия (status9). Чтение
// (согласование в reconcileCandidates) — из RAM; запись — в БД И в RAM одновременно.
// Прогрев на старте (Load); при внешних правках (UI-подтверждение) — Load заново.
type Status9Cache struct {
	repo port.Status9Repository

	mu      sync.RWMutex
	byVagon map[string]int // vagon → статус (8/9)
}

func NewStatus9Cache(repo port.Status9Repository) *Status9Cache {
	return &Status9Cache{repo: repo, byVagon: map[string]int{}}
}

func (c *Status9Cache) Load(ctx context.Context) error {
	m, err := c.repo.VagonStatuses(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.byVagon = m
	c.mu.Unlock()
	return nil
}

func (c *Status9Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byVagon)
}

// Statuses возвращает копию мапы vagon→статус (для согласования).
func (c *Status9Cache) Statuses() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]int, len(c.byVagon))
	for k, v := range c.byVagon {
		out[k] = v
	}
	return out
}

// InsertNew — новые живые кандидаты (статус 9): в БД (DoNothing по vagon) и в RAM.
func (c *Status9Cache) InsertNew(ctx context.Context, items []domain.Dislocation) (int, error) {
	n, err := c.repo.InsertNew(ctx, items)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	for _, it := range items {
		if _, ok := c.byVagon[it.Vagon]; !ok { // DoNothing-семантика
			c.byVagon[it.Vagon] = derefInt(it.Status)
		}
	}
	c.mu.Unlock()
	return n, nil
}

// UpsertMissing — пропавшие (статус 8): в БД и в RAM (перевод 9→8).
func (c *Status9Cache) UpsertMissing(ctx context.Context, items []domain.Dislocation) (int, error) {
	n, err := c.repo.UpsertMissing(ctx, items)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	for _, it := range items {
		c.byVagon[it.Vagon] = derefInt(it.Status)
	}
	c.mu.Unlock()
	return n, nil
}

// PurgeMissingOlderThan — автоочистка пропавших (статус 8) старше cutoff: выбирает
// устаревших в БД и удаляет через DeleteByVagons (БД и RAM одним путём). Живые
// кандидаты (статус 9) не затрагиваются.
func (c *Status9Cache) PurgeMissingOlderThan(ctx context.Context, cutoff domain.LocalTime) (int, error) {
	vagons, err := c.repo.MissingOlderThan(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if len(vagons) == 0 {
		return 0, nil
	}
	return c.DeleteByVagons(ctx, vagons)
}

// DeleteByVagons — снять кандидатов: из БД и из RAM.
func (c *Status9Cache) DeleteByVagons(ctx context.Context, vagons []string) (int, error) {
	n, err := c.repo.DeleteByVagons(ctx, vagons)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	for _, v := range vagons {
		delete(c.byVagon, v)
	}
	c.mu.Unlock()
	return n, nil
}

// Status6Cache — write-through RAM-кэш доноров перегруза (status6). Хранит полные
// записи (для матч-донорства в S2-3). Прогрев на старте.
type Status6Cache struct {
	repo port.Status6Repository

	mu      sync.RWMutex
	byVagon map[string]domain.Dislocation
}

func NewStatus6Cache(repo port.Status6Repository) *Status6Cache {
	return &Status6Cache{repo: repo, byVagon: map[string]domain.Dislocation{}}
}

func (c *Status6Cache) Load(ctx context.Context) error {
	items, err := c.repo.LoadAll(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]domain.Dislocation, len(items))
	for _, it := range items {
		if it.Vagon != "" {
			m[it.Vagon] = it
		}
	}
	c.mu.Lock()
	c.byVagon = m
	c.mu.Unlock()
	return nil
}

func (c *Status6Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byVagon)
}

// Donors возвращает копию всех доноров (для матч-донорства в S2-3).
func (c *Status6Cache) Donors() []domain.Dislocation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]domain.Dislocation, 0, len(c.byVagon))
	for _, d := range c.byVagon {
		out = append(out, d)
	}
	return out
}

// Upsert — доноры перегруза: в БД и в RAM.
func (c *Status6Cache) Upsert(ctx context.Context, items []domain.Dislocation) (int, error) {
	n, err := c.repo.Upsert(ctx, items)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	for _, it := range items {
		c.byVagon[it.Vagon] = it
	}
	c.mu.Unlock()
	return n, nil
}

// DeleteByVagons — снять доноров (после использования): из БД и из RAM.
func (c *Status6Cache) DeleteByVagons(ctx context.Context, vagons []string) (int, error) {
	n, err := c.repo.DeleteByVagons(ctx, vagons)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	for _, v := range vagons {
		delete(c.byVagon, v)
	}
	c.mu.Unlock()
	return n, nil
}

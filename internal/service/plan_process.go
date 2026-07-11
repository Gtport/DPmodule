package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/service/planmatch"
)

// PlanProcessor принимает файл плана подвода, сопоставляет нитки с вагонами
// дислокации (движок planmatch) и проставляет плановое прибытие в снимок.
//
// Запись идёт по пути пайплайна (не мутация RAM на месте): результат матча
// накладывается на копию снимка ActualCache.All() → ReplaceActual → actual.Load.
// Так снимок остаётся источником правды, а RAM-кэш перечитывается атомарно.
type PlanProcessor struct {
	dir      *DirectoryCache
	repo     port.DislocationRepository
	actual   *ActualCache
	planRepo port.PlanRepository // хранение сетки плана (для фронта); может быть nil
	baseDir  string
}

func NewPlanProcessor(dir *DirectoryCache, repo port.DislocationRepository, actual *ActualCache, planRepo port.PlanRepository, baseDir string) *PlanProcessor {
	return &PlanProcessor{dir: dir, repo: repo, actual: actual, planRepo: planRepo, baseDir: baseDir}
}

// PlanProcessResult — сводка обработки плана.
type PlanProcessResult struct {
	Filename string `json:"filename"`
	PlanCode string `json:"plan_code"`
	Nitki    int    `json:"nitki"`   // всего ниток в плане
	Matched  int    `json:"matched"` // ниток сопоставлено с агрегацией
	Stamped  int    `json:"stamped"` // вагонов проставлено плановое прибытие
	Cleared  int    `json:"cleared"` // вагонов очищено от прежнего плана
}

// ProcessFile сохраняет файл плана, разбирает его и применяет матч к снимку.
// planCode — код станции плана (ma/nk/…), должен иметь профиль и целевые площадки.
func (p *PlanProcessor) ProcessFile(ctx context.Context, planCode, filename string, data []byte) (PlanProcessResult, error) {
	prof, err := plan.ResolveProfile(planCode)
	if err != nil {
		return PlanProcessResult{}, err
	}
	target := p.dir.TargetNaznach(planCode)
	if len(target) == 0 {
		return PlanProcessResult{}, fmt.Errorf("для плана %q нет целевых площадок в ports (plan_code)", planCode)
	}

	path, err := p.save(planCode, data)
	if err != nil {
		return PlanProcessResult{}, err
	}

	doc, err := plan.ParseFile(path, planCode)
	if err != nil {
		return PlanProcessResult{}, fmt.Errorf("разбор плана: %w", err)
	}

	records := p.actual.All()
	agg := planmatch.Aggregate(records, target)
	matches := planmatch.Match(doc.Nitki, agg, prof.MatchRequiresNaznach)

	out, stats := planmatch.Apply(records, matches, target, clock.Now())

	if err := p.repo.ReplaceActual(ctx, out); err != nil {
		return PlanProcessResult{}, fmt.Errorf("замена снимка: %w", err)
	}
	if p.actual != nil {
		if err := p.actual.Load(ctx); err != nil {
			return PlanProcessResult{}, fmt.Errorf("перечитывание актуальной мапы: %w", err)
		}
	}

	// Служебная строка «Остаток на 18:00» не считается ниткой поезда.
	matched, trains := 0, 0
	for i, m := range matches {
		if !doc.Nitki[i].IsOstatok {
			trains++
		}
		if m.Matched {
			matched++
		}
	}

	// Сохраняем сетку плана для фронта (заголовок + нитки с результатом матча).
	// История: SavePlan добавляет новую загрузку, прежние на станцию не трогает.
	if p.planRepo != nil {
		now := clock.Now()
		header := domain.Plan{
			PlanCode:   planCode,
			SourceFile: filename,
			LoadedAt:   &now,
			Nitki:      trains,
			Matched:    matched,
			Stamped:    stats.Stamped,
		}
		nitki := make([]domain.PlanNitka, len(doc.Nitki))
		for i, n := range doc.Nitki {
			nitki[i] = domain.PlanNitka{
				PlanCode:      planCode,
				Ord:           i,
				Index:         n.Index,
				IndexPp:       n.IndexPp,
				StationOper:   planmatch.StationOperOf(matches[i].SubGroups),
				PlanMsk:       localPtr(n.PlanMsk),
				PlanJd:        localPtr(n.PlanJd),
				FactMsk:       localPtr(n.FactMsk),
				Otkl:          n.Otkl,
				Wagons:        n.Wagons,
				Activ:         n.Activ,
				Ports:         toDomainPorts(n.Ports),
				Sostav:        planmatch.FormatSostav(matches[i].SubGroups),
				Comment:       n.Comment,
				Matched:       matches[i].Matched,
				MatchedWagons: len(matches[i].Vagons),
				IsOstatok:     n.IsOstatok,
			}
		}
		if _, err := p.planRepo.SavePlan(ctx, header, nitki); err != nil {
			return PlanProcessResult{}, fmt.Errorf("сохранение сетки плана: %w", err)
		}
	}

	return PlanProcessResult{
		Filename: filename,
		PlanCode: planCode,
		Nitki:    trains,
		Matched:  matched,
		Stamped:  stats.Stamped,
		Cleared:  stats.Cleared,
	}, nil
}

// GetLatestPlan возвращает самую свежую сохранённую сетку плана станции для фронта.
func (p *PlanProcessor) GetLatestPlan(ctx context.Context, planCode string) (domain.Plan, []domain.PlanNitka, error) {
	if p.planRepo == nil {
		return domain.Plan{}, nil, fmt.Errorf("хранение плана не подключено")
	}
	return p.planRepo.GetLatestPlan(ctx, planCode)
}

// ListPlans возвращает список загрузок плана станции (свежие первыми) для выбора на фронте.
func (p *PlanProcessor) ListPlans(ctx context.Context, planCode string) ([]domain.PlanSummary, error) {
	if p.planRepo == nil {
		return nil, fmt.Errorf("хранение плана не подключено")
	}
	return p.planRepo.ListPlans(ctx, planCode)
}

// GetPlanByID возвращает конкретную загрузку плана по id (заголовок + нитки) для фронта.
func (p *PlanProcessor) GetPlanByID(ctx context.Context, id int64) (domain.Plan, []domain.PlanNitka, error) {
	if p.planRepo == nil {
		return domain.Plan{}, nil, fmt.Errorf("хранение плана не подключено")
	}
	return p.planRepo.GetPlanByID(ctx, id)
}

// toDomainPorts переводит ячейки портов парсера в доменные (разные пакеты/типы).
func toDomainPorts(cells []plan.PortCell) []domain.PortCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]domain.PortCell, len(cells))
	for i, c := range cells {
		out[i] = domain.PortCell{Label: c.Label, Count: c.Count, IsOur: c.IsOur}
	}
	return out
}

// localPtr — naive time.Time → *domain.LocalTime (нулевое время → nil).
func localPtr(t time.Time) *domain.LocalTime {
	if t.IsZero() {
		return nil
	}
	return domain.NewLocalTime(t)
}

// save кладёт файл плана в <baseDir>/plan/<planCode>.xlsx (один файл на план,
// перезапись) — аудит-копия и источник для парсера.
func (p *PlanProcessor) save(planCode string, data []byte) (string, error) {
	dir := filepath.Join(p.baseDir, "plan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("создание каталога плана: %w", err)
	}
	path := filepath.Join(dir, planCode+".xlsx")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("сохранение файла плана: %w", err)
	}
	return path, nil
}

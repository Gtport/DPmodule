package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
	"github.com/Gtport/DPmodule/internal/port"
)

// LKProcessor — шаг 2 двухшаговой загрузки ЛК: обработка staged-файлов в снимок
// дислокации. Читает контроль приёма (LKIntake.Status), парсит принятые файлы и
// атомарно заменяет снимок (ReplaceActual, «вариант B»). Обогащение Stage 1–4 —
// отдельными слоями (пока снимок «сырой»: коды без имён станций/портов).
type LKProcessor struct {
	intake   *LKIntake
	repo     port.DislocationRepository
	actual   *ActualCache
	status9  port.Status9Repository
	enricher *Enricher
}

func NewLKProcessor(intake *LKIntake, repo port.DislocationRepository, actual *ActualCache, status9 port.Status9Repository) *LKProcessor {
	return &LKProcessor{intake: intake, repo: repo, actual: actual, status9: status9, enricher: NewEnricher(intake.dir)}
}

var (
	ErrNotReady = errors.New("приём не готов к обработке")
	ErrDataLoss = errors.New("потеря данных превышает допустимый порог")
)

// LKProcessResult — итог обработки.
type LKProcessResult struct {
	Count            int            `json:"count"`              // записей в новом снимке
	Files            int            `json:"files"`              // обработано файлов
	PrevSnapshot     int            `json:"prev_snapshot"`      // размер прежнего снимка
	PerFile          map[string]int `json:"per_file"`           // имя файла → число записей
	NaznEnriched     int            `json:"nazn_enriched"`      // записей с заполненной станцией назначения (Stage 1)
	StationsNotFound []int          `json:"stations_not_found"` // коды станций вне справочника
	OpsNotFound      []int          `json:"ops_not_found"`      // коды операций вне справочника
	PortUnresolved   int            `json:"port_unresolved"`    // отброшено: (ОКПО+станция) не резолвится (Stage 2)
	PortDisabled     int            `json:"port_disabled"`      // отброшено: порт выключен (Stage 2)
	Status9Inserted  int            `json:"status9_inserted"`   // новых кандидатов статуса 9 (S2-1a)
	Status9Removed   int            `json:"status9_removed"`    // снято кандидатов (вернулись в поток)
	StatusDist       map[int]int    `json:"status_dist"`        // распределение статусов (Stage 1b)
}

// Process проверяет готовность приёма, парсит все принятые файлы ЛК и заменяет
// снимок дислокации. Гарды: блокирующий контроль приёма (Status.ready) и порог
// потери данных (max_data_loss_pct) относительно текущего снимка.
func (p *LKProcessor) Process(ctx context.Context) (LKProcessResult, error) {
	st, err := p.intake.Status()
	if err != nil {
		return LKProcessResult{}, err
	}
	if !st.Ready {
		return LKProcessResult{}, fmt.Errorf("%w: %d блокирующих замечаний", ErrNotReady, countBlocking(st.Issues))
	}

	// Профиль парсера — из настроек источника 'lk' (формат файла).
	var profile parser.SourceProfile
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		profile = parser.SourceProfile{
			DateCutoffHour: ds.Config.DateCutoffHour,
			HeaderMarker:   ds.Config.HeaderMarker,
		}
	}
	lp := parser.NewLKParser(profile)

	dir := filepath.Join(p.intake.baseDir, "lk")
	perFile := make(map[string]int, len(st.Files))
	all := make([]domain.Dislocation, 0, 4096)
	for _, f := range st.Files {
		recs, err := lp.ParseFile(filepath.Join(dir, f.Filename))
		if err != nil {
			return LKProcessResult{}, fmt.Errorf("парсинг %s: %w", f.Filename, err)
		}
		perFile[f.Filename] = len(recs)
		all = append(all, recs...)
	}

	// Stage 1: станции → идентификация порта + фильтр → операции → статусы.
	// Возвращает отфильтрованный обогащённый набор (только включённые порты).
	var cutoff int
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		cutoff = ds.Config.DateCutoffHour
	}
	sp := p.intake.cfg.Settings().Status
	var enr Stage1Stats
	all, enr = p.enricher.Stage1(all, Stage1Config{
		CutoffHour: cutoff, ProstDnMin: sp.ProstDnMin, ProstChMin: sp.ProstChMin,
	})

	// Контроль потери данных относительно текущего снимка (до подмены).
	current, err := p.repo.LoadActual(ctx)
	if err != nil {
		return LKProcessResult{}, fmt.Errorf("чтение текущего снимка: %w", err)
	}
	pol := p.intake.cfg.Settings().IngestPolicy.Dislocation
	if lost := dataLossPct(len(current), len(all)); pol.MaxDataLossPct > 0 && lost >= pol.MaxDataLossPct {
		return LKProcessResult{}, fmt.Errorf("%w: −%d%% (%d → %d) ≥ %d%%",
			ErrDataLoss, lost, len(current), len(all), pol.MaxDataLossPct)
	}

	// Stage 2 (S2-1a): наполнение таблицы кандидатов статусом 9 из живого батча —
	// ДО подмены снимка (actual = прежний снимок для сравнения «первого появления»).
	var s9 Status9Stats
	if p.actual != nil && p.status9 != nil {
		if s9, err = applyStatus9Live(ctx, all, p.actual, p.status9); err != nil {
			return LKProcessResult{}, fmt.Errorf("status9: %w", err)
		}
	}

	if err := p.repo.ReplaceActual(ctx, all); err != nil {
		return LKProcessResult{}, fmt.Errorf("замена снимка: %w", err)
	}
	// Актуальная мапа сменилась — перечитываем (для следующего цикла).
	if p.actual != nil {
		if err := p.actual.Load(ctx); err != nil {
			return LKProcessResult{}, fmt.Errorf("перечитывание актуальной мапы: %w", err)
		}
	}
	return LKProcessResult{
		Count: len(all), Files: len(st.Files), PrevSnapshot: len(current), PerFile: perFile,
		NaznEnriched: enr.NaznEnriched, StationsNotFound: enr.StationsNotFound, OpsNotFound: enr.OperationsNotFound,
		PortUnresolved: enr.PortUnresolved, PortDisabled: enr.PortDisabled, StatusDist: enr.StatusDist,
		Status9Inserted: s9.Inserted, Status9Removed: s9.Removed,
	}, nil
}

func countBlocking(issues []LKIssue) int {
	n := 0
	for _, i := range issues {
		if i.Level == LKIssueBlock {
			n++
		}
	}
	return n
}

// dataLossPct — процент сокращения набора относительно текущего снимка (0, если
// новый не меньше или снимок пуст). Целочисленно вниз.
func dataLossPct(current, next int) int {
	if current <= 0 || next >= current {
		return 0
	}
	return (current - next) * 100 / current
}

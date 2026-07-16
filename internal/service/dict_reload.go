package service

import (
	"context"
	"fmt"
)

// DictReloadResult — отчёт механизма «Обновить справочники» для фронта и журнала.
type DictReloadResult struct {
	Count            int `json:"count"`             // вагонов в снимке
	Refreshed        int `json:"refreshed"`         // атрибутированные: строка marka переприменена (правка словаря доехала)
	Filled           int `json:"filled"`            // были без атрибуции — заполнены строгим матчем marka
	FilledByTrain    int `json:"filled_by_train"`   // заполнены наследованием по составу
	StillEmpty       int `json:"still_empty"`       // остались без атрибуции (нет ни marka, ни состава)
	ForecastComputed int `json:"forecast_computed"` // вагонов с пересчитанным ходом (Stage 3)
	ProgComputed     int `json:"prog_computed"`     // вагонов с пересчитанным прогнозом порта (Stage 4)
}

// ReloadDirectories — механизм «Обновить справочники» (перенос эталона gtport,
// гибридная схема владельца): после правки словарей админом
//  1. горячая перезагрузка DirectoryCache из БД;
//  2. пересчёт снимка БЕЗ приёма новых данных:
//     — атрибутированным вагонам строка marka переприменяется СТРОГО по ключу
//       (ОКПО+станция из потока, группа груза — ПЕРЕНЕСЁННАЯ, не по коду):
//       правки словаря доезжают до вагонов, а испорченные в пути ключи ничем
//       не матчатся и достоверную запись не трогают;
//     — вагонам без атрибуции — полный путь S2-3 (словарь cargo + строгий матч
//       marka + наследование по составу + sms_2); refresh идёт ПЕРВЫМ, чтобы
//       доноры раздавали наследованием уже обновлённую атрибуцию;
//  3. пересчёт Stage 3–4 (ход и прогноз порта — тоже зависят от справочников);
//  4. атомарная подмена снимка + перечитка RAM.
//
// Событие журнала — dict_reload (trigger=actualization). Вагоны с наследованной
// ранее атрибуцией и кривыми ключами пересчёт не охватывает (осознанно).
func (p *LKProcessor) ReloadDirectories(ctx context.Context) (DictReloadResult, error) {
	// Пересчёт снимка не должен пересекаться с пересборкой (ЛК/АСУ) — общий мьютекс.
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.actual == nil {
		return DictReloadResult{}, fmt.Errorf("%w: снимок дислокации не загружен", ErrNotReady)
	}
	if err := p.intake.dir.Load(ctx); err != nil {
		return DictReloadResult{}, fmt.Errorf("перезагрузка справочников: %w", err)
	}

	all := p.actual.All()
	refreshed := applyMarkaRefresh(all, p.intake.dir)
	mk := applyMarkaEnrichment(all, p.intake.dir)

	var cutoff int
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		cutoff = ds.Config.DateCutoffHour
	}
	forecastN := applyForecast(all, p.intake.dir, cutoff)
	progN := applyStage4(all, p.intake.dir, p.intake.cfg, cutoff)

	if err := p.repo.ReplaceActual(ctx, all); err != nil {
		return DictReloadResult{}, fmt.Errorf("замена снимка: %w", err)
	}
	if err := p.actual.Load(ctx); err != nil {
		return DictReloadResult{}, fmt.Errorf("перечитывание актуальной мапы: %w", err)
	}

	res := DictReloadResult{
		Count: len(all), Refreshed: refreshed,
		Filled: mk.FilledFull, FilledByTrain: mk.FilledByTrain, StillEmpty: mk.MissedMarka,
		ForecastComputed: forecastN, ProgComputed: progN,
	}
	if p.journal != nil {
		p.journal.RecordDictReload(ctx, res)
	}
	return res, nil
}

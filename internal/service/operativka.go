package service

import (
	"context"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// OperativkaService — карточка «Оперативка» домашней страницы: суточные счётчики
// по терминалам (реестр ports, не хардкод) — прибыло/выгружено за вчерашние и
// текущие ЖД-сутки (вехи vagon_history: date_prib_d × naznach, date_vigr_d ×
// place_vigr) плюс «не выгружено» — вагоны статуса 10 в текущем снимке.
type OperativkaService struct {
	repo   port.HistoryRepository
	actual *ActualCache
	dir    *DirectoryCache
}

func NewOperativkaService(repo port.HistoryRepository, actual *ActualCache, dir *DirectoryCache) *OperativkaService {
	return &OperativkaService{repo: repo, actual: actual, dir: dir}
}

// OperativkaRowDTO — строка карточки: терминал и его суточные счётчики.
type OperativkaRowDTO struct {
	Terminal      string `json:"terminal"`
	Station       string `json:"station"`
	StationCode   string `json:"station_code"`
	PribYesterday int    `json:"prib_yesterday"`
	VigrYesterday int    `json:"vigr_yesterday"`
	PribToday     int    `json:"prib_today"`
	VigrToday     int    `json:"vigr_today"`
	NotUnloaded   int    `json:"not_unloaded"` // сейчас в статусе 10 (прибыл, не выгружен)
}

// OperativkaDTO — ответ ручки: даты суток и строки по терминалам.
type OperativkaDTO struct {
	Yesterday string             `json:"yesterday"` // yyyy-MM-dd (ЖД-сутки)
	Today     string             `json:"today"`
	Rows      []OperativkaRowDTO `json:"rows"`
}

// Snapshot — счётчики за вчера/сегодня (ЖД-сутки от МСК-«сейчас») по всем
// включённым терминалам реестра; порядок — станции по коду по убыванию (как
// колонки домашней страницы: Мыс, Находка), терминалы по имени.
func (s *OperativkaService) Snapshot(ctx context.Context) (OperativkaDTO, error) {
	today := clock.Now().Time().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)
	todayS, yestS := today.Format("2006-01-02"), yesterday.Format("2006-01-02")

	prib, vigr, err := s.repo.DailyTerminalCounts(ctx,
		domain.LocalTime(yesterday), domain.LocalTime(today))
	if err != nil {
		return OperativkaDTO{}, err
	}

	// «Не выгружено» — статус 10 по терминалам из RAM-снимка.
	notUnloaded := map[string]int{}
	for _, r := range s.actual.All() {
		if r.Status != nil && *r.Status == 10 && r.Naznach != "" {
			notUnloaded[r.Naznach]++
		}
	}

	targets := terminalTargets(s.dir)
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].StationCode != targets[j].StationCode {
			return targets[i].StationCode > targets[j].StationCode // Мыс (9857) раньше Находки (9847)
		}
		return targets[i].Name < targets[j].Name
	})

	rows := make([]OperativkaRowDTO, 0, len(targets))
	for _, t := range targets {
		rows = append(rows, OperativkaRowDTO{
			Terminal: t.Name, Station: t.Station, StationCode: t.StationCode,
			PribYesterday: prib[yestS+"|"+t.Name],
			VigrYesterday: vigr[yestS+"|"+t.Name],
			PribToday:     prib[todayS+"|"+t.Name],
			VigrToday:     vigr[todayS+"|"+t.Name],
			NotUnloaded:   notUnloaded[t.Name],
		})
	}
	return OperativkaDTO{Yesterday: yestS, Today: todayS, Rows: rows}, nil
}

package gormrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────────
//  ORM-модели настроечной таблицы. JSONB-колонки читаем в string и разбираем
//  в доменные структуры в адаптере (без внешних datatypes-зависимостей).
// ──────────────────────────────────────────────────────────────────────────

type dataSourceModel struct {
	ID             string `gorm:"column:id;primaryKey"`
	Name           string `gorm:"column:name"`
	Enabled        bool   `gorm:"column:enabled"`
	Ingest         string `gorm:"column:ingest"`
	Category       string `gorm:"column:category"`
	Config         string `gorm:"column:config"` // jsonb → text
	CoArrivalGroup string `gorm:"column:co_arrival_group"`
	SortOrder      int    `gorm:"column:sort_order"`
}

func (dataSourceModel) TableName() string { return "data_source" }

type clientSettingsModel struct {
	ID           int    `gorm:"column:id;primaryKey"`
	ClientName   string `gorm:"column:client_name"`
	IngestPolicy string `gorm:"column:ingest_policy"` // jsonb → text
	Extra        string `gorm:"column:extra"`         // jsonb → text (status-пороги и пр.)
}

func (clientSettingsModel) TableName() string { return "client_settings" }

type planProfileModel struct {
	StationCode          string  `gorm:"column:station_code;primaryKey"`
	StationName          string  `gorm:"column:station_name"`
	Mode                 string  `gorm:"column:mode"`
	PlanCode             *string `gorm:"column:plan_code"` // nullable — пусто у бесплановых
	CorrectionCoef       float64 `gorm:"column:correction_coef"`
	MatchRequiresNaznach bool    `gorm:"column:match_requires_naznach"`
	OurTerminals         string  `gorm:"column:our_terminals"` // jsonb → text
	SlotToleranceH       float64 `gorm:"column:slot_tolerance_h"`
}

func (planProfileModel) TableName() string { return "plan_profile" }

// ──────────────────────────────────────────────────────────────────────────
//  Адаптер: реализует port.ConfigRepository.
// ──────────────────────────────────────────────────────────────────────────

type ConfigRepository struct {
	db *gorm.DB
}

func NewConfigRepository(db *gorm.DB) *ConfigRepository {
	return &ConfigRepository{db: db}
}

func (r *ConfigRepository) LoadDataSources(ctx context.Context) ([]domain.DataSource, error) {
	var ms []dataSourceModel
	if err := r.db.WithContext(ctx).Order("sort_order, id").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.DataSource, len(ms))
	for i, m := range ms {
		var cfg domain.DataSourceConfig
		if m.Config != "" {
			if err := json.Unmarshal([]byte(m.Config), &cfg); err != nil {
				return nil, fmt.Errorf("data_source %q config: %w", m.ID, err)
			}
		}
		out[i] = domain.DataSource{
			ID: m.ID, Name: m.Name, Enabled: m.Enabled,
			Ingest: m.Ingest, Category: m.Category,
			Config: cfg, CoArrivalGroup: m.CoArrivalGroup, SortOrder: m.SortOrder,
		}
	}
	return out, nil
}

func (r *ConfigRepository) LoadClientSettings(ctx context.Context) (domain.ClientSettings, error) {
	var m clientSettingsModel
	err := r.db.WithContext(ctx).First(&m, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ClientSettings{}, nil // синглтона ещё нет — пустые настройки
	}
	if err != nil {
		return domain.ClientSettings{}, err
	}
	var pol domain.IngestPolicy
	if m.IngestPolicy != "" {
		if err := json.Unmarshal([]byte(m.IngestPolicy), &pol); err != nil {
			return domain.ClientSettings{}, fmt.Errorf("client_settings ingest_policy: %w", err)
		}
	}
	var extra struct {
		Status domain.StatusPolicy `json:"status"`
		Stage4 domain.Stage4Policy `json:"stage4"`
	}
	if m.Extra != "" {
		if err := json.Unmarshal([]byte(m.Extra), &extra); err != nil {
			return domain.ClientSettings{}, fmt.Errorf("client_settings extra: %w", err)
		}
	}
	return domain.ClientSettings{
		ClientName: m.ClientName, IngestPolicy: pol, Status: extra.Status, Stage4: extra.Stage4,
	}, nil
}

func (r *ConfigRepository) LoadPlanProfiles(ctx context.Context) ([]domain.PlanProfile, error) {
	var ms []planProfileModel
	if err := r.db.WithContext(ctx).Order("station_code").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.PlanProfile, len(ms))
	for i, m := range ms {
		var terms []string
		if m.OurTerminals != "" {
			if err := json.Unmarshal([]byte(m.OurTerminals), &terms); err != nil {
				return nil, fmt.Errorf("plan_profile %q our_terminals: %w", m.StationCode, err)
			}
		}
		planCode := ""
		if m.PlanCode != nil {
			planCode = *m.PlanCode
		}
		out[i] = domain.PlanProfile{
			StationCode: m.StationCode, StationName: m.StationName, Mode: m.Mode,
			PlanCode: planCode, CorrectionCoef: m.CorrectionCoef,
			MatchRequiresNaznach: m.MatchRequiresNaznach, OurTerminals: terms,
			SlotToleranceH: m.SlotToleranceH,
		}
	}
	return out, nil
}

func (r *ConfigRepository) LoadNitkaSchedule(ctx context.Context) ([]domain.NitkaSlot, error) {
	// slot_time — тип time; берём HH:MM строкой (to_char), чтобы не зависеть от типа драйвера.
	var rows []struct {
		StationCode string `gorm:"column:station_code"`
		HM          string `gorm:"column:hm"`
		SortOrder   int    `gorm:"column:sort_order"`
	}
	err := r.db.WithContext(ctx).Raw(
		`SELECT station_code, to_char(slot_time,'HH24:MI') AS hm, sort_order
		   FROM nitka_schedule ORDER BY station_code, sort_order`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.NitkaSlot, 0, len(rows))
	for _, row := range rows {
		var h, m int
		if _, err := fmt.Sscanf(row.HM, "%d:%d", &h, &m); err != nil {
			continue // битый слот пропускаем
		}
		out = append(out, domain.NitkaSlot{StationCode: row.StationCode, Hour: h, Minute: m, SortOrder: row.SortOrder})
	}
	return out, nil
}

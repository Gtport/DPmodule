package gormrepo

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// компилятор-проверка: репозиторий реализует порт.
var _ port.DislocationRepository = (*DislocationRepository)(nil)

// dislocationModel — ORM-представление dpport.dislocation. Поля, их порядок и типы
// ПОЛНОСТЬЮ совпадают с domain.Dislocation (отличаются только теги) — за счёт этого
// маппинг делается прямой конверсией типов Go (теги при конверсии игнорируются),
// а любое расхождение полей ловит компилятор. Имена колонок заданы явно.
type dislocationModel struct {
	ID          string `gorm:"column:id;primaryKey"`
	Vagon       string `gorm:"column:vagon"`
	Invoice     string `gorm:"column:invoice"`
	InvoiceMain string `gorm:"column:invoice_main"`

	Index     string `gorm:"column:index"`
	IndexMain string `gorm:"column:index_main"`
	IndexLast string `gorm:"column:index_last"`
	IndexPp   string `gorm:"column:index_pp"`

	DateNach        *domain.LocalTime `gorm:"column:date_nach"`
	DateOtpr        *domain.LocalTime `gorm:"column:date_otpr"`
	CodeStationNach string            `gorm:"column:code_station_nach"`
	StationNach     string            `gorm:"column:station_nach"`
	DorogaNach      string            `gorm:"column:doroga_nach"`
	StrNach         string            `gorm:"column:str_nach"`
	Zayavka         string            `gorm:"column:zayavka"`

	GruzotprOkpo string `gorm:"column:gruzotpr_okpo"`
	Gruzotpr     string `gorm:"column:gruzotpr"`

	CodeStanNazn  string `gorm:"column:code_stan_nazn"`
	Code4StanNazn string `gorm:"column:code4_stan_nazn"`
	StanNazn      string `gorm:"column:stan_nazn"`
	DorogaNazn    string `gorm:"column:doroga_nazn"`
	StrNazn       string `gorm:"column:str_nazn"`
	GruzpolOkpo   string `gorm:"column:gruzpol_okpo"`
	Gruzpol       string `gorm:"column:gruzpol"`
	GruzpolS      string `gorm:"column:gruzpol_s"`
	Naznach       string `gorm:"column:naznach"`
	Owner         string `gorm:"column:owner"`

	CodeCargo        string   `gorm:"column:code_cargo"`
	CodeCargoGng     string   `gorm:"column:code_cargo_gng"`
	CodeCargoVygr    string   `gorm:"column:code_cargo_vygr"`
	CargoS           string   `gorm:"column:cargo_s"`
	CargoSms         string   `gorm:"column:cargo_sms"`
	CargoGroup       string   `gorm:"column:cargo_group"`
	Ves              *float64 `gorm:"column:ves"`
	PorozhPriznak    string   `gorm:"column:porozh_priznak"`
	FreightExactName string   `gorm:"column:freight_exact_name"`
	GtdNumber        string   `gorm:"column:gtd_number"`

	TimeOp          *domain.LocalTime `gorm:"column:time_op"`
	DateOp          *domain.LocalTime `gorm:"column:date_op"`
	DateOpJd        *domain.LocalTime `gorm:"column:date_op_jd"`
	CodeOper        string            `gorm:"column:code_oper"`
	Oper            string            `gorm:"column:oper"`
	OperS           string            `gorm:"column:oper_s"`
	CodeStationOper string            `gorm:"column:code_station_oper"`
	StationOper     string            `gorm:"column:station_oper"`
	DorogaOper      string            `gorm:"column:doroga_oper"`

	IdOtprk string `gorm:"column:id_otprk"`
	Uno     string `gorm:"column:uno"`

	Latitude  string   `gorm:"column:latitude"`
	Longitude string   `gorm:"column:longitude"`
	Temper    *float64 `gorm:"column:temper"`

	RasstStanNazn *int `gorm:"column:rasst_stan_nazn"`
	RasstOb       *int `gorm:"column:rasst_ob"`
	RasstStanOp   *int `gorm:"column:rasst_stan_op"`

	ProstDn  *int `gorm:"column:prost_dn"`
	ProstCh  *int `gorm:"column:prost_ch"`
	ProstMin *int `gorm:"column:prost_min"`

	IdDisl    string `gorm:"column:id_disl"`
	NppVag    *int   `gorm:"column:npp_vag"`
	Status    *int   `gorm:"column:status"`
	IdStatus5 string `gorm:"column:id_status5"`
	IdStatus4 string `gorm:"column:id_status4"`

	DateDostav *domain.LocalTime `gorm:"column:date_dostav"`
	Delay      *int              `gorm:"column:delay"`
	DelayProg  *int              `gorm:"column:delay_prog"`
	PlanJd     *domain.LocalTime `gorm:"column:plan_jd"`
	PlanMsk    *domain.LocalTime `gorm:"column:plan_msk"`
	ToGo       *float64          `gorm:"column:to_go"`
	RaschMsk   *domain.LocalTime `gorm:"column:rasch_msk"`
	ProgMsk    *domain.LocalTime `gorm:"column:prog_msk"`
	Mistake    *float64          `gorm:"column:mistake"`
	RaschJd    *domain.LocalTime `gorm:"column:rasch_jd"`
	ProgJd     *domain.LocalTime `gorm:"column:prog_jd"`
	DateKon    *domain.LocalTime `gorm:"column:date_kon"`
	DatePrib   *domain.LocalTime `gorm:"column:date_prib"`

	AlternativeMove int `gorm:"column:alternative_move"`

	CarOwnerName   string `gorm:"column:car_owner_name"`
	CarOwnerOkpo   string `gorm:"column:car_owner_okpo"`
	CarTenantName  string `gorm:"column:car_tenant_name"`
	CarTenantOkpo  string `gorm:"column:car_tenant_okpo"`
	CarTrustedName string `gorm:"column:car_trusted_name"`
	CarTrustedOkpo string `gorm:"column:car_trusted_okpo"`

	PereadrType string `gorm:"column:pereadr_type"`
	PereadrPort string `gorm:"column:pereadr_port"`

	Client  string `gorm:"column:client"`
	Sms1    string `gorm:"column:sms_1"`
	Sms2    string `gorm:"column:sms_2"`
	Sms3    string `gorm:"column:sms_3"`
	Sprav1  string `gorm:"column:sprav_1"`
	Sprav2  string `gorm:"column:sprav_2"`
	Sprav3  string `gorm:"column:sprav_3"`
	Param1  string `gorm:"column:param_1"`
	Param2  string `gorm:"column:param_2"`
	Param3  string `gorm:"column:param_3"`
	NParam1 string `gorm:"column:n_param_1"`
	NParam2 string `gorm:"column:n_param_2"`
	NParam3 string `gorm:"column:n_param_3"`

	DateVigr  *domain.LocalTime `gorm:"column:date_vigr"`
	PlaceVigr string            `gorm:"column:place_vigr"`
	Frost     *int              `gorm:"column:frost"`
	Info1     string            `gorm:"column:info_1"`
	Info2     string            `gorm:"column:info_2"`
	Peregruz  string            `gorm:"column:peregruz"`
	Color     string            `gorm:"column:color"`
	RodVagUch string            `gorm:"column:rod_vag_uch"`
	Shipments string            `gorm:"column:shipments"`
	History   int               `gorm:"column:history"`

	CreatedAt domain.LocalTime `gorm:"column:created_at;default:now()"`
	UpdatedAt domain.LocalTime `gorm:"column:updated_at;default:now()"`
}

func (dislocationModel) TableName() string { return actualTable }

const (
	actualTable  = "dislocation"
	stagingTable = "dislocation_new"
	batchSize    = 500
)

// DislocationRepository — персистентность снимка дислокации. Атомарная замена
// снимка идёт по «варианту B»: тяжёлая заливка в staging-таблицу dislocation_new,
// затем быстрый swap через rename. История снимков не ведётся.
type DislocationRepository struct {
	db *gorm.DB
}

func NewDislocationRepository(db *gorm.DB) *DislocationRepository {
	return &DislocationRepository{db: db}
}

// LoadActual читает весь текущий снимок (для прогрева RAM-движка на старте).
func (r *DislocationRepository) LoadActual(ctx context.Context) ([]domain.Dislocation, error) {
	var ms []dislocationModel
	if err := r.db.WithContext(ctx).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.Dislocation, len(ms))
	for i, m := range ms {
		out[i] = domain.Dislocation(m) // прямая конверсия (поля идентичны)
	}
	return out, nil
}

// ReplaceActual атомарно заменяет снимок dislocation новым набором записей
// (ensure staging → залить в dislocation_new → swap).
func (r *DislocationRepository) ReplaceActual(ctx context.Context, items []domain.Dislocation) error {
	if err := r.ensureStaging(ctx); err != nil {
		return fmt.Errorf("ensure staging: %w", err)
	}
	if err := r.loadStaging(ctx, items); err != nil {
		return fmt.Errorf("load staging: %w", err)
	}
	if err := r.swap(ctx); err != nil {
		return fmt.Errorf("swap: %w", err)
	}
	return nil
}

// ensureStaging гарантирует наличие dislocation_new (копия структуры dislocation
// со всеми индексами/дефолтами). Базовую таблицу ведёт schema-миграция, не AutoMigrate.
func (r *DislocationRepository) ensureStaging(ctx context.Context) error {
	db := r.db.WithContext(ctx)
	if !db.Migrator().HasTable(actualTable) {
		return fmt.Errorf("base table %q is missing; run schema migrations first", actualTable)
	}
	if !db.Migrator().HasTable(stagingTable) {
		return db.Exec(`CREATE TABLE ` + stagingTable + ` (LIKE ` + actualTable + ` INCLUDING ALL)`).Error
	}
	return nil
}

// loadStaging заливает снимок в dislocation_new (отдельная транзакция, чтобы окно
// swap было крошечным). Таблица предварительно очищается.
func (r *DislocationRepository) loadStaging(ctx context.Context, items []domain.Dislocation) error {
	models := make([]dislocationModel, len(items))
	for i, d := range items {
		models[i] = dislocationModel(d) // прямая конверсия (поля идентичны)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`TRUNCATE TABLE ` + stagingTable).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		return tx.Table(stagingTable).CreateInBatches(models, batchSize).Error
	})
}

// swap — атомарная замена dislocation снимком из dislocation_new (вариант B).
// DDL в PostgreSQL транзакционен → читатель видит либо старый, либо новый снимок,
// без «пустого окна». После свапа заново создаём пустую staging-таблицу.
func (r *DislocationRepository) swap(ctx context.Context) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DROP TABLE IF EXISTS ` + actualTable).Error; err != nil {
			return err
		}
		if err := tx.Exec(`ALTER TABLE ` + stagingTable + ` RENAME TO ` + actualTable).Error; err != nil {
			return err
		}
		return tx.Exec(`CREATE TABLE ` + stagingTable + ` (LIKE ` + actualTable + ` INCLUDING ALL)`).Error
	})
}

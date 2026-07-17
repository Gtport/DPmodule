package gormrepo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// AdminTablesRepository — универсальный CRUD справочников для админ-редактора
// (реализация port.AdminTablesRepository). Динамический SQL — осознанно (канон
// GORM-гибрида): имена таблиц берутся ТОЛЬКО из реестра dpport.list_tables,
// имена колонок — только из information_schema; всё квотируется, значения
// передаются параметрами. Произвольный ввод в идентификаторы не попадает.
type AdminTablesRepository struct {
	db *gorm.DB
}

func NewAdminTablesRepository(db *gorm.DB) *AdminTablesRepository {
	return &AdminTablesRepository{db: db}
}

// quoteIdent квотирует идентификатор Postgres ("имя", кавычки внутри удваиваются).
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ListTables возвращает редактируемые таблицы реестра с определённым ключом
// строки: колонка id, если есть, иначе одноколоночный PRIMARY KEY. Таблица без
// такого ключа (составной PK без id) в редактор не попадает.
func (r *AdminTablesRepository) ListTables(ctx context.Context) ([]domain.AdminTable, error) {
	var rows []struct {
		Name   string `gorm:"column:name"`
		NameRu string `gorm:"column:name_ru"`
		PK     string `gorm:"column:pk"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT lt.name, lt.name_ru,
		       COALESCE(
		           (SELECT 'id' FROM information_schema.columns c
		             WHERE c.table_schema = current_schema() AND c.table_name = lt.name AND c.column_name = 'id'),
		           (SELECT a.attname
		              FROM pg_index i
		              JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		             WHERE i.indrelid = (quote_ident(current_schema()) || '.' || quote_ident(lt.name))::regclass
		               AND i.indisprimary AND array_length(i.indkey, 1) = 1),
		           ''
		       ) AS pk
		  FROM list_tables lt
		 WHERE lt.editable = true
		 ORDER BY lt.name_ru`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.AdminTable, 0, len(rows))
	for _, t := range rows {
		if t.PK == "" {
			continue // нет одноколоночного ключа — редактировать безопасно нельзя
		}
		out = append(out, domain.AdminTable{Name: t.Name, NameRu: t.NameRu, PK: t.PK})
	}
	return out, nil
}

// Columns возвращает колонки таблицы в порядке схемы (для грида и формы).
func (r *AdminTablesRepository) Columns(ctx context.Context, table string) ([]domain.AdminColumn, error) {
	var rows []struct {
		Name     string `gorm:"column:column_name"`
		Label    string `gorm:"column:label"`
		DataType string `gorm:"column:data_type"`
		Nullable string `gorm:"column:is_nullable"`
		Default  *string
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT c.column_name, c.data_type, c.is_nullable, c.column_default AS default,
		       COALESCE(col_description(
		           (quote_ident(c.table_schema) || '.' || quote_ident(c.table_name))::regclass,
		           c.ordinal_position), '') AS label
		  FROM information_schema.columns c
		 WHERE c.table_schema = current_schema() AND c.table_name = ?
		 ORDER BY c.ordinal_position`, table).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("таблица %q не найдена в схеме", table)
	}
	out := make([]domain.AdminColumn, len(rows))
	for i, c := range rows {
		kind := "text"
		switch c.DataType {
		case "smallint", "integer", "bigint", "numeric", "real", "double precision":
			kind = "number"
		case "boolean":
			kind = "boolean"
		}
		out[i] = domain.AdminColumn{
			Name:     c.Name,
			Label:    c.Label,
			Kind:     kind,
			Required: c.Nullable == "NO" && c.Default == nil,
			Hidden:   c.Name == "created_at" || c.Name == "updated_at",
		}
	}
	return out, nil
}

// Rows читает все строки таблицы (ORDER BY ключ). Значения приводятся к
// JSON-дружелюбным типам ([]byte → string, время → МСК-строка без TZ).
func (r *AdminTablesRepository) Rows(ctx context.Context, table string, pk string) ([]domain.AdminRow, error) {
	sqlRows, err := r.db.WithContext(ctx).
		Raw("SELECT * FROM " + quoteIdent(table) + " ORDER BY " + quoteIdent(pk)).Rows()
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, err
	}
	var out []domain.AdminRow
	for sqlRows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := domain.AdminRow{}
		for i, c := range cols {
			row[c] = jsonValue(vals[i])
		}
		out = append(out, row)
	}
	return out, sqlRows.Err()
}

// jsonValue приводит значение драйвера к JSON-дружелюбному виду.
func jsonValue(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02T15:04:05") // МСК naive — канон проекта
	default:
		return v
	}
}

// Insert добавляет строку: только известные колонки (cols), ключ-serial не задаётся.
func (r *AdminTablesRepository) Insert(ctx context.Context, table string, cols []domain.AdminColumn, values domain.AdminRow) error {
	names := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	ph := make([]string, 0, len(values))
	for _, c := range cols {
		v, ok := values[c.Name]
		if !ok {
			continue
		}
		names = append(names, quoteIdent(c.Name))
		args = append(args, v)
		ph = append(ph, "?")
	}
	if len(names) == 0 {
		return fmt.Errorf("нет полей для вставки")
	}
	return r.db.WithContext(ctx).Exec(
		"INSERT INTO "+quoteIdent(table)+" ("+strings.Join(names, ",")+") VALUES ("+strings.Join(ph, ",")+")",
		args...).Error
}

// Update правит строку по ключу: SET только по переданным известным колонкам.
func (r *AdminTablesRepository) Update(ctx context.Context, table string, pk string, id string, cols []domain.AdminColumn, values domain.AdminRow) error {
	sets := make([]string, 0, len(values))
	args := make([]any, 0, len(values)+1)
	for _, c := range cols {
		if c.Name == pk {
			continue // ключ не правится
		}
		v, ok := values[c.Name]
		if !ok {
			continue
		}
		sets = append(sets, quoteIdent(c.Name)+" = ?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return fmt.Errorf("нет полей для обновления")
	}
	args = append(args, id)
	res := r.db.WithContext(ctx).Exec(
		"UPDATE "+quoteIdent(table)+" SET "+strings.Join(sets, ", ")+" WHERE "+quoteIdent(pk)+"::text = ?",
		args...)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("строка %s=%s не найдена", pk, id)
	}
	return nil
}

// Delete удаляет строку по ключу.
func (r *AdminTablesRepository) Delete(ctx context.Context, table string, pk string, id string) error {
	res := r.db.WithContext(ctx).Exec(
		"DELETE FROM "+quoteIdent(table)+" WHERE "+quoteIdent(pk)+"::text = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("строка %s=%s не найдена", pk, id)
	}
	return nil
}

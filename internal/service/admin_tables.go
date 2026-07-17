package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// ErrTableNotEditable — таблица не в реестре list_tables (или editable=false).
var ErrTableNotEditable = errors.New("таблица не входит в реестр редактируемых")

// AdminTables — админ-редактор справочников: универсальный CRUD по реестру
// dpport.list_tables (перенос эталона gtport). Слой валидирует имя таблицы по
// реестру, отбрасывает неизвестные поля и приводит JSON-типы к типам колонок;
// сам SQL — в port.AdminTablesRepository. Кэши БД не перечитывает: применение
// правок к снимку — отдельная явная кнопка «Обновить справочники» (dict_reload).
type AdminTables struct {
	repo port.AdminTablesRepository
}

func NewAdminTables(repo port.AdminTablesRepository) *AdminTables {
	return &AdminTables{repo: repo}
}

// Tables — список редактируемых таблиц (для селектора страницы «Админ»).
func (s *AdminTables) Tables(ctx context.Context) ([]domain.AdminTable, error) {
	return s.repo.ListTables(ctx)
}

// TableData — колонки и все строки таблицы.
func (s *AdminTables) TableData(ctx context.Context, table string) (domain.AdminTable, []domain.AdminColumn, []domain.AdminRow, error) {
	t, cols, err := s.resolve(ctx, table)
	if err != nil {
		return domain.AdminTable{}, nil, nil, err
	}
	rows, err := s.repo.Rows(ctx, t.Name, t.PK)
	if err != nil {
		return domain.AdminTable{}, nil, nil, err
	}
	return t, cols, rows, nil
}

// Create добавляет строку (ключ-колонка игнорируется — её даёт БД/оператор явно).
func (s *AdminTables) Create(ctx context.Context, table string, values domain.AdminRow) error {
	t, cols, err := s.resolve(ctx, table)
	if err != nil {
		return err
	}
	vals, err := coerceValues(cols, values, t.PK, true)
	if err != nil {
		return err
	}
	return s.repo.Insert(ctx, t.Name, cols, vals)
}

// Update правит строку по ключу.
func (s *AdminTables) Update(ctx context.Context, table, id string, values domain.AdminRow) error {
	t, cols, err := s.resolve(ctx, table)
	if err != nil {
		return err
	}
	vals, err := coerceValues(cols, values, t.PK, false)
	if err != nil {
		return err
	}
	return s.repo.Update(ctx, t.Name, t.PK, id, cols, vals)
}

// Delete удаляет строку по ключу.
func (s *AdminTables) Delete(ctx context.Context, table, id string) error {
	t, _, err := s.resolve(ctx, table)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, t.Name, t.PK, id)
}

// resolve проверяет таблицу по реестру и читает её колонки (с пометкой ключа).
func (s *AdminTables) resolve(ctx context.Context, table string) (domain.AdminTable, []domain.AdminColumn, error) {
	tables, err := s.repo.ListTables(ctx)
	if err != nil {
		return domain.AdminTable{}, nil, err
	}
	for _, t := range tables {
		if t.Name != table {
			continue
		}
		cols, err := s.repo.Columns(ctx, t.Name)
		if err != nil {
			return domain.AdminTable{}, nil, err
		}
		for i := range cols {
			cols[i].PK = cols[i].Name == t.PK
		}
		return t, cols, nil
	}
	return domain.AdminTable{}, nil, fmt.Errorf("%w: %s", ErrTableNotEditable, table)
}

// coerceValues отбрасывает неизвестные поля и приводит JSON-значения к типам
// колонок (number: целые float64 → int64 — драйвер не кладёт float в bigint).
// skipPK: при создании ключ-serial не задаётся руками.
func coerceValues(cols []domain.AdminColumn, values domain.AdminRow, pk string, skipPK bool) (domain.AdminRow, error) {
	byName := map[string]domain.AdminColumn{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	out := domain.AdminRow{}
	for name, v := range values {
		c, ok := byName[name]
		if !ok {
			continue // неизвестное поле — игнорируем (фронт мог прислать служебные)
		}
		if skipPK && c.Name == pk {
			continue
		}
		cv, err := coerceValue(c, v)
		if err != nil {
			return nil, fmt.Errorf("поле %s: %w", name, err)
		}
		out[name] = cv
	}
	for _, c := range cols {
		if c.Required && !c.PK {
			if _, ok := out[c.Name]; !ok {
				return nil, fmt.Errorf("поле %s обязательно", c.Name)
			}
		}
	}
	return out, nil
}

func coerceValue(c domain.AdminColumn, v any) (any, error) {
	if v == nil {
		if c.Required {
			return nil, fmt.Errorf("обязательное поле пустое")
		}
		return nil, nil
	}
	switch c.Kind {
	case "number":
		switch t := v.(type) {
		case float64:
			if t == math.Trunc(t) {
				return int64(t), nil
			}
			return t, nil
		case string:
			if t == "" {
				return nil, fmt.Errorf("число не задано")
			}
			if n, err := strconv.ParseInt(t, 10, 64); err == nil {
				return n, nil
			}
			f, err := strconv.ParseFloat(t, 64)
			if err != nil {
				return nil, fmt.Errorf("не число: %q", t)
			}
			return f, nil
		default:
			return nil, fmt.Errorf("не число: %T", v)
		}
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("не логическое значение: %T", v)
	default: // text
		if s, ok := v.(string); ok {
			return s, nil
		}
		return fmt.Sprintf("%v", v), nil
	}
}

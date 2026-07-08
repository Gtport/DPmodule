package plan

import "fmt"

// Parser — контракт парсера плана: сырые строки листа Excel → PlanDoc. Реализация
// по умолчанию — GridParser (grid.go). Для станции с принципиально иным форматом
// листа можно зарегистрировать свой парсер, не трогая ядро (см. RegisterParser).
type Parser interface {
	Parse(rows [][]string, sourceFile string) (*PlanDoc, error)
}

// ParserFactory строит Parser из профиля станции.
type ParserFactory func(Profile) Parser

// customParsers — реестр парсеров под нестандартные форматы листов. Пусто по
// умолчанию: все станции обслуживает generic-парсер. Кастомный парсер станции
// самрегистрируется через RegisterParser в init() своего файла — так добавление
// станции с иным форматом не требует правок в Resolve/GridParser.
var customParsers = map[string]ParserFactory{}

// RegisterParser привязывает кастомный парсер к коду станции. Вызывается из
// init() файла парсера конкретной станции.
func RegisterParser(planCode string, f ParserFactory) {
	customParsers[planCode] = f
}

// Resolve возвращает готовый парсер для станции: профиль + (кастомный парсер,
// если зарегистрирован, иначе — generic GridParser). Ошибка — только если у
// станции нет профиля.
func Resolve(planCode string) (Parser, error) {
	prof, err := ResolveProfile(planCode)
	if err != nil {
		return nil, err
	}
	if f, ok := customParsers[prof.PlanCode]; ok {
		return f(prof), nil
	}
	return NewGridParser(prof), nil
}

// ParseFile — удобная обёртка: читает лист файла и разбирает его парсером станции.
func ParseFile(path, planCode string) (*PlanDoc, error) {
	p, err := Resolve(planCode)
	if err != nil {
		return nil, err
	}
	rows, err := ReadPlanSheet(path)
	if err != nil {
		return nil, fmt.Errorf("plan: чтение %q: %w", path, err)
	}
	return p.Parse(rows, path)
}

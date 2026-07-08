package plan

import (
	"fmt"
	"strings"
)

// Profile — настроечный портрет станции плана. Всё, что отличает один порт от
// другого при разборе «новой формы», сведено сюда. В GTport это было зашито в
// отдельные файлы plan_ma_parser_new.go / plan_nk_parser_new.go; здесь — данные.
//
// Пока профили встроены (builtinProfiles) как эталон GTport. Позже источником
// профилей станет настроечная таблица (переопределение из БД) — точка расширения
// в ResolveProfile, ядро парсера при этом не меняется.
type Profile struct {
	// PlanCode — код станции: «ma», «nk», …
	PlanCode string

	// OurTerminals — ключевые слова имён терминалов, чьи вагоны идут в Activ
	// (цель скоринга при сопоставлении с дислокацией). Сравнение — по вхождению
	// подстроки в ВЕРХНЕМ регистре имени терминала из файла.
	//
	// Эталон GTport:
	//   MA: Activ = НМТП (уголь+металл+чугун) + АТТИС
	//   NK: Activ = только НМТП
	OurTerminals []string
}

// isOurTerminal сообщает, относится ли терминал с именем termName к «нашим»
// причалам профиля (вклад в Activ). Сравнение регистронезависимое, по вхождению.
func (p Profile) isOurTerminal(termName string) bool {
	up := strings.ToUpper(strings.TrimSpace(termName))
	for _, kw := range p.OurTerminals {
		if strings.Contains(up, strings.ToUpper(kw)) {
			return true
		}
	}
	return false
}

// builtinProfiles — встроенные профили станций (эталон GTport). Ключ — PlanCode.
var builtinProfiles = map[string]Profile{
	"ma": {
		PlanCode:     "ma",
		OurTerminals: []string{"НАХОДКИНСКИЙ", "НМТП", "АТТИС"},
	},
	"nk": {
		PlanCode:     "nk",
		OurTerminals: []string{"НАХОДКИНСКИЙ", "НМТП"},
	},
}

// ResolveProfile возвращает профиль станции по коду. Точка расширения: позже
// здесь появится переопределение из настроечной таблицы (БД имеет приоритет над
// встроенным). Пока — только встроенные профили.
func ResolveProfile(planCode string) (Profile, error) {
	p, ok := builtinProfiles[strings.ToLower(strings.TrimSpace(planCode))]
	if !ok {
		return Profile{}, fmt.Errorf("plan: неизвестный код станции %q (нет профиля)", planCode)
	}
	return p, nil
}

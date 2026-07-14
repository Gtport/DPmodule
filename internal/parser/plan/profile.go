package plan

import (
	"fmt"
	"strings"
	"sync"
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

	// MatchRequiresNaznach — при записи результата матча (write-back) вагон
	// обновляется только если его Naznach совпадает с подгруппой. Единственное
	// поведенческое отличие между портами: NK=true (эталонный shouldUpdateWagonNK),
	// MA=false (shouldUpdateWagon — сверяет только IdDisl+IndexMain).
	MatchRequiresNaznach bool
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

// builtinProfiles — встроенные профили как FALLBACK для offline-утилит (cmd/planrun)
// и тестов без БД. В проде источник — настроечная таблица plan_profile: сервер на
// старте вызывает SetProfiles и переопределяет реестр. Значения = сид миграции 000023.
var builtinProfiles = map[string]Profile{
	"ma": {
		PlanCode:     "ma",
		OurTerminals: []string{"НАХОДКИНСКИЙ", "НМТП", "АТТИС"},
	},
	"nk": {
		PlanCode:             "nk",
		OurTerminals:         []string{"НАХОДКИНСКИЙ", "НМТП"},
		MatchRequiresNaznach: true,
	},
}

// registry — активный реестр профилей (ключ — PlanCode). По умолчанию builtin;
// сервер переопределяет из БД через SetProfiles. Под RWMutex — задел под горячую
// перезагрузку; в проде SetProfiles зовётся один раз до старта HTTP.
var (
	profMu   sync.RWMutex
	registry = builtinProfiles
)

// SetProfiles переопределяет реестр профилей из настроечной таблицы (plan_profile).
// Пустой набор игнорируется (остаётся fallback) — чтобы пустая/недоступная таблица
// не обнулила профили offline-утилит.
func SetProfiles(profiles map[string]Profile) {
	if len(profiles) == 0 {
		return
	}
	profMu.Lock()
	registry = profiles
	profMu.Unlock()
}

// ResolveProfile возвращает профиль станции по коду — из активного реестра
// (настроечная таблица в проде, builtin как fallback).
func ResolveProfile(planCode string) (Profile, error) {
	profMu.RLock()
	p, ok := registry[strings.ToLower(strings.TrimSpace(planCode))]
	profMu.RUnlock()
	if !ok {
		return Profile{}, fmt.Errorf("plan: неизвестный код станции %q (нет профиля)", planCode)
	}
	return p, nil
}

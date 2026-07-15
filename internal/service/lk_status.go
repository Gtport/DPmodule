package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// LKFileInfo — один staged-файл ЛК в папке приёма (<baseDir>/lk/).
type LKFileInfo struct {
	Okpo         string           `json:"okpo"`
	Organisation string           `json:"organisation"`
	Terminals    []string         `json:"terminals"`
	FormationTS  domain.LocalTime `json:"formation_ts"`
	AgeMinutes   int              `json:"age_minutes"`
	Filename     string           `json:"filename"`
}

// Уровни и коды замечаний контроля приёма.
const (
	LKIssueBlock   = "block"   // обработка небезопасна
	LKIssueWarning = "warning" // можно обрабатывать, но обратить внимание

	LKCodeMissing = "missing" // нет файла ожидаемого грузополучателя
	LKCodeGap     = "gap"     // разрыв меток формирования (разные срезы)
	LKCodeStale   = "stale"   // файл устарел относительно «сейчас»
	LKCodeUnknown = "unknown" // файл с ОКПО, которого нет в справочнике ports
)

// LKIssue — одно замечание контроля приёма.
type LKIssue struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Okpo    string `json:"okpo,omitempty"`
	Message string `json:"message"`
}

// LKStatus — сводка staged-файлов ЛК и результат контроля (ingest_policy, §3.9).
// Ready = нет блокирующих замечаний и есть хотя бы один файл.
type LKStatus struct {
	CoArrivalGroup string       `json:"co_arrival_group"`
	Files          []LKFileInfo `json:"files"`
	Issues         []LKIssue    `json:"issues"`
	Ready          bool         `json:"ready"`
}

// Status перечисляет staged-файлы ЛК и прогоняет контроль приёма по ingest_policy:
// устаревание (staleness), разрыв меток формирования между файлами (paradox),
// полнота набора ожидаемых грузополучателей. «Сейчас» — по Москве (clock.Now()).
func (s *LKIntake) Status() (LKStatus, error) {
	st := LKStatus{Files: []LKFileInfo{}, Issues: []LKIssue{}}
	if ds, ok := s.cfg.DataSource("lk"); ok {
		st.CoArrivalGroup = ds.CoArrivalGroup
	}

	now := time.Time(clock.Now())
	dir := filepath.Join(s.baseDir, "lk")
	matches, _ := filepath.Glob(filepath.Join(dir, "*.xlsx"))

	present := make(map[string]bool)
	for _, m := range matches {
		base := filepath.Base(m)
		okpo, ts, ok := okpoTsFromFilename(base)
		if !ok {
			continue // не наш формат имени — игнорируем
		}
		fi := LKFileInfo{
			Okpo:        okpo,
			FormationTS: ts,
			Filename:    base,
			AgeMinutes:  int(now.Sub(time.Time(ts)).Minutes()),
		}
		if okpoNum, err := strconv.ParseInt(okpo, 10, 64); err == nil {
			if ports, ok := s.dir.PortsByOkpo(okpoNum); ok && len(ports) > 0 {
				fi.Organisation = ports[0].Organisation
				names := make([]string, 0, len(ports))
				for _, p := range ports {
					names = append(names, p.NameS)
				}
				fi.Terminals = names
			} else {
				st.Issues = append(st.Issues, LKIssue{
					Level: LKIssueWarning, Code: LKCodeUnknown, Okpo: okpo,
					Message: fmt.Sprintf("файл с неизвестным ОКПО %s (нет в справочнике ports)", okpo),
				})
			}
		}
		present[okpo] = true
		st.Files = append(st.Files, fi)
	}
	sort.Slice(st.Files, func(i, j int) bool { return okpoLess(st.Files[i].Okpo, st.Files[j].Okpo) })

	pol := s.cfg.Settings().IngestPolicy.Dislocation

	// 1. Устаревание — на каждый файл. БЛОК (не warning): гард обработки
	// (checkFreshness) отклоняет устаревшие файлы безусловно (без роль-исключения),
	// поэтому Ready обязан это отражать — иначе статус зелёный «готово», а обработка
	// падает 409. «Любой файл устарел» ⟺ «самый старый устарел» ⟺ гард отклонит.
	if pol.MaxStalenessMinutes > 0 {
		for _, f := range st.Files {
			if f.AgeMinutes > pol.MaxStalenessMinutes {
				st.Issues = append(st.Issues, LKIssue{
					Level: LKIssueBlock, Code: LKCodeStale, Okpo: f.Okpo,
					Message: fmt.Sprintf("файл устарел: возраст %d мин при допустимых %d — не годится для обновления дислокации", f.AgeMinutes, pol.MaxStalenessMinutes),
				})
			}
		}
	}

	// 2. Разрыв меток формирования между файлами (парадокс совместного среза).
	if pol.MaxGapMinutes > 0 && len(st.Files) > 1 {
		lo, hi := time.Time(st.Files[0].FormationTS), time.Time(st.Files[0].FormationTS)
		for _, f := range st.Files[1:] {
			t := time.Time(f.FormationTS)
			if t.Before(lo) {
				lo = t
			}
			if t.After(hi) {
				hi = t
			}
		}
		if gap := int(hi.Sub(lo).Minutes()); gap > pol.MaxGapMinutes {
			st.Issues = append(st.Issues, LKIssue{
				Level: LKIssueBlock, Code: LKCodeGap,
				Message: fmt.Sprintf("разрыв меток формирования %d мин > %d — файлы из разных срезов", gap, pol.MaxGapMinutes),
			})
		}
	}

	// 3. Полнота: у каждого ожидаемого грузополучателя (активный порт) — свой файл.
	for _, okpo := range s.dir.EnabledOkpos() {
		okpoStr := strconv.FormatInt(okpo, 10)
		if present[okpoStr] {
			continue
		}
		org := ""
		if ports, ok := s.dir.PortsByOkpo(okpo); ok && len(ports) > 0 {
			org = ports[0].Organisation
		}
		st.Issues = append(st.Issues, LKIssue{
			Level: LKIssueBlock, Code: LKCodeMissing, Okpo: okpoStr,
			Message: fmt.Sprintf("нет файла ожидаемого грузополучателя %s (%s)", okpoStr, org),
		})
	}

	st.Ready = len(st.Files) > 0 && !hasBlockingIssue(st.Issues)
	return st, nil
}

// okpoLess упорядочивает ОКПО по числовому значению (строки разной длины иначе
// сортируются лексикографически неверно: "10230304" < "1126022").
func okpoLess(a, b string) bool {
	ai, aerr := strconv.ParseInt(a, 10, 64)
	bi, berr := strconv.ParseInt(b, 10, 64)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

func hasBlockingIssue(issues []LKIssue) bool {
	for _, i := range issues {
		if i.Level == LKIssueBlock {
			return true
		}
	}
	return false
}

// okpoTsFromFilename разбирает имя <ОКПО>_<ДДММГГ-ЧЧММ>.xlsx на ОКПО и метку
// формирования (naive, как записано приёмом). ОКПО — до последнего '_'.
func okpoTsFromFilename(name string) (string, domain.LocalTime, bool) {
	stem, ok := strings.CutSuffix(name, ".xlsx")
	if !ok {
		return "", domain.LocalTime{}, false
	}
	i := strings.LastIndex(stem, "_")
	if i <= 0 || i+1 >= len(stem) {
		return "", domain.LocalTime{}, false
	}
	t, err := time.Parse("020106-1504", stem[i+1:])
	if err != nil {
		return "", domain.LocalTime{}, false
	}
	return stem[:i], domain.LocalTime(t), true
}

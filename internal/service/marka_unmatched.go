package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// MarkaUnmatchedService — «Без атрибуции» (карточка «Информация»): гружёные вагоны
// снимка, не сматченные со словарём marka (Gruzotpr пуст после S2-3), агрегированно
// по ключу матчинга (ОКПО, станция отправления, группа груза) с разворотом до
// вагона и причиной промаха. Назначение атрибуции — Assign: правка снимка через
// канонический MutateSnapshot + дозаполнение vagon_history; с галкой AddToMarka
// комбинация записывается в словарь и закрывается навсегда (следующие загрузки
// матчатся сами), без неё правка живёт в пределах рейса (carry-over переносит
// груз-блок при непустом Gruzotpr, новый рейс начинается с чистого листа).
type MarkaUnmatchedService struct {
	actual  *ActualCache
	dir     *DirectoryCache
	proc    *LKProcessor
	history port.HistoryRepository
}

func NewMarkaUnmatchedService(actual *ActualCache, dir *DirectoryCache, proc *LKProcessor, history port.HistoryRepository) *MarkaUnmatchedService {
	return &MarkaUnmatchedService{actual: actual, dir: dir, proc: proc, history: history}
}

// Причины промаха матчинга (классификация — как determineMarkaMissingComponents
// эталона gtlogic, но по строгому ключу DPmodule без частичного матча).
const (
	ReasonBadOkpo      = "bad_okpo"       // ОКПО отправителя в потоке пустое либо нечисловое
	ReasonBadStation   = "bad_station"    // код станции отправления пустой либо нечисловой
	ReasonNoCargoGroup = "no_cargo_group" // группа груза пуста: код ЕТСНГ вне словаря cargo
	ReasonNoMarka      = "no_marka"       // ключ полный, но комбинации в marka нет
)

// UnmatchedVagonDTO — вагон разворота группы. ID — id рейса (= vagon_history.id):
// по нему интерфейс открывает «Историю движения вагона».
type UnmatchedVagonDTO struct {
	ID          string            `json:"id"`
	Vagon       string            `json:"vagon"`
	Index       string            `json:"index"`
	StationOper string            `json:"station_oper"`
	OperS       string            `json:"oper_s"`
	TimeOp      *domain.LocalTime `json:"time_op"`
	Status      *int              `json:"status"`
	CodeCargo   string            `json:"code_cargo"`
	CargoS      string            `json:"cargo_s"`
	Ves         *float64          `json:"ves"`
	GruzpolS    string            `json:"gruzpol_s"`
	Naznach     string            `json:"naznach"`
	DateNach    *domain.LocalTime `json:"date_nach"`
}

// UnmatchedSuggestDTO — подсказка предзаполнения формы: атрибуция того же ОКПО
// с другой станции/группы (имя отправителя и клиент у известного ОКПО обычно одни).
type UnmatchedSuggestDTO struct {
	Gruzotpr string `json:"gruzotpr"`
	Client   string `json:"client"`
	Sms1     string `json:"sms_1"`
	Sms3     string `json:"sms_3"`
	Color    string `json:"color"`
}

// UnmatchedGroupDTO — группа несматченных вагонов: одна комбинация ключа матчинга.
// Okpo/StationKod — сырые строки потока (у групп с причиной bad_* они и есть
// диагноз); CanAddToMarka — ключ полный и числовой, комбинацию можно завести в
// словарь. OkpoInMarka/StationInMarka/GroupInMarka заполняются при no_marka:
// какие компоненты ключа в словаре уже встречаются (чего именно не хватает).
type UnmatchedGroupDTO struct {
	Key            string               `json:"key"` // okpo|station_kod|cargo_group
	Okpo           string               `json:"okpo"`
	StationKod     string               `json:"station_kod"`
	StationNach    string               `json:"station_nach"`
	CargoGroup     string               `json:"cargo_group"`
	CargoS         string               `json:"cargo_s"`
	VagonCount     int                  `json:"vagon_count"`
	Reason         string               `json:"reason"`
	OkpoInMarka    bool                 `json:"okpo_in_marka"`
	StationInMarka bool                 `json:"station_in_marka"`
	GroupInMarka   bool                 `json:"group_in_marka"`
	CanAddToMarka  bool                 `json:"can_add_to_marka"`
	Suggest        *UnmatchedSuggestDTO `json:"suggest,omitempty"`
	Vagons         []UnmatchedVagonDTO  `json:"vagons"`
}

// Groups — несматченные вагоны снимка группами по комбинации ключа, крупные группы
// первыми. Порожние под погрузку (PorozhInbound) не показываются: у них нет группы
// груза, матч с marka невозможен в принципе — это не пробел справочника.
func (s *MarkaUnmatchedService) Groups() []UnmatchedGroupDTO {
	rows := s.actual.All()
	type gkey struct{ okpo, station, group string }
	groups := map[gkey]*UnmatchedGroupDTO{}
	var order []gkey
	for i := range rows {
		r := &rows[i]
		if r.Gruzotpr != "" || r.Vagon == "" {
			continue
		}
		if s.dir.PorozhInbound(r) {
			continue
		}
		okpoRaw := strings.TrimSpace(r.GruzotprOkpo)
		stRaw := strings.TrimSpace(r.CodeStationNach)
		k := gkey{okpoRaw, stRaw, r.CargoGroup}
		g, ok := groups[k]
		if !ok {
			g = newUnmatchedGroup(okpoRaw, stRaw, r.CargoGroup, s.dir)
			groups[k] = g
			order = append(order, k)
		}
		g.VagonCount++
		if g.StationNach == "" {
			g.StationNach = r.StationNach
		}
		if g.CargoS == "" {
			g.CargoS = r.CargoS
		}
		g.Vagons = append(g.Vagons, UnmatchedVagonDTO{
			ID: r.ID, Vagon: r.Vagon, Index: r.Index,
			StationOper: r.StationOper, OperS: r.OperS, TimeOp: r.TimeOp,
			Status: r.Status, CodeCargo: r.CodeCargo, CargoS: r.CargoS,
			Ves: r.Ves, GruzpolS: r.GruzpolS, Naznach: r.Naznach,
			DateNach: r.DateNach,
		})
	}

	out := make([]UnmatchedGroupDTO, 0, len(order))
	for _, k := range order {
		g := groups[k]
		// ActualCache.All() отдаёт вагоны в порядке мапы — сортируем для стабильного экрана.
		sort.Slice(g.Vagons, func(i, j int) bool { return g.Vagons[i].Vagon < g.Vagons[j].Vagon })
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].VagonCount > out[j].VagonCount })
	return out
}

// newUnmatchedGroup — заголовок группы: классификация причины промаха и подсказка
// предзаполнения. Порядок проверок — как guard'ы enrichFromMarka: ОКПО → станция →
// группа → сама комбинация.
func newUnmatchedGroup(okpoRaw, stRaw, group string, dir *DirectoryCache) *UnmatchedGroupDTO {
	g := &UnmatchedGroupDTO{
		Key: okpoRaw + "|" + stRaw + "|" + group,
		Okpo: okpoRaw, StationKod: stRaw, CargoGroup: group,
	}
	okpo, err1 := strconv.ParseInt(okpoRaw, 10, 64)
	st, err2 := strconv.ParseInt(stRaw, 10, 64)
	okpoOk := okpoRaw != "" && err1 == nil
	stOk := stRaw != "" && err2 == nil
	switch {
	case !okpoOk:
		g.Reason = ReasonBadOkpo
	case !stOk:
		g.Reason = ReasonBadStation
	case group == "":
		g.Reason = ReasonNoCargoGroup
	default:
		g.Reason = ReasonNoMarka
		g.OkpoInMarka, g.StationInMarka, g.GroupInMarka = dir.MarkaKeyPresence(okpo, st, group)
	}
	g.CanAddToMarka = okpoOk && stOk && group != ""
	if okpoOk {
		if m, found := dir.MarkaSampleByOkpo(okpo); found {
			g.Suggest = &UnmatchedSuggestDTO{
				Gruzotpr: m.Shipper, Client: m.Client,
				Sms1: m.Sms1, Sms3: m.Sms3, Color: m.Color,
			}
		}
	}
	if stOk && g.StationNach == "" {
		if stn, ok := dir.GetStationByKod(int(st)); ok {
			g.StationNach = stn.Name
		}
	}
	return g
}

// ErrBadAssign — ошибка валидации запроса назначения атрибуции (→ 400).
var ErrBadAssign = fmt.Errorf("некорректный запрос назначения атрибуции")

// MarkaAssignRequest — назначение атрибуции группе несматченных вагонов.
// Okpo/StationKod/CargoGroup — сырой ключ группы из Groups (дословно).
type MarkaAssignRequest struct {
	Okpo       string `json:"okpo"`
	StationKod string `json:"station_kod"`
	CargoGroup string `json:"cargo_group"`
	Gruzotpr   string `json:"gruzotpr"`
	Client     string `json:"client"`
	Sms1       string `json:"sms_1"`
	Sms3       string `json:"sms_3"`
	Color      string `json:"color"`
	AddToMarka bool   `json:"add_to_marka"`
}

// MarkaAssignResult — итог назначения для фронта.
type MarkaAssignResult struct {
	Updated       int  `json:"updated"`        // вагонов заполнено в снимке
	HistoryFilled int  `json:"history_filled"` // строк vagon_history дозаполнено
	MarkaSaved    bool `json:"marka_saved"`    // комбинация записана в словарь
}

// Assign — назначение атрибуции комбинации ключа. Два пути:
//   - AddToMarka: строка пишется в словарь (upsert), затем канонический
//     ReloadDirectories — перезагрузка словарей + пересчёт снимка + дозаполнение
//     истории; комбинация закрыта навсегда, журнал — dict_reload;
//   - разовая правка: только вагоны этой комбинации в снимке через MutateSnapshot
//     (журнал marka_assign) + дозаполнение их строк vagon_history. Правка живёт
//     до конца рейса (carry-over), новый рейс той же комбинации снова несматчен.
func (s *MarkaUnmatchedService) Assign(ctx context.Context, req MarkaAssignRequest) (MarkaAssignResult, error) {
	req.Gruzotpr = strings.TrimSpace(req.Gruzotpr)
	req.Client = strings.TrimSpace(req.Client)
	req.Sms1 = strings.TrimSpace(req.Sms1)
	req.Sms3 = strings.TrimSpace(req.Sms3)
	req.Color = strings.TrimSpace(req.Color)
	if req.Gruzotpr == "" {
		return MarkaAssignResult{}, fmt.Errorf("%w: грузоотправитель обязателен", ErrBadAssign)
	}
	if req.Color != "" && !markColorRe.MatchString(req.Color) {
		return MarkaAssignResult{}, fmt.Errorf("%w: цвет ожидается в виде #rrggbb", ErrBadAssign)
	}
	if s.proc == nil {
		return MarkaAssignResult{}, fmt.Errorf("%w: конвейер дислокации не собран", ErrNotReady)
	}

	if req.AddToMarka {
		okpo, err1 := strconv.ParseInt(strings.TrimSpace(req.Okpo), 10, 64)
		st, err2 := strconv.ParseInt(strings.TrimSpace(req.StationKod), 10, 64)
		if err1 != nil || err2 != nil || req.CargoGroup == "" {
			return MarkaAssignResult{}, fmt.Errorf(
				"%w: в словарь можно добавить только полную комбинацию (числовые ОКПО и код станции, непустая группа груза)", ErrBadAssign)
		}
		station := ""
		if stn, ok := s.dir.GetStationByKod(int(st)); ok {
			station = stn.Name
		}
		if err := s.dir.SaveMarka(ctx, domain.Marka{
			Okpo: okpo, StationKod: st, Station: station, CargoGroup: req.CargoGroup,
			Shipper: req.Gruzotpr, Client: req.Client,
			Sms1: req.Sms1, Sms3: req.Sms3, Color: req.Color,
		}); err != nil {
			return MarkaAssignResult{}, fmt.Errorf("запись в marka: %w", err)
		}
		res, err := s.proc.ReloadDirectories(ctx)
		if err != nil {
			return MarkaAssignResult{}, err
		}
		return MarkaAssignResult{
			Updated:       res.Filled + res.FilledByTrain,
			HistoryFilled: res.HistoryFilled,
			MarkaSaved:    true,
		}, nil
	}

	var attrs []domain.HistoryAttribution
	n, _, _, err := s.proc.MutateSnapshot(ctx, "marka_assign", map[string]any{
		"okpo": req.Okpo, "station_kod": req.StationKod,
		"cargo_group": req.CargoGroup, "gruzotpr": req.Gruzotpr,
	}, func(all []domain.Dislocation) int {
		var updated int
		updated, attrs = applyManualAttribution(all, req, s.dir)
		return updated
	})
	if err != nil {
		return MarkaAssignResult{}, err
	}
	filled := 0
	if s.history != nil && len(attrs) > 0 {
		if filled, err = s.history.FillAttribution(ctx, attrs); err != nil {
			return MarkaAssignResult{}, fmt.Errorf("дозаполнение истории: %w", err)
		}
	}
	return MarkaAssignResult{Updated: n, HistoryFilled: filled}, nil
}

// applyManualAttribution — правка копии снимка: вагонам комбинации без атрибуции
// проставляются поля запроса (пустые не затирают — семантика applyMarka), sms_2
// пересчитывается. Возвращает число правок и атрибуцию для дозаполнения истории.
func applyManualAttribution(all []domain.Dislocation, req MarkaAssignRequest, dir *DirectoryCache) (int, []domain.HistoryAttribution) {
	now := clock.Now()
	n := 0
	var attrs []domain.HistoryAttribution
	for i := range all {
		r := &all[i]
		if r.Gruzotpr != "" || r.Vagon == "" {
			continue
		}
		if strings.TrimSpace(r.GruzotprOkpo) != req.Okpo ||
			strings.TrimSpace(r.CodeStationNach) != req.StationKod ||
			r.CargoGroup != req.CargoGroup {
			continue
		}
		if dir != nil && dir.PorozhInbound(r) {
			continue
		}
		r.Gruzotpr = req.Gruzotpr
		if req.Client != "" {
			r.Client = req.Client
		}
		if req.Sms1 != "" {
			r.Sms1 = req.Sms1
		}
		if req.Sms3 != "" {
			r.Sms3 = req.Sms3
		}
		if req.Color != "" {
			r.Color = req.Color
		}
		r.Sms2 = joinNonEmpty(r.Sms1, r.CargoSms)
		r.UpdatedAt = now
		n++
		if key, ok := historyTripKey(r); ok {
			attrs = append(attrs, domain.HistoryAttribution{
				TripKey: key, Gruzotpr: r.Gruzotpr, Client: r.Client,
				Sms1: r.Sms1, Sms2: r.Sms2, Sms3: r.Sms3, Color: r.Color,
			})
		}
	}
	return n, attrs
}

// attributionRows — атрибуция всех атрибутированных вагонов снимка для дозаполнения
// vagon_history («Обновить справочники»): строки истории, вставленные до матча,
// остаются с пустым грузоотправителем навсегда — historyUpdateFields атрибуцию не
// ведёт; FillAttribution закрывает их по guard'у gruzotpr = ''.
func attributionRows(all []domain.Dislocation) []domain.HistoryAttribution {
	var out []domain.HistoryAttribution
	for i := range all {
		r := &all[i]
		if r.Gruzotpr == "" {
			continue
		}
		key, ok := historyTripKey(r)
		if !ok {
			continue
		}
		out = append(out, domain.HistoryAttribution{
			TripKey: key, Gruzotpr: r.Gruzotpr, Client: r.Client,
			Sms1: r.Sms1, Sms2: r.Sms2, Sms3: r.Sms3, Color: r.Color,
		})
	}
	return out
}

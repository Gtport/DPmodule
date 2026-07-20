package parser

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Форматы времени контракта reference/update. Проверены на боевых ответах обоих
// клиентов 2026-07-20 (117 памяток, 2012 вагонов): аномалий нет, поля строго в
// этих форматах, опциональные поля вагона в JSON просто отсутствуют (не null).
const (
	refDateCreateLayout = "01-02-2006" // DATE_CREATE — американский порядок MM-DD-YYYY
	refDayMonthLayout   = "02.01"      // даты вагона — «дд.мм», год не передаётся
	refClockLayout      = "15:04"      // времена вагона — «чч:мм»
)

// ReferenceUpdate — результат разбора ответа reference/update одного клиента.
// LastUpdate — курсор провайдера как пришёл («2026-07-20 01:48:37.900»): не
// переформатируем, он дословно уйдёт в query-параметр следующего инкремента.
type ReferenceUpdate struct {
	LastUpdate string
	Pamyatki   []domain.Pamyatka
}

// refEnvelope/refPamyatka/refVagon — сырой контракт источника.
type refEnvelope struct {
	LastUpdate string        `json:"LAST_UPDATE"`
	Pamyatki   []refPamyatka `json:"PAMYATKI"`
}

type refPamyatka struct {
	NumberPamyatka string     `json:"NUMBER_PAMYATKA"`
	DateCreate     string     `json:"DATE_CREATE"`
	OperationType  string     `json:"OPERATION_TYPE"`
	GetPlace       string     `json:"GET_PLACE"`
	NameStation    string     `json:"NAME_STATION"`
	PathOwnerOkpo  string     `json:"PATH_OWNER_OKPO"`
	Vagons         []refVagon `json:"VAGONS"`
}

type refVagon struct {
	NumberVagon     string `json:"NUMBER_VAGON"`
	GrOperationType string `json:"GR_OPERATION_TYPE"`
	GetInDate       string `json:"GET_IN_DATE"`
	GetInTime       string `json:"GET_IN_TIME"`
	ReportDate      string `json:"REPORT_DATE"`
	ReportTime      string `json:"REPORT_TIME"`
	GetOutDate      string `json:"GET_OUT_DATE"`
	GetOutTime      string `json:"GET_OUT_TIME"`
}

// ParseReferenceUpdate разбирает ответ <client>/reference/update в доменные
// памятки. client — код клиента провайдера из пути запроса (в теле его нет).
// Ошибка любой памятки прерывает весь разбор: курсор last_update нельзя двигать,
// пока пачка не разобрана целиком, частичный результат тут опаснее отказа.
func ParseReferenceUpdate(raw []byte, client string) (ReferenceUpdate, error) {
	var env refEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return ReferenceUpdate{}, fmt.Errorf("reference/update (%s): парсинг JSON: %w", client, err)
	}
	out := ReferenceUpdate{
		LastUpdate: env.LastUpdate,
		Pamyatki:   make([]domain.Pamyatka, 0, len(env.Pamyatki)),
	}
	for i := range env.Pamyatki {
		p, err := convertPamyatka(&env.Pamyatki[i], client)
		if err != nil {
			return ReferenceUpdate{}, err
		}
		out.Pamyatki = append(out.Pamyatki, p)
	}
	return out, nil
}

func convertPamyatka(src *refPamyatka, client string) (domain.Pamyatka, error) {
	created, err := time.Parse(refDateCreateLayout, src.DateCreate)
	if err != nil {
		return domain.Pamyatka{}, fmt.Errorf("памятка %s/%s: DATE_CREATE %q не в формате MM-DD-YYYY: %w",
			client, src.NumberPamyatka, src.DateCreate, err)
	}
	p := domain.Pamyatka{
		Client:        client,
		Number:        src.NumberPamyatka,
		DateCreate:    domain.NewLocalTime(created),
		OperationType: src.OperationType,
		GetPlace:      src.GetPlace,
		NameStation:   src.NameStation,
		PathOwnerOkpo: src.PathOwnerOkpo,
		Vagons:        make([]domain.PamyatkaVagon, 0, len(src.Vagons)),
	}
	for _, v := range src.Vagons {
		getIn, err := vagonTime(v.GetInDate, v.GetInTime, created, "GET_IN", client, src.NumberPamyatka)
		if err != nil {
			return domain.Pamyatka{}, err
		}
		report, err := vagonTime(v.ReportDate, v.ReportTime, created, "REPORT", client, src.NumberPamyatka)
		if err != nil {
			return domain.Pamyatka{}, err
		}
		getOut, err := vagonTime(v.GetOutDate, v.GetOutTime, created, "GET_OUT", client, src.NumberPamyatka)
		if err != nil {
			return domain.Pamyatka{}, err
		}
		p.Vagons = append(p.Vagons, domain.PamyatkaVagon{
			Vagon:           v.NumberVagon,
			GrOperationType: v.GrOperationType,
			GetIn:           getIn,
			Report:          report,
			GetOut:          getOut,
		})
	}
	return p, nil
}

// vagonTime собирает naive-время вагона из пары «дд.мм» + «чч:мм». Год источник
// не передаёт — берём тот из соседних с датой создания памятки (created−1,
// created, created+1), при котором дата ближе всего к дате создания: памятка,
// созданная в январе, может нести декабрьские времена, и наоборот. При равном
// расстоянии предпочитаем год создания. Оба поля пары пустые → nil (поле
// опционально); заполнено только одно — ошибка целостности.
func vagonTime(datePart, timePart string, created time.Time, field, client, number string) (*domain.LocalTime, error) {
	if datePart == "" && timePart == "" {
		return nil, nil
	}
	if datePart == "" || timePart == "" {
		return nil, fmt.Errorf("памятка %s/%s: %s: дата и время должны идти парой (дата %q, время %q)",
			client, number, field, datePart, timePart)
	}
	dm, err := time.Parse(refDayMonthLayout, datePart)
	if err != nil {
		return nil, fmt.Errorf("памятка %s/%s: %s дата %q не в формате «дд.мм»: %w",
			client, number, field, datePart, err)
	}
	hm, err := time.Parse(refClockLayout, timePart)
	if err != nil {
		return nil, fmt.Errorf("памятка %s/%s: %s время %q не в формате «чч:мм»: %w",
			client, number, field, timePart, err)
	}

	var best time.Time
	var bestDiff time.Duration
	for _, year := range []int{created.Year(), created.Year() - 1, created.Year() + 1} {
		t := time.Date(year, dm.Month(), dm.Day(), hm.Hour(), hm.Minute(), 0, 0, time.UTC)
		if t.Month() != dm.Month() {
			continue // 29.02 в невисокосном году: time.Date отнормализовал бы в 1 марта
		}
		diff := t.Sub(created)
		if diff < 0 {
			diff = -diff
		}
		if best.IsZero() || diff < bestDiff {
			best, bestDiff = t, diff
		}
	}
	if best.IsZero() {
		return nil, fmt.Errorf("памятка %s/%s: %s дата %q не существует ни в одном соседнем году",
			client, number, field, datePart)
	}
	return domain.NewLocalTime(best), nil
}

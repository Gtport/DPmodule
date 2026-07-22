package service

import (
	"context"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// MissingService — списки вагонов «сбоку от снимка» для интерфейса:
//   - пропавшие: записи-8 из таблицы кандидатов (status9) с последней известной
//     позицией; появляются в reconcileCandidates (S2-1b), уходят возвратом
//     вагона в поток либо автоочисткой по TTL (S2-1c);
//   - доноры перегруза: записи статуса 6 (таблица status6), у которых забирают
//     груз/назначение приёмники (S2-3c).
//
// Только чтение. Обе таблицы — копии структуры dislocation, поэтому у строк
// есть id рейса: интерфейс адресует им «Историю движения вагона».
type MissingService struct {
	status9 *Status9Cache
	status6 *Status6Cache
}

func NewMissingService(status9 *Status9Cache, status6 *Status6Cache) *MissingService {
	return &MissingService{status9: status9, status6: status6}
}

// MissingVagonDTO — строка экрана: последняя известная позиция + давность пропажи.
// ID — id рейса (= dislocation.id = vagon_history.id): по нему интерфейс
// открывает историю движения вагона.
type MissingVagonDTO struct {
	ID           string            `json:"id"`
	Vagon        string            `json:"vagon"`
	Index        string            `json:"index"`        // последний поездной индекс
	StationOper  string            `json:"station_oper"` // где видели в последний раз
	DorogaOper   string            `json:"doroga_oper"`
	OperS        string            `json:"oper_s"`  // последняя операция
	TimeOp       *domain.LocalTime `json:"time_op"` // время последней операции
	Naznach      string            `json:"naznach"` // терминал назначения
	GruzpolS     string            `json:"gruzpol_s"`
	StanNazn     string            `json:"stan_nazn"`
	CargoS       string            `json:"cargo_s"`
	Ves          *float64          `json:"ves"`
	DateDostav   *domain.LocalTime `json:"date_dostav"`
	MissingSince domain.LocalTime  `json:"missing_since"` // когда зафиксирована пропажа
	DaysMissing  int               `json:"days_missing"`  // полных суток с пропажи (от «сейчас» МСК)
}

// List — все пропавшие, свежие первыми (порядок — из репозитория).
func (s *MissingService) List(ctx context.Context) ([]MissingVagonDTO, error) {
	rows, err := s.status9.MissingRows(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Time(clock.Now())
	out := make([]MissingVagonDTO, 0, len(rows))
	for _, r := range rows {
		d := MissingVagonDTO{
			ID: r.ID, Vagon: r.Vagon, Index: r.Index,
			StationOper: r.StationOper, DorogaOper: r.DorogaOper,
			OperS: r.OperS, TimeOp: r.TimeOp,
			Naznach: r.Naznach, GruzpolS: r.GruzpolS, StanNazn: r.StanNazn,
			CargoS: r.CargoS, Ves: r.Ves, DateDostav: r.DateDostav,
			MissingSince: r.UpdatedAt,
		}
		if !r.UpdatedAt.IsZero() {
			d.DaysMissing = int(now.Sub(time.Time(r.UpdatedAt)).Hours() / 24)
		}
		out = append(out, d)
	}
	return out, nil
}

// Status6VagonDTO — строка списка доноров перегруза (статус 6): последняя
// известная позиция и груз, который у донора могут забрать приёмники.
type Status6VagonDTO struct {
	ID          string            `json:"id"`
	Vagon       string            `json:"vagon"`
	Index       string            `json:"index"`
	StationOper string            `json:"station_oper"`
	DorogaOper  string            `json:"doroga_oper"`
	OperS       string            `json:"oper_s"`
	TimeOp      *domain.LocalTime `json:"time_op"`
	Naznach     string            `json:"naznach"`
	GruzpolS    string            `json:"gruzpol_s"`
	StanNazn    string            `json:"stan_nazn"`
	CargoS      string            `json:"cargo_s"`
	Ves         *float64          `json:"ves"`
	DateDostav  *domain.LocalTime `json:"date_dostav"`
	Since       domain.LocalTime  `json:"since"` // когда запись донора обновлена
	DaysDonor   int               `json:"days_donor"`
}

// Donors — доноры перегруза (статус 6) из RAM-кэша, свежие первыми.
func (s *MissingService) Donors() []Status6VagonDTO {
	if s.status6 == nil {
		return nil
	}
	now := time.Time(clock.Now())
	rows := s.status6.Donors()
	out := make([]Status6VagonDTO, 0, len(rows))
	for _, r := range rows {
		d := Status6VagonDTO{
			ID: r.ID, Vagon: r.Vagon, Index: r.Index,
			StationOper: r.StationOper, DorogaOper: r.DorogaOper,
			OperS: r.OperS, TimeOp: r.TimeOp,
			Naznach: r.Naznach, GruzpolS: r.GruzpolS, StanNazn: r.StanNazn,
			CargoS: r.CargoS, Ves: r.Ves, DateDostav: r.DateDostav,
			Since: r.UpdatedAt,
		}
		if !r.UpdatedAt.IsZero() {
			d.DaysDonor = int(now.Sub(time.Time(r.UpdatedAt)).Hours() / 24)
		}
		out = append(out, d)
	}
	// Свежие доноры первыми — как в списке пропавших.
	sort.Slice(out, func(i, j int) bool {
		return time.Time(out[i].Since).After(time.Time(out[j].Since))
	})
	return out
}

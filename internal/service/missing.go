package service

import (
	"context"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// MissingService — экран «Пропавшие вагоны»: записи-8 из таблицы кандидатов
// (status9) с последней известной позицией вагона. Только чтение; записи
// появляются в reconcileCandidates (S2-1b) и удаляются возвратом вагона в
// поток либо автоочисткой по TTL (S2-1c).
type MissingService struct {
	status9 *Status9Cache
}

func NewMissingService(status9 *Status9Cache) *MissingService {
	return &MissingService{status9: status9}
}

// MissingVagonDTO — строка экрана: последняя известная позиция + давность пропажи.
type MissingVagonDTO struct {
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
			Vagon: r.Vagon, Index: r.Index,
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

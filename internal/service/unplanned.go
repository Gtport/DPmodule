package service

import (
	"context"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// defaultUnplannedMoveKm — порог «бесплановых в подходе», если настройка
// client_settings.extra.status.unplanned_move_km не задана.
const defaultUnplannedMoveKm = 1000

// trackUnplannedMoves — «бесплановые в подходе» (карточка «Оперативка», этап
// сравнения снимков — решение владельца): гружёный вагон в пути БЕЗ плановых
// данных, назначением на терминал ПЛАНОВОЙ станции (plan_code в реестре),
// ближе порога км, СМЕНИВШИЙ станцию операции против прежнего снимка →
// upsert в unplanned_move (запись живёт до «Скрыть» оператора).
//
// Автоснятие: у вагона появился план, либо он вышел из «гружёного в пути»
// (порожний 6, кандидат 9, прибыл 10, выгружен 12) — сигнал отработан.
// Повторная смена станции после скрытия создаёт запись заново.
func trackUnplannedMoves(
	ctx context.Context,
	kept []domain.Dislocation,
	actual *ActualCache,
	repo port.UnplannedMoveRepository,
	dir *DirectoryCache,
	thresholdKm int,
) (added, cleared int, err error) {
	if thresholdKm <= 0 {
		thresholdKm = defaultUnplannedMoveKm
	}
	now := clock.Now()

	var toUpsert []domain.Dislocation
	var toClear []string
	for i := range kept {
		r := kept[i]
		if r.Vagon == "" {
			continue
		}
		st := 0
		if r.Status != nil {
			st = *r.Status
		}
		inTransit := st == 0 || st == 1 || st == 2 || st == 4 || st == 5

		// Автоснятие: план появился либо вагон больше не «гружёный в пути».
		if r.PlanMsk != nil || !inTransit {
			toClear = append(toClear, r.Vagon)
			continue
		}

		// Сигнал: без плана + плановая станция назначения + ближе порога + смена станции.
		if r.Naznach == "" || r.RasstStanNazn == nil || *r.RasstStanNazn >= thresholdKm {
			continue
		}
		if p, ok := dir.PortByNameS(r.Naznach); !ok || p.PlanCode == "" {
			continue // терминал не плановой станции — сигнал не про него
		}
		prev, ok := actual.FindVagonInActual(r.Vagon)
		if !ok || prev.CodeStationOper == r.CodeStationOper {
			continue // новый вагон или станция не менялась — не «движение»
		}
		r.UpdatedAt = now
		toUpsert = append(toUpsert, r)
	}

	if cleared, err = repo.DeleteByVagons(ctx, toClear); err != nil {
		return 0, 0, err
	}
	if added, err = repo.Upsert(ctx, toUpsert); err != nil {
		return 0, 0, err
	}
	return added, cleared, nil
}

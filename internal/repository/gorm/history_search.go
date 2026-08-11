package gormrepo

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Поисковые выборки экрана «Работа с историческими данными» (перенос gtport
// POST /history/universal). Отличие от оригинала — честная серверная пагинация
// и сортировка: gtport отдавал первые 5000 строк без UI листания, а сортировал
// клиент по загруженному куску. WHERE — билдером (канон GORM-гибрида: динамика
// без CTE/окон), имя колонки сортировки приходит из белого списка сервиса.

// historySearchWhere — общий WHERE фильтра (для страницы, COUNT и курсора).
// Все даты сравниваются с колонками *_d (только дата, полночь) — границы
// включительные без добивки времени.
func (r *HistoryRepository) historySearchWhere(ctx context.Context, f domain.HistorySearchFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&vagonHistoryModel{})
	if f.DateNachDFrom != nil {
		q = q.Where("date_nach_d >= ?", *f.DateNachDFrom)
	}
	if f.DateNachDTo != nil {
		q = q.Where("date_nach_d <= ?", *f.DateNachDTo)
	}
	if f.DatePribDFrom != nil {
		q = q.Where("date_prib_d >= ?", *f.DatePribDFrom)
	}
	if f.DatePribDTo != nil {
		q = q.Where("date_prib_d <= ?", *f.DatePribDTo)
	}
	if f.DateVigrDFrom != nil {
		q = q.Where("date_vigr_d >= ?", *f.DateVigrDFrom)
	}
	if f.DateVigrDTo != nil {
		q = q.Where("date_vigr_d <= ?", *f.DateVigrDTo)
	}
	if len(f.GruzpolS) > 0 {
		q = q.Where("gruzpol_s IN ?", f.GruzpolS)
	}
	if len(f.Naznach) > 0 {
		q = q.Where("naznach IN ?", f.Naznach)
	}
	if len(f.PlaceVigr) > 0 {
		q = q.Where("place_vigr IN ?", f.PlaceVigr)
	}
	if f.NotUnloaded {
		// «Не выгруж.»: и '', и NULL (gtport ловил только пустую строку).
		q = q.Where("COALESCE(place_vigr, '') = ''")
	}
	if len(f.Vagons) > 0 {
		q = q.Where("vagon IN ?", f.Vagons)
	}
	if len(f.Invoices) > 0 {
		// По полю invoice, не invoice_main — как в gtport.
		q = q.Where("invoice IN ?", f.Invoices)
	}
	if len(f.StationNach) > 0 {
		q = q.Where("station_nach IN ?", f.StationNach)
	}
	if f.OnlyOverdue {
		// Просрочка доставки: delay пишется при вехе прибытия (в срок → 0,
		// рейс без прибытия/норматива → NULL) — оба отсекаются сами.
		q = q.Where("delay > 0")
	}
	return q
}

// historyOrder — ORDER BY проверенной колонки: NULLS LAST в обе стороны
// (пустые значения всегда внизу, как в клиентской сортировке gtport) +
// стабилизирующий хвост vagon, id.
func historyOrder(orderCol string, desc bool) string {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	return fmt.Sprintf("%s %s NULLS LAST, vagon, id", orderCol, dir)
}

// SearchRows — страница истории: COUNT по полному фильтру + выборка LIMIT/OFFSET.
func (r *HistoryRepository) SearchRows(ctx context.Context, f domain.HistorySearchFilter, orderCol string, desc bool, limit, offset int) ([]domain.VagonHistory, int, error) {
	var total int64
	if err := r.historySearchWhere(ctx, f).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ms []vagonHistoryModel
	if err := r.historySearchWhere(ctx, f).
		Order(historyOrder(orderCol, desc)).Limit(limit).Offset(offset).
		Find(&ms).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]domain.VagonHistory, len(ms))
	for i, m := range ms {
		rows[i] = toHistoryDomain(m)
	}
	return rows, int(total), nil
}

// IterateSearch — курсорный обход всего отфильтрованного набора (экспорт Excel):
// строки читаются по одной, набор целиком в память не поднимается.
func (r *HistoryRepository) IterateSearch(ctx context.Context, f domain.HistorySearchFilter, orderCol string, desc bool, fn func(domain.VagonHistory) error) error {
	rows, err := r.historySearchWhere(ctx, f).Order(historyOrder(orderCol, desc)).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var m vagonHistoryModel
		if err := r.db.ScanRows(rows, &m); err != nil {
			return err
		}
		if err := fn(toHistoryDomain(m)); err != nil {
			return err
		}
	}
	return rows.Err()
}

// DistinctStationsNach — уникальные непустые станции погрузки по алфавиту.
func (r *HistoryRepository) DistinctStationsNach(ctx context.Context) ([]string, error) {
	var out []string
	err := r.db.WithContext(ctx).Model(&vagonHistoryModel{}).
		Distinct("station_nach").Where("station_nach <> ''").
		Order("station_nach").Pluck("station_nach", &out).Error
	return out, err
}

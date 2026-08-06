package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// Экран «Карта»: группировка ТЕМ ЖЕ ключом, что «Поезда в движении» (trainKey =
// нормализованный индекс + станция операции), координаты — строки снимка Stage 1,
// группы без координат — в отдельный список no_coords (счётчик «не на карте»),
// пометка диспетчера — парой info_1/info_2 с первого помеченного вагона.
func TestMapData(t *testing.T) {
	lt := func(d, h int) *domain.LocalTime {
		return domain.NewLocalTime(time.Date(2026, 8, d, h, 0, 0, 0, time.UTC))
	}
	ip := func(v int) *int { return &v }

	mk := func(id, vagon, index, stationOper string, status int) domain.Dislocation {
		return domain.Dislocation{
			ID: id, Vagon: vagon, Index: index, StationOper: stationOper,
			Naznach: "АЭ", StanNazn: "МЫС АСТАФЬЕВА", Status: ip(status),
		}
	}

	// Группа X (index_pp главнее index): два вагона с координатами, один брошен,
	// пометка на первом по натурному листу, прогноз — у второго.
	x1 := mk("X1", "62000001", "9370-101-9857", "БИКИН", 2)
	x1.IndexPp = "9379-786-9857"
	x1.NppVag = ip(1)
	x1.Latitude, x1.Longitude = "46.800000", "134.250000"
	x1.Info1, x1.Info2 = "контроль подвода", "#fa8c16"
	x1.RasstStanNazn = ip(500)
	x1.Color = "#13c2c2" // marka.color — у обоих вагонов один → цвет группы

	x2 := mk("X2", "62000002", "9370-101-9857", "БИКИН", 5)
	x2.IndexPp = "9379-786-9857"
	x2.NppVag = ip(2)
	x2.Latitude, x2.Longitude = "46.800000", "134.250000"
	x2.Naznach = "УТ-1"
	x2.ProgJd = lt(10, 20)
	x2.TimeOp, x2.OperS = lt(6, 8), "ПРИБ"
	x2.Color = "#13c2c2"

	// Отцепка Y: тот же нормализованный индекс на другой станции, координат нет
	// (станции нет в справочнике) → отдельная группа в no_coords.
	y1 := mk("Y1", "62000003", "9370-101-9857", "ШУШАРЫ", 4)
	y1.IndexPp = "9379-786-9857"

	// Группа Z: «нулевые» координаты Stage 1 = координат нет; второй вагон с
	// ДРУГИМ цветом marka — цвет группы гаснет (правило gtport: только единогласие).
	z1 := mk("Z1", "62000004", "8631-880-9847", "ЕРУНАКОВО", 2)
	z1.Latitude, z1.Longitude = "0.000000", "0.000000"
	z1.Color = "#f5222d"
	z2 := mk("Z2", "62000005", "8631-880-9847", "ЕРУНАКОВО", 2)
	z2.Color = "#52c41a"

	repo := &fakeDislRepo{current: []domain.Dislocation{x1, x2, y1, z1, z2}}
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))

	out := service.NewMapService(actual, nil, nil).Data(context.Background())

	assert.Equal(t, 3, out.Total)
	assert.Equal(t, 5, out.Vagons)
	require.Len(t, out.Groups, 1)
	require.Len(t, out.NoCoords, 2)

	x := out.Groups[0]
	assert.Equal(t, "9379-786-9857|БИКИН", x.Key) // ключ = ключу «Поездов»
	assert.Equal(t, "9379-786-9857", x.Index)     // index_pp главнее index
	assert.Equal(t, 2, x.VagonCount)
	require.NotNil(t, x.Lat)
	require.NotNil(t, x.Lon)
	assert.InDelta(t, 46.8, *x.Lat, 0.0001)
	assert.InDelta(t, 134.25, *x.Lon, 0.0001)
	assert.True(t, x.Broshen)
	assert.False(t, x.Arrived)
	require.NotNil(t, x.Status)
	assert.Equal(t, 2, *x.Status) // большинство; при равенстве — меньший код
	require.NotNil(t, x.ProgJd)   // прогноз — первое непустое → «ходовой»
	assert.Equal(t, "ПРИБ", x.OperS)
	assert.Equal(t, []string{"АЭ", "УТ-1"}, x.NaznachList)
	assert.Equal(t, "АЭ", x.Naznach) // большинство; при равенстве — меньшее имя
	assert.Equal(t, "контроль подвода", x.MarkText)
	assert.Equal(t, "#fa8c16", x.MarkColor)
	assert.Equal(t, "#13c2c2", x.Color) // marka.color единогласен по группе
	require.NotNil(t, x.Rasst)
	assert.Equal(t, 500, *x.Rasst)

	// Без координат: отцепка Y (пустые строки) и Z («нулевые») — с ключами.
	keys := []string{out.NoCoords[0].Key, out.NoCoords[1].Key}
	assert.Contains(t, keys, "9379-786-9857|ШУШАРЫ")
	assert.Contains(t, keys, "8631-880-9847|ЕРУНАКОВО")
	for _, g := range out.NoCoords {
		assert.Nil(t, g.Lat)
		assert.Nil(t, g.Lon)
	}
	// Отцепка Y без прогноза — «отцепка» и в терминах режима карты; у Z цвета
	// marka разошлись по вагонам → группа без цвета клиента (красится статусом).
	for _, g := range out.NoCoords {
		if g.Key == "9379-786-9857|ШУШАРЫ" {
			assert.Nil(t, g.ProgJd)
		}
		if g.Key == "8631-880-9847|ЕРУНАКОВО" {
			assert.Equal(t, "", g.Color)
		}
	}
}

// Вагоны группы — отдельной ручкой по ключу: в основной ответ не входят.
func TestMapWagons(t *testing.T) {
	ip := func(v int) *int { return &v }
	a := domain.Dislocation{
		ID: "A", Vagon: "62000002", Index: "9370-101-9857", StationOper: "БИКИН",
		NppVag: ip(2), Invoice: "ЭЛ1", CargoS: "УГОЛЬ КАМЕННЫЙ", Owner: "ОПЕРАТОР",
		GruzpolS: "АЭ", Naznach: "АЭ", Status: ip(2),
	}
	b := a
	b.ID, b.Vagon, b.NppVag = "B", "62000001", ip(1)
	b.FreightExactName = "УГОЛЬ МАРКИ Д"
	other := a
	other.ID, other.Vagon, other.StationOper = "C", "62000003", "ХАБАРОВСК-2"

	repo := &fakeDislRepo{current: []domain.Dislocation{a, b, other}}
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))

	out := service.NewMapService(actual, nil, nil).Wagons("9370-101-9857|БИКИН")

	require.Len(t, out.Wagons, 2)
	// Натурный лист: B (npp 1) раньше A (npp 2); марка — freight_exact_name,
	// у A пусто → cargo_s.
	assert.Equal(t, "62000001", out.Wagons[0].Vagon)
	assert.Equal(t, "УГОЛЬ МАРКИ Д", out.Wagons[0].Freight)
	assert.Equal(t, "УГОЛЬ КАМЕННЫЙ", out.Wagons[1].Freight)
}

// Пометка: валидация запроса и запись в снимок через MutateSnapshot — все
// вагоны выбранных групп получают info_1/info_2, чужие не трогаются; пустые
// text+color снимают пометку.
func TestMapApplyMark(t *testing.T) {
	ip := func(v int) *int { return &v }
	mk := func(id, vagon, index, station string) domain.Dislocation {
		return domain.Dislocation{ID: id, Vagon: vagon, Index: index, StationOper: station, Status: ip(2)}
	}
	x1 := mk("X1", "62000001", "9370-101-9857", "БИКИН")
	x2 := mk("X2", "62000002", "9370-101-9857", "БИКИН")
	z1 := mk("Z1", "62000003", "8631-880-9847", "ЕРУНАКОВО")

	repo := &fakeDislRepo{current: []domain.Dislocation{x1, x2, z1}}
	proc, _ := newProcessor(t, repo)
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))
	svc := service.NewMapService(actual, nil, proc)

	// Валидация.
	_, err := svc.ApplyMark(context.Background(), service.MapMarkRequest{Keys: nil, Text: "т"})
	assert.ErrorIs(t, err, service.ErrBadMap)
	_, err = svc.ApplyMark(context.Background(), service.MapMarkRequest{
		Keys: []string{"9370-101-9857|БИКИН"}, Color: "красный"})
	assert.ErrorIs(t, err, service.ErrBadMap)

	// Без конвейера пометка не работает (карта собрана без БД).
	_, err = service.NewMapService(actual, nil, nil).
		ApplyMark(context.Background(), service.MapMarkRequest{Keys: []string{"k"}})
	assert.ErrorIs(t, err, service.ErrNotReady)

	// Применение: оба вагона группы X помечены, Z не тронут.
	res, err := svc.ApplyMark(context.Background(), service.MapMarkRequest{
		Keys: []string{"9370-101-9857|БИКИН"}, Text: "приоритет", Color: "#ff4d4f"})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Updated)
	assert.Equal(t, 1, res.Selected)

	byID := map[string]domain.Dislocation{}
	for _, r := range repo.replaced {
		byID[r.ID] = r
	}
	assert.Equal(t, "приоритет", byID["X1"].Info1)
	assert.Equal(t, "#ff4d4f", byID["X1"].Info2)
	assert.Equal(t, "приоритет", byID["X2"].Info1)
	assert.Equal(t, "", byID["Z1"].Info1)

	// Снятие: пустые text и color очищают оба поля. Фейковый репозиторий не
	// отдаёт заменённый снимок при перечитке — собираем процессор заново поверх
	// помеченного снимка (в бою перечитка вернула бы его сама).
	repo.current = repo.replaced
	proc2, _ := newProcessor(t, repo)
	svc = service.NewMapService(actual, nil, proc2)
	res, err = svc.ApplyMark(context.Background(), service.MapMarkRequest{
		Keys: []string{"9370-101-9857|БИКИН"}})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Updated)
	byID = map[string]domain.Dislocation{}
	for _, r := range repo.replaced {
		byID[r.ID] = r
	}
	assert.Equal(t, "", byID["X1"].Info1)
	assert.Equal(t, "", byID["X1"].Info2)
}

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// s9StubDisl — минимальный DislocationRepository для наполнения ActualCache.
type s9StubDisl struct{ items []domain.Dislocation }

func (s s9StubDisl) LoadActual(context.Context) ([]domain.Dislocation, error)  { return s.items, nil }
func (s s9StubDisl) ReplaceActual(context.Context, []domain.Dislocation) error { return nil }

// s9StubRepo — in-memory port.Status9Repository (vagon → статус в таблице).
type s9StubRepo struct {
	vagons    map[string]int              // vagon → статус (8/9)
	updatedAt map[string]domain.LocalTime // vagon → updated_at (для MissingOlderThan)
	inserted  []string
	deleted   []string
	missing8  []string
}

func (r *s9StubRepo) VagonStatuses(context.Context) (map[string]int, error) {
	out := make(map[string]int, len(r.vagons))
	for k, v := range r.vagons {
		out[k] = v
	}
	return out, nil
}
func (r *s9StubRepo) InsertNew(_ context.Context, items []domain.Dislocation) (int, error) {
	n := 0
	for _, it := range items {
		if _, ok := r.vagons[it.Vagon]; ok {
			continue // DoNothing по vagon
		}
		r.vagons[it.Vagon] = derefStatus(it.Status)
		r.inserted = append(r.inserted, it.Vagon)
		n++
	}
	return n, nil
}
func (r *s9StubRepo) UpsertMissing(_ context.Context, items []domain.Dislocation) (int, error) {
	for _, it := range items {
		r.vagons[it.Vagon] = derefStatus(it.Status) // insert или update статуса (→8)
		r.missing8 = append(r.missing8, it.Vagon)
	}
	return len(items), nil
}
func (r *s9StubRepo) DeleteByVagons(_ context.Context, vagons []string) (int, error) {
	n := 0
	for _, v := range vagons {
		if _, ok := r.vagons[v]; ok {
			delete(r.vagons, v)
			r.deleted = append(r.deleted, v)
			n++
		}
	}
	return n, nil
}

func (r *s9StubRepo) MissingOlderThan(_ context.Context, cutoff domain.LocalTime) ([]string, error) {
	var out []string
	for v, s := range r.vagons {
		if s != 8 {
			continue
		}
		if ua, ok := r.updatedAt[v]; ok && ua.Time().Before(cutoff.Time()) {
			out = append(out, v)
		}
	}
	return out, nil
}

func derefStatus(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// s6StubRepo — in-memory port.Status6Repository.
type s6StubRepo struct {
	stored   map[string]domain.Dislocation
	upserted []domain.Dislocation
}

func newS6Stub() *s6StubRepo { return &s6StubRepo{stored: map[string]domain.Dislocation{}} }

func (r *s6StubRepo) LoadAll(context.Context) ([]domain.Dislocation, error) {
	out := make([]domain.Dislocation, 0, len(r.stored))
	for _, d := range r.stored {
		out = append(out, d)
	}
	return out, nil
}
func (r *s6StubRepo) Upsert(_ context.Context, items []domain.Dislocation) (int, error) {
	for _, it := range items {
		r.stored[it.Vagon] = it
	}
	r.upserted = append(r.upserted, items...)
	return len(items), nil
}
func (r *s6StubRepo) DeleteByVagons(_ context.Context, vagons []string) (int, error) {
	n := 0
	for _, v := range vagons {
		if _, ok := r.stored[v]; ok {
			delete(r.stored, v)
			n++
		}
	}
	return n, nil
}

// s9cache/s6cache — кэши поверх stub-репозиториев (прогреты).
func s9cache(t *testing.T, repo *s9StubRepo) *Status9Cache {
	t.Helper()
	c := NewStatus9Cache(repo)
	require.NoError(t, c.Load(context.Background()))
	return c
}
func s6cache(t *testing.T, repo *s6StubRepo) *Status6Cache {
	t.Helper()
	c := NewStatus6Cache(repo)
	require.NoError(t, c.Load(context.Background()))
	return c
}

// Переход на статус 6 (был ≠6 и <10) → донор в status6. gruzpol_s/naznach обнулены
// ТОЛЬКО в снимке; в записи-доноре они реальные (нужны для передачи приёмнику, §3.17).
// Новый сразу 6, «уже был 6» и переходы из 10/12 (груз доехал) — не фиксируются.
func TestApplyStatus6Transition(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "T1", Status: ip(2)},  // ехал гружёным
		{Vagon: "T2", Status: ip(6)},  // уже был порожним
		{Vagon: "T5", Status: ip(10)}, // прибыл у нас
		{Vagon: "T6", Status: ip(12)}, // выгружен у нас
	}})
	require.NoError(t, actual.Load(ctx))
	repo := newS6Stub()

	kept := []domain.Dislocation{
		{Vagon: "T1", Status: ip(6), GruzpolS: "ГУТ-2", Naznach: "ГУТ-2", CargoS: "УГОЛЬ"}, // 2→6: переход, донор
		{Vagon: "T2", Status: ip(6), GruzpolS: "УТ-1"},                                     // 6→6: не переход
		{Vagon: "T3", Status: ip(6), GruzpolS: "АЭ"},                                       // новый сразу 6: не фиксируем
		{Vagon: "T4", Status: ip(2), GruzpolS: "ГУТ-2"},                                    // не 6
		{Vagon: "T5", Status: ip(6), GruzpolS: "АЭ", Naznach: "АЭ"},                        // 10→6: груз доехал, не донор
		{Vagon: "T6", Status: ip(6), GruzpolS: "ГУТ-2", Naznach: "АЭ"},                     // 12→6: груз выгружен, не донор
	}

	n, err := applyStatus6Transition(ctx, kept, actual, s6cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 1, n) // только T1
	require.Len(t, repo.upserted, 1)
	d := repo.upserted[0]
	assert.Equal(t, "T1", d.Vagon)
	assert.Equal(t, "ГУТ-2", d.GruzpolS) // в доноре реальные — для передачи приёмнику
	assert.Equal(t, "ГУТ-2", d.Naznach)
	assert.Equal(t, "УГОЛЬ", d.CargoS) // груз сохранён для передачи
	// в снимке T1 тоже обнулён
	assert.Equal(t, "0", kept[0].GruzpolS)
	assert.Equal(t, "0", kept[0].Naznach)
	// T2/T3/T4 в снимке не тронуты
	assert.Equal(t, "УТ-1", kept[1].GruzpolS)
	assert.Equal(t, "АЭ", kept[2].GruzpolS)
	// T5/T6 не доноры и не затёрты в снимке
	assert.Equal(t, "АЭ", kept[4].GruzpolS)
	assert.Equal(t, "ГУТ-2", kept[5].GruzpolS)
}

func ip(v int) *int         { return &v }
func fp(v float64) *float64 { return &v }
func ld(y, mo, d int) *domain.LocalTime {
	v := domain.LocalTime(time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC))
	return &v
}

// Донорство перегруза (S2-3c): новый вагон без груза, совпавший с донором по станции
// операции + вес ±0.1 + срок доставки, наследует груз/назначение/отправление донора,
// оставаясь собой физически; номер донора → peregruz; донор удаляется. §3.17.
func TestApplyStatus6Donorship(t *testing.T) {
	ctx := context.Background()
	repo := newS6Stub()
	repo.stored["D1"] = domain.Dislocation{
		Vagon: "D1", CodeStationOper: "100", Ves: fp(68.0), DateDostav: ld(2026, 7, 10),
		Gruzotpr: "ОТПР", GruzotprOkpo: "555", CodeCargo: "16021", CargoS: "УГОЛЬ",
		CargoGroup: "УГ", Client: "КЛ", GruzpolS: "ГУТ-2", Naznach: "ГУТ-2",
		StanNazn: "МЫС АСТАФЬЕВА", CodeStationNach: "200", StationNach: "СТ-ДОНОРА", DorogaNach: "ЗСЖД",
	}
	cache := s6cache(t, repo)

	kept := []domain.Dislocation{
		// приёмник: новый, без груза; станция погрузки(отправления)=100 == станция операции донора;
		// вес 68.05 (в пределах ±0.1), срок совпал.
		{Vagon: "R1", CodeStationNach: "100", Ves: fp(68.05), DateDostav: ld(2026, 7, 10),
			CodeStationOper: "777", StationOper: "ГДЕ-ТО", Status: ip(2), Index: "IDX-R1"},
		{Vagon: "R2", CodeStationNach: "100", Ves: fp(90.0), DateDostav: ld(2026, 7, 10)},                   // вес далеко
		{Vagon: "R3", CodeStationNach: "100", Ves: fp(68.0), DateDostav: ld(2026, 7, 11)},                   // срок иной
		{Vagon: "R4", Gruzotpr: "СВОЙ", CodeStationNach: "100", Ves: fp(68.0), DateDostav: ld(2026, 7, 10)}, // груз есть
	}

	n, err := applyStatus6Donorship(ctx, kept, cache)
	require.NoError(t, err)
	assert.Equal(t, 1, n) // только R1

	// R1 наследовал груз/назначение/отправление донора
	assert.Equal(t, "ОТПР", kept[0].Gruzotpr)
	assert.Equal(t, "УГОЛЬ", kept[0].CargoS)
	assert.Equal(t, "ГУТ-2", kept[0].GruzpolS)
	assert.Equal(t, "МЫС АСТАФЬЕВА", kept[0].StanNazn)
	assert.Equal(t, "200", kept[0].CodeStationNach) // станция отправления — донора
	assert.Equal(t, "СТ-ДОНОРА", kept[0].StationNach)
	assert.Equal(t, "D1", kept[0].Peregruz) // номер донора
	// физика приёмника не тронута
	assert.Equal(t, "777", kept[0].CodeStationOper)
	assert.Equal(t, "IDX-R1", kept[0].Index)
	assert.Equal(t, 2, *kept[0].Status)

	// прочие не тронуты
	assert.Empty(t, kept[1].Gruzotpr)
	assert.Empty(t, kept[2].Gruzotpr)
	assert.Equal(t, "СВОЙ", kept[3].Gruzotpr)
	assert.Empty(t, kept[1].Peregruz)

	// донор использован и удалён
	assert.Equal(t, 0, cache.Count())
}

// Живой статус 9: первое появление / повторный / возврат в поток.
func TestReconcile_Status9Live(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "V1", Status: ip(2)},
		{Vagon: "V2", Status: ip(9)},
	}})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"V3": 9}} // V3 — кандидат с прошлого цикла

	batch := []domain.Dislocation{
		{Vagon: "V1", Status: ip(9)}, // был 2 → 9: первое появление → insert
		{Vagon: "V2", Status: ip(9)}, // был 9 → 9: не первое → skip
		{Vagon: "V3", Status: ip(2)}, // кандидат вернулся (≠9) → delete
		{Vagon: "V4", Status: ip(2)}, // обычный, не в таблице → ничего
		{Vagon: "V5", Status: ip(9)}, // новый сразу 9 → insert
	}

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 2, st.Inserted) // V1, V5
	assert.Equal(t, 1, st.Removed)  // V3
	assert.Contains(t, repo.vagons, "V1")
	assert.Contains(t, repo.vagons, "V5")
	assert.NotContains(t, repo.vagons, "V3")
	assert.NotContains(t, repo.vagons, "V2")
}

// Пропавшие → статус 8; штатно выбывшие (6 — порожний в пути, 10 — прибыл,
// 12 — выгружен) не фиксируются; статус-9 при пропаже → 8.
func TestReconcile_Missing8(t *testing.T) {
	ctx := context.Background()
	// актуальная: M1 ехал (2), M2 порожний в пути (6), M3 живой кандидат (9),
	// M4 прибыл (10), M5 выгружен (12), P — останется в батче
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "M1", Status: ip(2)},
		{Vagon: "M2", Status: ip(6)},
		{Vagon: "M3", Status: ip(9)},
		{Vagon: "M4", Status: ip(10)},
		{Vagon: "M5", Status: ip(12)},
		{Vagon: "P", Status: ip(2)},
	}})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"M3": 9}} // M3 уже в таблице как живой 9

	// В батче только P (остальные пропали).
	batch := []domain.Dislocation{{Vagon: "P", Status: ip(2)}}

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 2, st.Missing8) // M1 (новый 8) + M3 (перевод 9→8); M2/M4/M5 выбыли
	assert.Equal(t, 8, repo.vagons["M1"])
	assert.Equal(t, 8, repo.vagons["M3"]) // 9 → 8 при пропаже
	assert.NotContains(t, repo.vagons, "M2")
	assert.NotContains(t, repo.vagons, "M4") // прибыл и уехал — штатно
	assert.NotContains(t, repo.vagons, "M5") // выгружен и уехал — штатно
	assert.ElementsMatch(t, []string{"M1", "M3"}, repo.missing8)
}

// Автоочистка: пропавшие (8) старше cutoff удаляются из БД и RAM; свежие 8 и
// живые кандидаты 9 не затрагиваются.
func TestPurgeMissingOlderThan(t *testing.T) {
	ctx := context.Background()
	lt := func(d int) domain.LocalTime {
		return domain.LocalTime(time.Date(2026, 7, d, 12, 0, 0, 0, time.UTC))
	}
	repo := &s9StubRepo{
		vagons:    map[string]int{"OLD1": 8, "OLD2": 8, "FRESH": 8, "LIVE": 9},
		updatedAt: map[string]domain.LocalTime{"OLD1": lt(1), "OLD2": lt(5), "FRESH": lt(15), "LIVE": lt(1)},
	}
	cache := s9cache(t, repo)

	n, err := cache.PurgeMissingOlderThan(ctx, lt(10)) // cutoff 10.07
	require.NoError(t, err)

	assert.Equal(t, 2, n) // OLD1, OLD2
	assert.ElementsMatch(t, []string{"OLD1", "OLD2"}, repo.deleted)
	assert.NotContains(t, repo.vagons, "OLD1")
	assert.Contains(t, repo.vagons, "FRESH")        // свежий 8 остался
	assert.Contains(t, repo.vagons, "LIVE")         // живой 9 не тронут, хоть и старый
	assert.NotContains(t, cache.Statuses(), "OLD1") // RAM тоже почищен
	assert.Contains(t, cache.Statuses(), "FRESH")
}

// Вагон был защищённым 8, вернулся живым кандидатом 9 → снять 8, записать 9.
func TestReconcile_Return8AsLive9(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{}) // вагона нет в снимке (пропадал)
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"W": 8}} // W лежит как пропавший 8

	batch := []domain.Dislocation{{Vagon: "W", Status: ip(9)}} // вернулся, сразу на ст.назн

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 1, st.Removed)  // старый 8 снят
	assert.Equal(t, 1, st.Inserted) // записан как живой 9
	assert.Equal(t, 9, repo.vagons["W"])
	assert.Contains(t, repo.deleted, "W")
}

// Вагон был 8, вернулся в поток НЕ как кандидат (статус 2) → снять из таблицы.
func TestReconcile_Return8ToStream(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"W": 8}}

	batch := []domain.Dislocation{{Vagon: "W", Status: ip(2)}}

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 1, st.Removed)
	assert.NotContains(t, repo.vagons, "W")
}

// Пустой вагон в батче пропускается.
func TestReconcile_SkipEmptyVagon(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{}}

	st, err := reconcileCandidates(ctx, []domain.Dislocation{{Vagon: "", Status: ip(9)}}, actual, s9cache(t, repo))
	require.NoError(t, err)
	assert.Equal(t, 0, st.Inserted)
}

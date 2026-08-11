package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/domain"
)

// ───────────────────────── AudiencesFor ─────────────────────────

// Аудитории по обеим ролевым схемам (в духе TestAllowsSchemasNotMixed):
// client-роли и realm-роли дают одинаковые аудитории, чужая схема — нет.
func TestAudiencesFor(t *testing.T) {
	cases := []struct {
		name string
		c    *auth.Claims
		want []string
	}{
		{"nil claims (Keycloak выключен)", nil, []string{domain.AudienceAll}},
		{"client-роль client", &auth.Claims{ClientRoles: []auth.Role{auth.ClientClient}},
			[]string{domain.AudienceAll}},
		{"client-роль operator", &auth.Claims{ClientRoles: []auth.Role{auth.ClientOperator}},
			[]string{domain.AudienceAll, domain.AudienceOper}},
		{"client-роль senior-operator", &auth.Claims{ClientRoles: []auth.Role{auth.ClientSeniorOperator}},
			[]string{domain.AudienceAll, domain.AudienceOper, domain.AudienceDicts}},
		{"client-роль admin", &auth.Claims{ClientRoles: []auth.Role{auth.ClientAdmin}},
			[]string{domain.AudienceAll, domain.AudienceOper, domain.AudienceDicts, domain.AudienceAdmin}},
		{"realm-роль operator_dpport", &auth.Claims{Roles: []auth.Role{auth.RoleOperator}},
			[]string{domain.AudienceAll, domain.AudienceOper}},
		{"realm-роль admin_dpport", &auth.Claims{Roles: []auth.Role{auth.RoleAdmin}},
			[]string{domain.AudienceAll, domain.AudienceOper, domain.AudienceDicts, domain.AudienceAdmin}},
		// Чужая полка: наши client-имена в realm-полке аудиторий не дают.
		{"client-имя в realm-полке", &auth.Claims{Roles: []auth.Role{auth.ClientOperator}},
			[]string{domain.AudienceAll}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, AudiencesFor(tc.c))
		})
	}
}

// ───────────────────────── события applyBros ─────────────────────────

// Новый бросок рождает событие new с реквизитами группы.
func TestApplyBros_EventNew(t *testing.T) {
	const key = "IDX-A|770001|2026-07-24 08:00:00"
	kept := []domain.Dislocation{
		brosVag("100", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
		brosVag("101", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
	}

	st, err := applyBros(context.Background(), kept, emptyActual(), &fakeBrosRepo{})
	require.NoError(t, err)

	require.Len(t, st.Events, 1)
	e := st.Events[0]
	assert.Equal(t, brosEventNew, e.Kind)
	assert.Equal(t, key, e.ID)
	assert.Equal(t, "IDX-A", e.Index)
	assert.Equal(t, "УЛАК", e.Station)
	assert.Equal(t, "ДВ", e.Doroga)
	assert.Equal(t, 2, e.VagonCount)
	assert.Equal(t, "СМЫЧКА УГОЛЬ (2)", e.Sostav)
}

// Переоткрытие поднятого броска (повторное бросание) — тоже событие new.
func TestApplyBros_EventReopen(t *testing.T) {
	const key = "IDX-A|770001|2026-07-24 08:00:00"
	repo := &fakeBrosRepo{stored: []domain.Bros{
		{ID: key, Index1: "IDX-A", StatusBr: false, VagonCount: 1},
	}}
	kept := []domain.Dislocation{
		brosVag("100", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
	}

	st, err := applyBros(context.Background(), kept, emptyActual(), repo)
	require.NoError(t, err)

	require.Len(t, st.Events, 1)
	assert.Equal(t, brosEventNew, st.Events[0].Kind)
	assert.Equal(t, key, st.Events[0].ID)
}

// Подъём рождает событие stop: реквизиты из записи bros, индекс и дата подъёма —
// из нового состояния вагона, duration_days считается по датам.
func TestApplyBros_EventStop(t *testing.T) {
	const key = "IDX-A|770001|2026-07-24 08:00:00"
	dateBr := domain.NewLocalTime(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC))
	repo := &fakeBrosRepo{active: []domain.Bros{{
		ID: key, Index0: "IDX-A", Index1: "IDX-A", StationBr: "УЛАК", DorogaBr: "ДВ",
		StatusBr: true, VagonCount: 1, DateBr: dateBr,
	}}}
	moved := brosVag("100", "", "IDX-A2", "ТАЙШЕТ", "СМЫЧКА", "УГОЛЬ", 2)
	moved.DateKon = domain.NewLocalTime(time.Date(2026, 7, 27, 9, 30, 0, 0, time.UTC))
	actual := &ActualCache{byVagon: map[string]domain.Dislocation{
		"100": brosVag("100", key, "IDX-A", "УЛАК", "СМЫЧКА", "УГОЛЬ", 5),
	}}

	st, err := applyBros(context.Background(), []domain.Dislocation{moved}, actual, repo)
	require.NoError(t, err)

	require.Len(t, st.Events, 1)
	e := st.Events[0]
	assert.Equal(t, brosEventStop, e.Kind)
	assert.Equal(t, key, e.ID)
	assert.Equal(t, "IDX-A2", e.Index, "индекс — из нового состояния вагона")
	assert.Equal(t, "УЛАК", e.Station, "станция — где бросили, из записи bros")
	require.NotNil(t, e.DatePod)
	assert.Equal(t, 3, brosDurationDays(e.DateBr, e.DatePod))
}

// ───────────────────────── collectArrivalGroups ─────────────────────────

func arrVag(vagon, index, stan string, status int, prib *domain.LocalTime) domain.Dislocation {
	s := status
	return domain.Dislocation{Vagon: vagon, Index: index, StanNazn: stan, Status: &s, DatePrib: prib}
}

func TestCollectArrivalGroups(t *testing.T) {
	prib := domain.NewLocalTime(time.Date(2026, 8, 10, 9, 15, 0, 0, time.UTC))
	prib2 := domain.NewLocalTime(time.Date(2026, 8, 10, 14, 40, 0, 0, time.UTC))
	st9 := 9
	st10 := 10
	actual := &ActualCache{byVagon: map[string]domain.Dislocation{
		"100": {Vagon: "100", Status: &st9},  // был в подходе → переход
		"101": {Vagon: "101", Status: &st9},  // был в подходе → переход
		"200": {Vagon: "200", Status: &st10}, // sticky-10 → не сигналит
	}}
	all := []domain.Dislocation{
		arrVag("100", "1234-567-8901", "МЫС АСТАФЬЕВА", 10, prib),
		arrVag("101", "1234-567-8901", "МЫС АСТАФЬЕВА", 10, prib),
		arrVag("102", "1234-567-8901", "МЫС АСТАФЬЕВА", 10, prib), // не было в снимке → считается
		arrVag("200", "1234-567-8901", "МЫС АСТАФЬЕВА", 10, prib), // sticky-10 → пропуск
		arrVag("300", "Б/И", "МЫС АСТАФЬЕВА", 10, prib),           // Б/И → пропуск
		arrVag("301", "", "МЫС АСТАФЬЕВА", 10, prib),              // пустой индекс → пропуск
		arrVag("400", "9999-888-7777", "НАХОДКА", 10, prib2),      // другой поезд
		arrVag("500", "1234-567-8901", "МЫС АСТАФЬЕВА", 9, nil),   // ещё в подходе → пропуск
	}

	groups := collectArrivalGroups(all, actual, 18)

	require.Len(t, groups, 2)
	assert.Equal(t, "1234-567-8901", groups[0].Index)
	assert.Equal(t, 3, groups[0].VagonCount, "два перехода 9→10 + один новый вагон")
	assert.Equal(t, "9999-888-7777", groups[1].Index)
	assert.Equal(t, 1, groups[1].VagonCount)
}

// Момент прибытия приводится к ЖД-шкале: час ≥ отсечки (18) даёт +1 сутки —
// и в тексте, и в dedup-ключе группы.
func TestCollectArrivalGroups_JdShift(t *testing.T) {
	prib := domain.NewLocalTime(time.Date(2026, 8, 10, 19, 22, 0, 0, time.UTC))
	all := []domain.Dislocation{arrVag("100", "1234-567-8901", "МЫС", 10, prib)}

	groups := collectArrivalGroups(all, emptyActual(), 18)

	require.Len(t, groups, 1)
	require.NotNil(t, groups[0].Prib)
	assert.Equal(t, "2026-08-11T19:22", notifMinute(groups[0].Prib))
}

// Один поезд двумя порциями с РАЗНЫМ штампом прибытия — две группы (дедуп по
// index+минута сработает только при едином штампе; см. комментарий в плане).
func TestCollectArrivalGroups_DifferentPrib(t *testing.T) {
	pribA := domain.NewLocalTime(time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	pribB := domain.NewLocalTime(time.Date(2026, 8, 10, 9, 1, 0, 0, time.UTC))
	all := []domain.Dislocation{
		arrVag("100", "1234-567-8901", "МЫС", 10, pribA),
		arrVag("101", "1234-567-8901", "МЫС", 10, pribB),
	}

	groups := collectArrivalGroups(all, emptyActual(), 18)
	assert.Len(t, groups, 2)
}

// ───────────────────────── collectMarkaMissing ─────────────────────────

func TestCollectMarkaMissing(t *testing.T) {
	dir := markaDir(t, markaFixture, cargoFixture, nil)
	ves := 70.0
	vesZero := 0.0
	mk := func(vagon, okpo, station, group, gruzotpr string) domain.Dislocation {
		return domain.Dislocation{
			Vagon: vagon, GruzotprOkpo: okpo, CodeStationNach: station,
			CargoGroup: group, Gruzotpr: gruzotpr, Ves: &ves,
		}
	}
	porozh := mk("500", "77", "888", "", "")
	porozh.PorozhPriznak = "1"
	porozh.Ves = &vesZero
	all := []domain.Dislocation{
		mk("100", "99", "777", "УГОЛЬ", ""),        // дыра
		mk("101", "99", "777", "УГОЛЬ", ""),        // та же комбинация
		mk("200", "99", "778", "УГОЛЬ", ""),        // другая станция → другая комбинация
		mk("300", "1", "2", "УГОЛЬ", "АТРИБУЦИЯ"),  // сматчен → пропуск
		porozh,                                     // порожний под погрузку → пропуск
		{Vagon: "", GruzotprOkpo: "99", Ves: &ves}, // без номера → пропуск
	}

	combos := collectMarkaMissing(all, dir)

	require.Len(t, combos, 2)
	assert.Equal(t, "99", combos[0].Okpo)
	assert.Equal(t, "777", combos[0].Station)
	assert.Equal(t, "УГОЛЬ", combos[0].CargoGroup)
	assert.Equal(t, 2, combos[0].VagonCount)
	assert.Equal(t, "778", combos[1].Station)
}

// ───────────────────────── системные сбои ─────────────────────────

// fakeNotifRepo — in-memory NotificationRepository, копит вставки.
type fakeNotifRepo struct {
	inserts []domain.Notification
	keys    map[string]bool
}

func (f *fakeNotifRepo) Insert(_ context.Context, n domain.Notification) (bool, error) {
	if f.keys == nil {
		f.keys = map[string]bool{}
	}
	if n.DedupKey != "" && f.keys[n.DedupKey] {
		return false, nil
	}
	f.keys[n.DedupKey] = true
	f.inserts = append(f.inserts, n)
	return true, nil
}
func (f *fakeNotifRepo) ListForUser(context.Context, string, []string, bool, int) ([]domain.UserNotification, error) {
	return nil, nil
}
func (f *fakeNotifRepo) UnreadCount(context.Context, string, []string) (int, error) { return 0, nil }
func (f *fakeNotifRepo) MarkRead(context.Context, string, []string, int64) error    { return nil }
func (f *fakeNotifRepo) MarkAllRead(context.Context, string, []string) (int, error) { return 0, nil }
func (f *fakeNotifRepo) PurgeOlderThan(context.Context, domain.LocalTime) (int, error) {
	return 0, nil
}

// Отклонённый забор АСУ сигналит админам; штатный not_newer — нет; повтор того
// же гарда в тот же день гасится дедупом.
func TestNotifyASURejected(t *testing.T) {
	repo := &fakeNotifRepo{}
	svc := NewNotificationService(repo, 72*time.Hour, 0, zap.NewNop())

	svc.NotifyASURejected(context.Background(), "fetch", "connection refused")
	svc.NotifyASURejected(context.Background(), "fetch", "connection refused (повтор)")
	svc.NotifyASURejected(context.Background(), "not_newer", "данные не обновились")

	require.Len(t, repo.inserts, 1)
	n := repo.inserts[0]
	assert.Equal(t, domain.NotifyTypeError, n.Type)
	assert.Equal(t, domain.AudienceAdmin, n.Audience)
	assert.Contains(t, n.Message, "fetch")
}

// Сторож устаревания: моложе порога — тишина; старше — error админам с
// дедупом по штампу застрявшего обновления (один сигнал на эпизод).
func TestDislStaleNotification(t *testing.T) {
	last := domain.NewLocalTime(time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))

	_, stale := dislStaleNotification(last,
		domain.LocalTime(time.Date(2026, 8, 10, 10, 30, 0, 0, time.UTC)), 2*time.Hour)
	assert.False(t, stale, "1.5 ч < порога 2 ч — тишина")

	n, stale := dislStaleNotification(last,
		domain.LocalTime(time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)), 2*time.Hour)
	require.True(t, stale)
	assert.Equal(t, domain.NotifyTypeError, n.Type)
	assert.Equal(t, domain.AudienceAdmin, n.Audience)
	assert.Contains(t, n.Message, "5 ч")
	assert.Equal(t, "disl_stale_2026-08-10T09:00", n.DedupKey)

	// Час спустя снимок всё ещё стоит — тот же dedup-ключ, второго сигнала нет.
	n2, _ := dislStaleNotification(last,
		domain.LocalTime(time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)), 2*time.Hour)
	assert.Equal(t, n.DedupKey, n2.DedupKey)
}

// ───────────────────────── помощники форматирования ─────────────────────────

func TestNotifHelpers(t *testing.T) {
	assert.Equal(t, "дата не указана", notifDate(nil))
	d := domain.NewLocalTime(time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	assert.Equal(t, "10.08.26", notifDate(d))
	assert.Equal(t, "", notifMinute(nil))
	assert.Equal(t, 0, brosDurationDays(nil, d))
	assert.Equal(t, "—", notifOrDash(""))
}

package gormrepo_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/internal/domain"
	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
)

// Integration-тест против реальной БД. Запускается только если задан
// DPMODULE_TEST_PG_DSN (иначе Skip). Требует применённой миграции
// 000061_notifications. Покрывает: дедуп ON CONFLICT, «непрочитанные =
// видимые минус прочитанные», видимость по аудитории и адресно,
// идемпотентность MarkRead/MarkAllRead, PurgeOlderThan с каскадом и
// освобождением dedup_key. Свои строки убирает за собой.
func TestNotificationRepository_Lifecycle(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2})
	require.NoError(t, err)

	const (
		userOper  = "test-notif-oper"  // тестовые имена — в живых данных не встречаются
		userAdmin = "test-notif-admin" //
		pfx       = "test-notif-"     // префикс dedup_key тестовых строк
	)
	cleanup := func() {
		db.Exec("DELETE FROM notifications WHERE dedup_key LIKE ?", pfx+"%")
		db.Exec("DELETE FROM notification_read WHERE username IN ?", []string{userOper, userAdmin})
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := gormrepo.NewNotificationRepository(db)
	ctx := context.Background()

	// Вставка трёх уведомлений: два операторских (одно старое — для purge)
	// и одно админское.
	old := domain.LocalTime(time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC))
	ins := func(n domain.Notification) bool {
		ok, err := repo.Insert(ctx, n)
		require.NoError(t, err)
		return ok
	}
	require.True(t, ins(domain.Notification{
		Type: domain.NotifyTypeWarning, Title: "Брошен поезд!", Message: "тест",
		Audience: domain.AudienceOper, DedupKey: pfx + "bros-1",
		ActionComponent: domain.NotifyActionBros,
	}))
	require.True(t, ins(domain.Notification{
		Type: domain.NotifyTypeInfo, Title: "Старое", Audience: domain.AudienceOper,
		DedupKey: pfx + "old-1", CreatedAt: old,
	}))
	require.True(t, ins(domain.Notification{
		Type: domain.NotifyTypeError, Title: "Сбой АСУ", Audience: domain.AudienceAdmin,
		DedupKey: pfx + "asu-1",
	}))

	// Дедуп: повторная вставка того же ключа гасится (false, не ошибка).
	assert.False(t, ins(domain.Notification{
		Type: domain.NotifyTypeWarning, Title: "Брошен поезд! (дубль)",
		Audience: domain.AudienceOper, DedupKey: pfx + "bros-1",
	}))

	// Видимость: оператор видит 2 операторских, админского не видит.
	operAud := []string{domain.AudienceOper}
	adminAud := []string{domain.AudienceOper, domain.AudienceDicts, domain.AudienceAdmin}
	listOper, err := repo.ListForUser(ctx, userOper, operAud, false, 100)
	require.NoError(t, err)
	countPfx := func(list []domain.UserNotification) (n int) {
		for _, x := range list {
			if len(x.DedupKey) >= len(pfx) && x.DedupKey[:len(pfx)] == pfx {
				n++
			}
		}
		return
	}
	assert.Equal(t, 2, countPfx(listOper))

	// Админ с полным набором аудиторий видит все три.
	listAdmin, err := repo.ListForUser(ctx, userAdmin, adminAud, false, 100)
	require.NoError(t, err)
	assert.Equal(t, 3, countPfx(listAdmin))

	// Непрочитанных у оператора ≥ 2; MarkRead одного — стало на 1 меньше.
	cnt0, err := repo.UnreadCount(ctx, userOper, operAud)
	require.NoError(t, err)
	var brosID int64
	for _, x := range listOper {
		if x.DedupKey == pfx+"bros-1" {
			brosID = x.ID
		}
	}
	require.NotZero(t, brosID)
	require.NoError(t, repo.MarkRead(ctx, userOper, operAud, brosID))
	require.NoError(t, repo.MarkRead(ctx, userOper, operAud, brosID)) // идемпотентно
	cnt1, err := repo.UnreadCount(ctx, userOper, operAud)
	require.NoError(t, err)
	assert.Equal(t, cnt0-1, cnt1)

	// MarkRead чужого (админского) уведомления оператором — молча игнорируется.
	var asuID int64
	for _, x := range listAdmin {
		if x.DedupKey == pfx+"asu-1" {
			asuID = x.ID
		}
	}
	require.NotZero(t, asuID)
	require.NoError(t, repo.MarkRead(ctx, userOper, operAud, asuID))
	var stray int
	db.Raw("SELECT count(*) FROM notification_read WHERE username = ? AND notification_id = ?",
		userOper, asuID).Scan(&stray)
	assert.Zero(t, stray, "прочтение невидимого уведомления не должно записываться")

	// unreadOnly не отдаёт прочитанное.
	unread, err := repo.ListForUser(ctx, userOper, operAud, true, 100)
	require.NoError(t, err)
	for _, x := range unread {
		assert.NotEqual(t, pfx+"bros-1", x.DedupKey)
	}

	// MarkAllRead добивает остальные видимые; повтор — ноль новых.
	n1, err := repo.MarkAllRead(ctx, userOper, operAud)
	require.NoError(t, err)
	assert.Positive(t, n1)
	n2, err := repo.MarkAllRead(ctx, userOper, operAud)
	require.NoError(t, err)
	assert.Zero(t, n2)

	// Purge: старое уведомление удаляется, каскад чистит прочтения,
	// dedup_key освобождается — вставка с тем же ключом снова проходит.
	cutoff := domain.LocalTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	purged, err := repo.PurgeOlderThan(ctx, cutoff)
	require.NoError(t, err)
	assert.Positive(t, purged)
	assert.True(t, ins(domain.Notification{
		Type: domain.NotifyTypeInfo, Title: "Старое (снова)",
		Audience: domain.AudienceOper, DedupKey: pfx + "old-1",
	}), "после purge dedup_key должен освободиться")
}

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/domain"
)

// adminStubRepo — заглушка реестра list_tables: полный боевой набор таблиц.
type adminStubRepo struct{ deleted []string }

func (r *adminStubRepo) ListTables(context.Context) ([]domain.AdminTable, error) {
	names := []string{"marka", "stations", "naznach_station", "sf", "cargo", "porozh_cargo", "port_cargo_line"}
	out := make([]domain.AdminTable, len(names))
	for i, n := range names {
		out[i] = domain.AdminTable{Name: n, PK: "id"}
	}
	return out, nil
}
func (r *adminStubRepo) Columns(context.Context, string) ([]domain.AdminColumn, error) {
	return []domain.AdminColumn{{Name: "id"}, {Name: "value"}}, nil
}
func (r *adminStubRepo) Rows(context.Context, string, string) ([]domain.AdminRow, error) {
	return nil, nil
}
func (r *adminStubRepo) Insert(context.Context, string, []domain.AdminColumn, domain.AdminRow) error {
	return nil
}
func (r *adminStubRepo) Update(context.Context, string, string, string, []domain.AdminColumn, domain.AdminRow) error {
	return nil
}
func (r *adminStubRepo) Delete(_ context.Context, table, _, _ string) error {
	r.deleted = append(r.deleted, table)
	return nil
}

func ctxWithClientRoles(roles ...auth.Role) context.Context {
	return auth.WithClaims(context.Background(), &auth.Claims{ClientRoles: roles})
}

// Решение владельца 06.08.2026: senior-operator видит и правит в админ-редакторе
// ТОЛЬКО свои словари (marka, stations, naznach_station, sf), admin — весь реестр.
func TestAdminTables_SeniorSeesOnlyDicts(t *testing.T) {
	svc := NewAdminTables(&adminStubRepo{})

	t.Run("admin — весь реестр", func(t *testing.T) {
		tables, err := svc.Tables(ctxWithClientRoles(auth.ClientAdmin))
		require.NoError(t, err)
		assert.Len(t, tables, 7)
	})

	t.Run("senior — только словари", func(t *testing.T) {
		tables, err := svc.Tables(ctxWithClientRoles(auth.ClientSeniorOperator))
		require.NoError(t, err)
		names := map[string]bool{}
		for _, tb := range tables {
			names[tb.Name] = true
		}
		assert.Equal(t, map[string]bool{"marka": true, "stations": true, "naznach_station": true, "sf": true}, names)
	})

	t.Run("realm admin_dpport (старая схема) — весь реестр", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Roles: []auth.Role{auth.RoleAdmin}})
		tables, err := svc.Tables(ctx)
		require.NoError(t, err)
		assert.Len(t, tables, 7)
	})

	t.Run("auth выключен (нет claims) — весь реестр, как раньше", func(t *testing.T) {
		tables, err := svc.Tables(context.Background())
		require.NoError(t, err)
		assert.Len(t, tables, 7)
	})
}

func TestAdminTables_SeniorMutationsGated(t *testing.T) {
	repo := &adminStubRepo{}
	svc := NewAdminTables(repo)
	senior := ctxWithClientRoles(auth.ClientSeniorOperator)

	require.NoError(t, svc.Delete(senior, "marka", "1"), "свой словарь senior правит")

	err := svc.Delete(senior, "cargo", "1")
	require.Error(t, err, "чужая таблица недоступна")
	assert.True(t, errors.Is(err, ErrTableForbidden))

	_, _, _, err = svc.TableData(senior, "porozh_cargo")
	assert.True(t, errors.Is(err, ErrTableForbidden), "чтение чужой таблицы тоже закрыто")

	require.NoError(t, svc.Delete(ctxWithClientRoles(auth.ClientAdmin), "cargo", "1"), "admin правит всё")
	assert.Equal(t, []string{"marka", "cargo"}, repo.deleted)
}

package service

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/domain"
)

// lt — хелпер: domain.LocalTime из компонентов (московское naive, без TZ).
func lt(h, m int) *domain.LocalTime {
	t := domain.LocalTime(time.Date(2026, 7, 13, h, m, 0, 0, time.UTC))
	return &t
}

func TestCheckSkew(t *testing.T) {
	a := &ASUIngest{log: zap.NewNop()}

	cases := []struct {
		name    string
		pulled  []pulledSource
		limit   int
		wantErr error
	}{
		{
			name:   "guard off (limit 0)",
			pulled: []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: lt(10, 30)}},
			limit:  0,
		},
		{
			name:   "single source — nothing to compare",
			pulled: []pulledSource{{label: "asu/attis", ts: lt(10, 0)}},
			limit:  2,
		},
		{
			name:   "within limit",
			pulled: []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: lt(10, 2)}},
			limit:  2,
		},
		{
			name:    "over limit",
			pulled:  []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: lt(10, 5)}},
			limit:   2,
			wantErr: ErrSourceSkew,
		},
		{
			name:    "over limit regardless of order",
			pulled:  []pulledSource{{label: "asu/nmtp", ts: lt(10, 5)}, {label: "asu/attis", ts: lt(10, 0)}},
			limit:   2,
			wantErr: ErrSourceSkew,
		},
		{
			name:    "missing formation ts blocks",
			pulled:  []pulledSource{{label: "asu/attis", ts: lt(10, 0)}, {label: "asu/nmtp", ts: nil}},
			limit:   2,
			wantErr: ErrNoFormationTS,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.checkSkew(tc.pulled, tc.limit)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ожидали nil, получили %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ожидали %v, получили %v", tc.wantErr, err)
			}
		})
	}
}

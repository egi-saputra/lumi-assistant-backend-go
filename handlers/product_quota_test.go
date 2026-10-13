package handlers

import (
	"database/sql"
	"testing"
	"time"
)

func TestProductLimit(t *testing.T) {
	now := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)
	future := sql.NullTime{Time: now.Add(time.Hour), Valid: true}
	past := sql.NullTime{Time: now.Add(-time.Hour), Valid: true}

	tests := []struct {
		name      string
		plan      string
		end       sql.NullTime
		bonus     int
		wantPlan  string
		wantLimit int
	}{
		{name: "free base", plan: "free", wantPlan: "free", wantLimit: 10},
		{name: "free after one ad", plan: "free", bonus: 2, wantPlan: "free", wantLimit: 12},
		{name: "free bonus capped", plan: "free", bonus: 100, wantPlan: "free", wantLimit: 20},
		{name: "premium active", plan: "premium", end: future, bonus: 4, wantPlan: "premium", wantLimit: 50},
		{name: "expired premium is free", plan: "premium", end: past, bonus: 0, wantPlan: "free", wantLimit: 10},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, limit := productLimit(test.plan, test.end, test.bonus, now)
			if plan != test.wantPlan || limit != test.wantLimit {
				t.Fatalf("productLimit() = (%q, %d), want (%q, %d)", plan, limit, test.wantPlan, test.wantLimit)
			}
		})
	}
}

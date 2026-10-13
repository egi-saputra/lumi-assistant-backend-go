package auth

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"assistant/utils"
)

func TestTenantDatabaseNameIsReadableAndSafe(t *testing.T) {
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	name := tenantDatabaseName("Café Senja & Roti", id)

	if !strings.HasPrefix(name, "toko_caf_senja_roti_") {
		t.Fatalf("expected readable normalized database name, got %q", name)
	}
	if len(name) > 64 {
		t.Fatalf("database name exceeds MySQL's 64-character limit: %d", len(name))
	}

	second := tenantDatabaseName("Café Senja & Roti", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	if name == second {
		t.Fatalf("database names for different tenant IDs must differ: %q", name)
	}
}

func TestTenantDatabaseNameHandlesLongAndNonAsciiNames(t *testing.T) {
	longName := strings.Repeat("Long store name ", 10)
	name := tenantDatabaseName(longName, "tenant-id")
	if len(name) > 64 {
		t.Fatalf("database name exceeds MySQL's 64-character limit: %d", len(name))
	}
	if !strings.HasPrefix(name, "toko_long_store_name_") {
		t.Fatalf("expected normalized long store name, got %q", name)
	}

	name = tenantDatabaseName("東京", "tenant-id")
	if name != "toko_store_tenant-id" {
		t.Fatalf("expected fallback for names without ASCII letters, got %q", name)
	}
}

func TestGenerateTenantCodeUsesEightUppercaseAlphanumericCharacters(t *testing.T) {
	for range 100 {
		code, err := utils.GenerateTenantCode()
		if err != nil {
			t.Fatalf("GenerateTenantCode returned an error: %v", err)
		}
		if len(code) < 7 || len(code) > 9 {
			t.Fatalf("expected a tenant code between 7 and 9 characters, got %q", code)
		}
		for _, character := range code {
			if !((character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z')) {
				t.Fatalf("expected uppercase alphanumeric tenant code, got %q", code)
			}
		}
	}
}

func TestActivePaidPlanRequiresUnexpiredSubscription(t *testing.T) {
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	future := sql.NullTime{Time: now.Add(time.Hour), Valid: true}
	past := sql.NullTime{Time: now.Add(-time.Hour), Valid: true}

	if !activePaidPlan("month", future, now) {
		t.Fatal("expected active monthly plan to allow creating a store")
	}
	for _, test := range []struct {
		plan string
		end  sql.NullTime
	}{
		{plan: "free", end: future},
		{plan: "month", end: past},
		{plan: "year", end: sql.NullTime{}},
		{plan: "trial", end: future},
	} {
		if activePaidPlan(test.plan, test.end, now) {
			t.Errorf("plan %q with subscription end %+v must not allow creating a store", test.plan, test.end)
		}
	}
}

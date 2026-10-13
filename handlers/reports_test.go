package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseReportDateRange(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{name: "no range", query: ""},
		{name: "valid range", query: "?start_at=2026-10-08T00%3A00%3A00%2B07%3A00&end_at=2026-10-09T00%3A00%3A00%2B07%3A00"},
		{name: "missing end", query: "?start_at=2026-10-08T00%3A00%3A00Z", wantErr: true},
		{name: "invalid timestamp", query: "?start_at=invalid&end_at=2026-10-09T00%3A00%3A00Z", wantErr: true},
		{name: "end before start", query: "?start_at=2026-10-09T00%3A00%3A00Z&end_at=2026-10-08T00%3A00%3A00Z", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("GET", "/reports/summary"+test.query, nil)
			start, end, err := parseReportDateRange(context)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseReportDateRange() error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && test.query != "" && (start == nil || end == nil) {
				t.Fatal("valid range must return both endpoints")
			}
			if !test.wantErr && test.query == "" && (start != nil || end != nil) {
				t.Fatal("empty range must return no endpoints")
			}
		})
	}
}

func TestReportDateClauseUsesHalfOpenRange(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(
		"GET",
		"/reports/summary?start_at=2026-10-08T00%3A00%3A00%2B07%3A00&end_at=2026-10-09T00%3A00%3A00%2B07%3A00",
		nil,
	)
	start, end, err := parseReportDateRange(context)
	if err != nil {
		t.Fatal(err)
	}
	clause, args := reportDateClause("created_at", start, end)
	if clause != "created_at >= ? AND created_at < ?" {
		t.Fatalf("unexpected date clause: %q", clause)
	}
	if len(args) != 2 || args[0] == args[1] {
		t.Fatalf("expected distinct start and exclusive end arguments, got %#v", args)
	}
}

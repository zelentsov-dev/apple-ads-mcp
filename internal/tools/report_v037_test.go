package tools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

func TestReportStorefrontGroupByMigration(t *testing.T) {
	for _, kind := range []string{"campaigns", "adgroups", "keywords", "ads", "searchterms"} {
		t.Run(kind, func(t *testing.T) {
			manager := &v036Manager{}
			service := &Service{manager: manager}
			_, output, err := service.reportHandler("report", kind)(context.Background(), nil, QueryInput{
				AccountInput: AccountInput{Profile: "test", AdAccountID: "123"}, GroupBy: []string{"storefront"},
			})
			if err != nil || output.Error == nil || !strings.Contains(output.Error.Message, "countryOrRegion") || len(manager.calls) != 0 {
				t.Fatalf("output=%+v err=%v calls=%v", output, err, manager.calls)
			}
			input := QueryInput{GroupBy: []string{"countryOrRegion"}}
			request, err := input.reportRequest(kind)
			if err != nil || !reflect.DeepEqual(request["groupBy"], input.GroupBy) {
				t.Fatalf("request=%v err=%v", request, err)
			}
		})
	}
	for _, kind := range []string{"campaigns", "adgroups"} {
		_, err := (QueryInput{Filters: []QueryFilterInput{{Field: "storefront", Operator: "EQUALS", Value: "US"}}}).reportRequest(kind)
		if err != nil {
			t.Fatalf("metadata storefront must remain available: %v", err)
		}
	}
}

func TestGeneralReportsPreserveDateStringsAndEmptyRows(t *testing.T) {
	for _, test := range []struct {
		name, granularity string
		data              any
	}{
		{"daily", "DAILY", []any{map[string]any{"granularMetrics": []any{map[string]any{"date": "2026-09-01", "tapInstalls": "0"}}}}},
		{"hourly", "HOURLY", []any{map[string]any{"granularMetrics": []any{map[string]any{"date": "2026-09-01T03:00:00.000", "tapInstalls": "1"}}}}},
		{"future_date_format", "HOURLY", []any{map[string]any{"granularMetrics": []any{map[string]any{"date": "2026-09-01T03:00:00Z"}}}}},
		{"empty", "DAILY", []any{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &v036Manager{responses: map[string][]appleads.Result{
				"reports/apps/campaigns/query": {{Data: test.data, Status: 200}},
			}}
			service := &Service{manager: manager}
			_, output, err := service.reportHandler("report", "campaigns")(context.Background(), nil, QueryInput{
				AccountInput: AccountInput{Profile: "test", AdAccountID: "123"},
				TimeRange:    &TimeRangeInput{Start: "2026-09-01", End: "2026-09-01", Granularity: test.granularity, TimeZone: "UTC"},
			})
			if err != nil || output.Error != nil || !reflect.DeepEqual(output.Data, test.data) {
				t.Fatalf("output=%+v err=%v", output, err)
			}
		})
	}
}

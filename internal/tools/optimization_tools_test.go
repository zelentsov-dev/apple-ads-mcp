package tools

import (
	"fmt"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
)

func TestOptimizationParsesOfficialCampaignReportShape(t *testing.T) {
	rows := []map[string]any{{
		"metadata":        map[string]any{"id": "123", "name": "Fixture"},
		"granularMetrics": reportMetricFixtures(28),
	}}
	metrics, err := dailyMetrics(rows, "123", "USD")
	if err != nil || len(metrics) != 28 || metrics[0].TapInstalls != 1 || metrics[0].Spend.Amount != "2.50" {
		t.Fatalf("metrics=%+v err=%v", metrics, err)
	}
}

func TestOptimizationParsesOfficialBiddableReportShape(t *testing.T) {
	rows := []map[string]any{{
		"metadata": map[string]any{
			"id": "456", "name": "Search Match", "status": "ENABLED", "automatedKeywordsOptIn": true,
			"bidStrategy": map[string]any{"bid": map[string]any{"amount": "1.25", "currency": "USD"}},
		},
		"granularMetrics": reportMetricFixtures(28),
	}}
	items, err := biddableRows(rows, "ad_group", "USD")
	if err != nil || len(items) != 1 || items[0].ResourceID != "456" || !items[0].SearchMatch || items[0].Bid == nil || items[0].Bid.Amount != "1.25" || len(items[0].Daily) != 28 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestOptimizationReportRejectsMalformedCountsAndShape(t *testing.T) {
	metrics := reportMetricFixtures(1)
	metrics[0].(map[string]any)["tapInstalls"] = "9223372036854775808"
	if _, err := dailyMetrics([]map[string]any{{"metadata": map[string]any{"id": "123"}, "granularMetrics": metrics}}, "123", "USD"); err == nil {
		t.Fatal("expected overflowing count rejection")
	}
	if _, err := biddableRows([]map[string]any{{"id": "456", "date": "2026-08-01"}}, "keyword", "USD"); err == nil {
		t.Fatal("expected legacy flattened report shape rejection")
	}
}

func TestOptimizationReportRejectsPartiallyEmptyMetrics(t *testing.T) {
	metric := reportMetricFixtures(1)[0].(map[string]any)
	delete(metric, "tapInstalls")
	if _, err := granularDailyMetrics(map[string]any{"granularMetrics": []any{metric}}, "USD"); err == nil {
		t.Fatal("expected partially empty metric rejection")
	}
}

func TestOptimizationReportAcceptsFullyEmptyMetricsAsZero(t *testing.T) {
	metrics, err := granularDailyMetrics(map[string]any{"granularMetrics": []any{map[string]any{"date": "2026-08-24"}}}, "USD")
	if err != nil || len(metrics) != 1 || metrics[0].Spend.Amount != "0" || metrics[0].TapInstalls != 0 {
		t.Fatalf("metrics=%+v err=%v", metrics, err)
	}
}

func TestOptimizationReportAcceptsEmptyAppleResult(t *testing.T) {
	rows, err := reportRows(map[string]any{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestOptimizationReportUnwrapsAppleRowsContainer(t *testing.T) {
	want := map[string]any{"metadata": map[string]any{"id": "123"}, "granularMetrics": []any{}}
	rows, err := reportRows(map[string]any{"rows": []any{want}})
	if err != nil || len(rows) != 1 || rows[0]["metadata"] == nil {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestOptimizationReportRequestsEmptyRowsAndRequestedPage(t *testing.T) {
	policy := optimization.Policy{Profile: "owner", AdAccountID: "123", MaxTotalDailyBudget: appleads.Money{Currency: "USD"}}
	start := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	request, err := optimizationBiddableReportRequest(policy, "keywords", "456", start, end, 400)
	if err != nil {
		t.Fatal(err)
	}
	pagination, ok := request["pagination"].(map[string]any)
	if !ok || pagination["offset"] != 400 || pagination["pageSize"] != MaxItems {
		t.Fatalf("pagination=%+v", request["pagination"])
	}
	options, ok := request["options"].(*QueryOptionsInput)
	if !ok || len(options.IncludeRows) != 1 || options.IncludeRows[0] != "EMPTY_METRICS" {
		t.Fatalf("options=%+v", request["options"])
	}
	fields, ok := request["fields"].([]string)
	if !ok || fmt.Sprint(fields) != "[localSpend impressions taps tapInstalls]" {
		t.Fatalf("fields=%+v", request["fields"])
	}
}

func TestOptimizationReportCollectsEveryBoundedPage(t *testing.T) {
	offsets := make([]int, 0, 3)
	rows, err := collectOptimizationReportRows(func(offset int) (appleads.Result, error) {
		offsets = append(offsets, offset)
		page := []any{map[string]any{"metadata": map[string]any{"id": fmt.Sprint(offset + 1)}, "granularMetrics": []any{}}}
		next := ""
		if offset < 400 {
			next = fmt.Sprintf("offset:%d", offset+200)
		}
		return appleads.Result{Data: page, Pagination: appleads.Pagination{Offset: offset, PageSize: 200, Total: 3, Next: next}}, nil
	})
	if err != nil || len(rows) != 3 || fmt.Sprint(offsets) != "[0 200 400]" {
		t.Fatalf("rows=%d offsets=%v err=%v", len(rows), offsets, err)
	}
}

func TestOptimizationReportRejectsIncompletePagination(t *testing.T) {
	_, err := collectOptimizationReportRows(func(offset int) (appleads.Result, error) {
		return appleads.Result{
			Data:       []any{map[string]any{"metadata": map[string]any{"id": "1"}, "granularMetrics": []any{}}},
			Pagination: appleads.Pagination{Offset: offset, PageSize: 1, Total: 2},
		}, nil
	})
	if err == nil {
		t.Fatal("expected incomplete pagination rejection")
	}
}

func reportMetricFixtures(days int) []any {
	end := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	result := make([]any, 0, days)
	for index := days - 1; index >= 0; index-- {
		result = append(result, map[string]any{
			"date":       end.AddDate(0, 0, -index).Format("2006-01-02"),
			"localSpend": map[string]any{"amount": "2.50", "currency": "USD"},
			"taps":       fmt.Sprint(index + 1), "impressions": "100", "tapInstalls": "1",
		})
	}
	return result
}

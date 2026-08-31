package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
)

type v036Manager struct {
	responses map[string][]appleads.Result
	failures  map[string]error
	calls     []string
	profile   config.Profile
}

func (m *v036Manager) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	path := operation.Path()
	m.calls = append(m.calls, path)
	if err := m.failures[path]; err != nil {
		return appleads.Result{}, err
	}
	queue := m.responses[path]
	if len(queue) == 0 {
		return appleads.Result{Data: []any{}, Status: 200}, nil
	}
	result := queue[0]
	if len(queue) > 1 {
		m.responses[path] = queue[1:]
	}
	return result, nil
}

func (m *v036Manager) Profile(string) (config.Profile, error) {
	return m.profile, nil
}

func (*v036Manager) Profiles() []config.PublicProfile {
	return nil
}

type receiptScopeManager struct {
	started        chan struct{}
	release        chan struct{}
	campaignStatus string
	budgetAssigned bool
	now            time.Time
	once           sync.Once
	writes         atomic.Int32
}

func (m *receiptScopeManager) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	if operation.IsMutation() {
		m.writes.Add(1)
		m.once.Do(func() { close(m.started) })
		<-m.release
		return appleads.Result{Data: map[string]any{"id": "501"}, Status: 200}, nil
	}
	switch operation.Path() {
	case "acls":
		return appleads.Result{Data: map[string]any{"acls": []any{map[string]any{
			"adAccount": map[string]any{"id": "123"}, "roles": []any{"API Campaign Manager"},
		}}}, Status: 200}, nil
	case "ad-accounts/123":
		return appleads.Result{Data: map[string]any{"id": "123", "currency": "USD", "paymentModel": "LOC"}, Status: 200}, nil
	case "campaigns/100":
		return appleads.Result{Data: scopeCampaignFixture(m.campaignStatus, m.budgetAssigned), Status: 200}, nil
	case "campaigns/query":
		return appleads.Result{Data: []any{scopeCampaignFixture(m.campaignStatus, m.budgetAssigned)}, Status: 200, Pagination: appleads.Pagination{Offset: 0, PageSize: MaxItems, Total: 1}}, nil
	case "adgroups/10":
		return appleads.Result{Data: map[string]any{"id": "10", "campaignId": "100", "status": "PAUSED"}, Status: 200}, nil
	case "keywords/501":
		return appleads.Result{Data: map[string]any{
			"id": "501", "campaignId": "100", "adGroupId": "10", "text": "voice notes", "matchType": "EXACT", "status": "ENABLED",
			"bid": map[string]any{"amount": "1.00", "currency": "USD"},
		}, Status: 200}, nil
	case "keywords/query":
		return appleads.Result{Data: []any{map[string]any{
			"id": "501", "campaignId": "100", "adGroupId": "10", "text": "voice notes", "matchType": "EXACT", "status": "ENABLED",
		}}, Status: 200, Pagination: appleads.Pagination{Offset: 0, PageSize: MaxItems, Total: 1}}, nil
	case "shared-budgets/700":
		return appleads.Result{Data: map[string]any{"id": "700", "name": "scope budget", "adAccountIds": []any{"123"}}, Status: 200}, nil
	case "reports/apps/campaigns/query":
		return appleads.Result{Data: []any{optimizationReportRow(m.now, "100", "campaign", "0.00", 0)}, Status: 200, Pagination: appleads.Pagination{Offset: 0, PageSize: MaxItems, Total: 1}}, nil
	case "reports/apps/adgroups/query":
		return appleads.Result{Data: []any{}, Status: 200, Pagination: appleads.Pagination{Offset: 0, PageSize: MaxItems}}, nil
	case "reports/apps/keywords/query":
		return appleads.Result{Data: []any{optimizationReportRow(m.now, "501", "voice notes", "1.00", 2)}, Status: 200, Pagination: appleads.Pagination{Offset: 0, PageSize: MaxItems, Total: 1}}, nil
	case "recommendations/daily-budgets/query", "recommendations/target-cpas/query":
		return appleads.Result{Data: []any{}, Status: 200}, nil
	default:
		return appleads.Result{Data: []any{}, Status: 200}, nil
	}
}

func (*receiptScopeManager) Profile(string) (config.Profile, error) {
	return config.Profile{Name: "owner", AllowWrites: true, AllowDeletes: true}, nil
}

func (*receiptScopeManager) Profiles() []config.PublicProfile {
	return nil
}

func TestV036CorrelationIDAndKeywordBulkMatrix(t *testing.T) {
	for _, value := range []any{"0", 0, "001", int64(27), float64(28)} {
		text, wire, err := validateCorrelationID(value, map[string]struct{}{})
		if err != nil || fmt.Sprint(wire) != text {
			t.Fatalf("value=%#v text=%q wire=%v err=%v", value, text, wire, err)
		}
	}
	for _, value := range []any{"ACTIVE", "opaque", -1, float64(1.5)} {
		if value == "ACTIVE" {
			payload, err := typedPayloadMap(KeywordCreatePayload{AdGroupID: "10", Text: "音声メモ", MatchType: "EXACT", Status: value.(string)})
			if err != nil {
				t.Fatal(err)
			}
			if err := validateKeywordPayload(payload, true); err == nil || !strings.Contains(err.Error(), "ACTIVE") {
				t.Fatalf("ACTIVE error=%v", err)
			}
			continue
		}
		if _, _, err := validateCorrelationID(value, map[string]struct{}{}); err == nil {
			t.Fatalf("expected correlation rejection for %#v", value)
		}
	}
	for _, itemCount := range []int{2, 28} {
		manager := &v036Manager{
			profile: config.Profile{Name: "owner", AllowWrites: true},
			responses: map[string][]appleads.Result{
				"adgroups/10":     {{Data: map[string]any{"id": "10", "campaignId": "100"}, Status: 200}},
				"campaigns/100":   {{Data: searchResultsCampaign("100"), Status: 200}},
				"ad-accounts/123": {{Data: map[string]any{"id": "123", "currency": "USD"}, Status: 200}},
				"acls": {{Data: map[string]any{"acls": []any{map[string]any{
					"adAccount": map[string]any{"id": "123"}, "roles": []any{"API Campaign Manager"},
				}}}, Status: 200}},
			},
		}
		service := &Service{manager: manager, allowWrites: true}
		items := make([]BulkKeywordCreateItem, itemCount)
		for index := range items {
			text := fmt.Sprintf("keyword-%d", index)
			if index == itemCount-1 {
				text = "音声メモ"
			}
			items[index] = BulkKeywordCreateItem{CorrelationID: index, Text: text, MatchType: "EXACT", Status: "ENABLED"}
			if itemCount == 28 && index%2 == 0 {
				items[index].Bid = &appleads.Money{Amount: "1.25", Currency: "USD"}
			}
		}
		_, output, err := service.keywordsBulkCreatePreview(operations.NewStore())(context.Background(), nil, BulkKeywordCreateInput{
			AccountInput: AccountInput{Profile: "owner", AdAccountID: "123"}, AdGroupID: "10", Items: items,
		})
		if err != nil || output.Error != nil || output.Preview == nil || len(output.Preview.Items) != itemCount {
			t.Fatalf("itemCount=%d output=%+v err=%v", itemCount, output, err)
		}
	}
}

func TestV036CrossGroupBulkPreviewUsesOneAggregateReceipt(t *testing.T) {
	manager := &v036Manager{
		profile: config.Profile{Name: "owner", AllowWrites: true},
		responses: map[string][]appleads.Result{
			"adgroups/10":   {{Data: map[string]any{"id": "10", "campaignId": "100"}, Status: 200}},
			"adgroups/20":   {{Data: map[string]any{"id": "20", "campaignId": "200"}, Status: 200}},
			"campaigns/100": {{Data: searchResultsCampaign("100"), Status: 200}},
			"campaigns/200": {{Data: searchResultsCampaign("200"), Status: 200}},
			"acls": {{Data: map[string]any{"acls": []any{map[string]any{
				"adAccount": map[string]any{"id": "123"}, "roles": []any{"API Campaign Manager"},
			}}}, Status: 200}},
		},
	}
	service := &Service{manager: manager, allowWrites: true}
	store := operations.NewStore()
	_, output, err := service.keywordsBulkCreatePreview(store)(context.Background(), nil, BulkKeywordCreateInput{
		AccountInput: AccountInput{Profile: "owner", AdAccountID: "123"}, AdGroupID: "10",
		Items: []BulkKeywordCreateItem{
			{CorrelationID: 0, Text: "voice notes", MatchType: "EXACT", Status: "ENABLED"},
			{CorrelationID: "1", AdGroupID: "20", Text: "音声メモ", MatchType: "BROAD", Status: "ENABLED"},
		},
	})
	if err != nil || output.Error != nil || output.Preview == nil {
		t.Fatalf("output=%+v err=%v", output, err)
	}
	if len(output.Preview.Items) != 2 || output.Preview.Items[0].CorrelationID != "0" || output.Preview.Items[1].CampaignID != "200" {
		t.Fatalf("preview=%+v", output.Preview)
	}
	before, ok := output.Preview.Before.(map[string]any)
	if !ok || len(before) != 2 || len(output.Preview.Impact.ParentIDs) != 4 {
		t.Fatalf("before=%#v impact=%+v", output.Preview.Before, output.Preview.Impact)
	}
	for _, path := range manager.calls {
		if path == "keywords/bulk-create" {
			t.Fatal("preview must not send the Apple bulk mutation")
		}
	}
	manager.responses["keywords/bulk-create"] = []appleads.Result{{Data: map[string]any{"items": []any{
		map[string]any{"correlationId": 0, "success": true, "result": map[string]any{"id": "501"}},
		map[string]any{"correlationId": 1, "success": true, "result": map[string]any{"id": "502"}},
	}}, Status: 200}}
	manager.responses["keywords/501"] = []appleads.Result{{Data: map[string]any{"id": "501", "adGroupId": "10", "text": "voice notes", "matchType": "EXACT", "status": "ENABLED"}, Status: 200}}
	manager.responses["keywords/502"] = []appleads.Result{{Data: map[string]any{"id": "502", "adGroupId": "20", "text": "音声メモ", "matchType": "BROAD", "status": "ENABLED"}, Status: 200}}
	receipt, err := store.Apply(context.Background(), manager, output.Preview.Receipt)
	if err != nil || receipt.Status != "applied" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	bulkCalls := 0
	for _, path := range manager.calls {
		if path == "keywords/bulk-create" {
			bulkCalls++
		}
	}
	if bulkCalls != 1 {
		t.Fatalf("bulk calls=%d paths=%v", bulkCalls, manager.calls)
	}
	verification, err := store.Verify(context.Background(), manager, output.Preview.Receipt)
	if err != nil || verification.Status != "verified" {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
}

func TestV036BulkReadOnlyGatePrecedesAppleReads(t *testing.T) {
	manager := &v036Manager{}
	service := &Service{manager: manager}
	_, output, err := service.keywordsBulkCreatePreview(operations.NewStore())(context.Background(), nil, BulkKeywordCreateInput{
		AccountInput: AccountInput{Profile: "owner", AdAccountID: "123"}, AdGroupID: "10",
		Items: []BulkKeywordCreateItem{{CorrelationID: 0, Text: "voice", MatchType: "EXACT", Status: "ENABLED"}},
	})
	if err != nil || output.Error == nil || output.Error.Type != "write_gate_error" || output.Error.Message != "server is in read-only mode; restart with --allow-writes" {
		t.Fatalf("output=%+v err=%v", output, err)
	}
	if len(manager.calls) != 0 {
		t.Fatalf("read-only bulk preview contacted Apple: %v", manager.calls)
	}
}

func TestV036QueryContractsAndAliases(t *testing.T) {
	if id, err := resolveResourceAlias("123", "123", "campaignId"); err != nil || id != "123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := resolveResourceAlias("123", "456", "campaignId"); err == nil {
		t.Fatal("mismatched aliases must fail")
	}
	if err := validateKeywordQueryScope(nil); err == nil {
		t.Fatal("unscoped keywords query must fail")
	}
	if err := validateKeywordQueryScope([]QueryFilterInput{{Field: "id", Operator: "IS_NULL"}}); err == nil {
		t.Fatal("null keyword ID must not satisfy the scope requirement")
	}
	for _, filters := range [][]QueryFilterInput{
		{{Field: "campaignId", Operator: "IN", Value: []string{"1"}}},
		{{Field: "adGroupId", Operator: "NOT_EQUALS", Value: "1"}},
		{{Field: "campaignId", Operator: "IN", Value: []string{}}},
		{{Field: "adGroupId", Operator: "IN", Value: []any{}}},
		{{Field: "id", Operator: "EQUALS", Value: "0"}},
	} {
		if err := validateKeywordQueryScope(filters); err == nil {
			t.Fatalf("invalid keyword scope accepted: %+v", filters)
		}
	}
	for _, filters := range [][]QueryFilterInput{
		{{Field: "campaignId", Operator: "EQUALS", Value: "1"}},
		{{Field: "adGroupId", Operator: "IN", Value: []string{"1", "2"}}},
		{{Field: "id", Operator: "IN", Value: []any{"1"}}},
		{
			{Field: "campaignId", Operator: "EQUALS", Value: "1"},
			{Field: "text", Operator: "STARTS_WITH", Value: "voice"},
			{Field: "matchType", Operator: "IN", Value: []string{"EXACT", "BROAD"}},
			{Field: "status", Operator: "EQUALS", Value: "ENABLED"},
			{Field: "deleted", Operator: "EQUALS", Value: false},
		},
	} {
		if err := validateKeywordQueryScope(filters); err != nil {
			t.Fatalf("valid keyword scope rejected: %+v: %v", filters, err)
		}
	}
	if err := validateNegativeKeywordQueryScope([]QueryFilterInput{{Field: "campaignId", Operator: "EQUALS", Value: "1"}}); err == nil {
		t.Fatal("campaign-only legacy negative query must fail")
	}
	if err := validateNegativeKeywordQueryScope([]QueryFilterInput{{Field: "campaignId", Operator: "EQUALS", Value: "1"}, {Field: "adGroupId", Operator: "IS_NULL"}}); err != nil {
		t.Fatal(err)
	}
	if err := validateNegativeKeywordQueryScope([]QueryFilterInput{{Field: "campaignId", Operator: "IN", Value: []string{"1"}}, {Field: "adGroupId", Operator: "IS_NULL"}}); err == nil {
		t.Fatal("campaignId IN must be rejected for negative keyword scope")
	}
	if err := validateNegativeKeywordQueryScope([]QueryFilterInput{{Field: "adGroupId", Operator: "IN", Value: []string{}}}); err == nil {
		t.Fatal("empty negative keyword adGroupId IN must be rejected")
	}
	if err := validateNegativeKeywordQueryScope([]QueryFilterInput{
		{Field: "campaignId", Operator: "EQUALS", Value: "1"},
		{Field: "adGroupId", Operator: "IS_NULL"},
		{Field: "text", Operator: "STARTS_WITH", Value: "voice"},
		{Field: "status", Operator: "IN", Value: []string{"ENABLED", "PAUSED"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, filters := range [][]QueryFilterInput{
		{{Field: "campaignId", Operator: "EQUALS", Value: "1"}, {Field: "text", Operator: "STARTSWITH", Value: "voice"}},
		{{Field: "campaignId", Operator: "EQUALS", Value: "1"}, {Field: "typo", Operator: "EQUALS", Value: "voice"}},
		{{Field: "campaignId", Operator: "EQUALS", Value: "1"}, {Field: "matchType", Operator: "IN", Value: []string{"PHRASE"}}},
		{{Field: "campaignId", Operator: "EQUALS", Value: "1"}, {Field: "deleted", Operator: "EQUALS", Value: "false"}},
	} {
		if err := validateKeywordQueryScope(filters); err == nil {
			t.Fatalf("invalid keyword filter accepted: %+v", filters)
		}
	}
	if err := validateNegativeKeywordQueryScope([]QueryFilterInput{{Field: "adGroupId", Operator: "EQUALS", Value: "1"}, {Field: "deleted", Operator: "EQUALS", Value: false}}); err == nil {
		t.Fatal("unsupported negative keyword filter field must fail")
	}
	if _, err := addShortcutFilters(QueryInput{Filters: []QueryFilterInput{{Field: "campaignId", Operator: "EQUALS", Value: "1"}}}, []QueryFilterInput{shortcutEquals("campaignId", "1")}); err == nil {
		t.Fatal("shortcut/filter overlap must fail")
	}
}

func TestV036KeywordReportChangeHistoryAndImpressionShareRequests(t *testing.T) {
	keywordRequest, err := keywordReportRequest(KeywordReportInput{IncludeZeroMetrics: true})
	if err != nil {
		t.Fatal(err)
	}
	options := keywordRequest["options"].(*QueryOptionsInput)
	if !containsFold(options.IncludeRows, "EMPTY_METRICS") {
		t.Fatalf("options=%+v", options)
	}
	needTotals := false
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	changeRequest, err := changeHistoryRequestAt(ChangeHistoryInput{
		Start: "2026-08-01", End: "2026-08-31", EntityTypes: []string{"Keyword"}, EventTypes: []string{"UPDATE"},
		CampaignIDs: []string{"100"}, AdGroupIDs: []string{"10"}, NeedTotals: &needTotals,
		QueryInput: QueryInput{Sorting: []QuerySortInput{{Field: "entityType", Order: "DESC"}}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	filters := changeRequest["filters"].([]QueryFilterInput)
	changeOptions := changeRequest["options"].(map[string]any)
	if filters[0].Operator != "BETWEEN" || len(filters) != 5 || changeOptions["metadata"] != "latest" || changeOptions["needTotals"] != "false" {
		t.Fatalf("request=%+v", changeRequest)
	}
	if _, err := changeHistoryRequestAt(ChangeHistoryInput{Start: "", End: "2026-08-31"}, now); err == nil {
		t.Fatal("missing change-history start must fail")
	}
	if _, err := changeHistoryRequestAt(ChangeHistoryInput{Start: "2026-08-01", End: "2026-08-31", EventTypes: []string{"UPSERT"}}, now); err == nil {
		t.Fatal("unsupported change-history event type must fail")
	}
	if _, err := changeHistoryRequestAt(ChangeHistoryInput{Start: "2026-02-28", End: "2026-08-28"}, now); err != nil {
		t.Fatalf("exact six-month lookback boundary rejected: %v", err)
	}
	if _, err := changeHistoryRequestAt(ChangeHistoryInput{Start: "2026-02-27", End: "2026-08-27"}, now); err == nil {
		t.Fatal("stale change-history window must fail")
	}
	impressionRequest, err := impressionShareRequest(ImpressionShareInput{
		AdamID: "123456789", Country: "us", Start: "2026-08-01", End: "2026-08-30", Granularity: "DAILY", ReportType: "FIRST_SLOT",
	})
	if err != nil {
		t.Fatal(err)
	}
	timeRange := impressionRequest["timeRange"].(map[string]any)
	if _, exists := timeRange["timeZone"]; exists {
		t.Fatal("impression_share must not send timeZone")
	}
	if _, err := impressionShareRequest(ImpressionShareInput{AdamID: "1", Start: "2026-08-01", End: "2026-09-01", Granularity: "DAILY", ReportType: "ALL_SLOTS"}); err == nil {
		t.Fatal("31-day DAILY window must fail")
	}
}

func TestV036OwnedUnicodeFallbackAndCompactLocaleDetails(t *testing.T) {
	manager := &v036Manager{responses: map[string][]appleads.Result{
		"search/apps": {
			{Data: []any{}, Status: 200},
			{Data: []any{map[string]any{"adamId": "123", "appName": "セットログ"}, map[string]any{"adamId": "456", "appName": "Other"}}, Status: 200},
		},
		"apps/123/locale-details/query": {{Data: localeFixture(), Status: 200, Pagination: appleads.Pagination{Total: 1, PageSize: 1}}},
	}}
	service := &Service{manager: manager}
	_, searchOutput, err := service.appsSearch(context.Background(), nil, AppsSearchInput{
		AccountInput: AccountInput{Profile: "owner", AdAccountID: "10"}, Query: "セットログ", ReturnOwnedApps: true,
	})
	if err != nil || searchOutput.Error != nil {
		t.Fatalf("output=%+v err=%v", searchOutput, err)
	}
	if items := searchOutput.Data.([]any); len(items) != 1 || stringField(items[0].(map[string]any), "adamId") != "123" {
		t.Fatalf("data=%#v", searchOutput.Data)
	}
	_, localeOutput, err := service.appLocaleDetails(context.Background(), nil, AppLocaleDetailsInput{
		QueryInput: QueryInput{AccountInput: AccountInput{Profile: "owner", AdAccountID: "10"}}, AdamID: "123",
	})
	if err != nil || localeOutput.Error != nil {
		t.Fatalf("output=%+v err=%v", localeOutput, err)
	}
	locale := localeOutput.Data.([]any)[0].(map[string]any)
	if _, fullAssets := locale["assetsByDevice"]; fullAssets || locale["assetCountsByDevice"] == nil {
		t.Fatalf("locale=%#v", locale)
	}
	_, fullOutput, _ := service.appLocaleDetails(context.Background(), nil, AppLocaleDetailsInput{
		QueryInput: QueryInput{AccountInput: AccountInput{Profile: "owner", AdAccountID: "10"}}, AdamID: "123", IncludeAssets: true,
	})
	if fullOutput.Error == nil || fullOutput.Error.Type != "validation_error" {
		t.Fatalf("output=%+v", fullOutput)
	}
	_, fullOutput, err = service.appLocaleDetails(context.Background(), nil, AppLocaleDetailsInput{
		QueryInput: QueryInput{AccountInput: AccountInput{Profile: "owner", AdAccountID: "10"}}, AdamID: "123", LanguageCode: "ja-JP", IncludeAssets: true,
	})
	if err != nil || fullOutput.Error != nil {
		t.Fatalf("output=%+v err=%v", fullOutput, err)
	}
	fullLocale := fullOutput.Data.([]any)[0].(map[string]any)
	if fullLocale["assetsByDevice"] == nil {
		t.Fatalf("locale=%#v", fullLocale)
	}
}

func TestV036ImpressionSharePreservesApple400AndKeywordReportWarns(t *testing.T) {
	manager := &v036Manager{failures: map[string]error{
		"insights/apps/impression-share/query": &appleads.APIError{
			HTTPStatus: 400, Code: "INVALID_VALUE", Message: "Invalid report request",
			Body: map[string]any{"code": "INVALID_VALUE", "message": "Invalid report request"},
		},
	}}
	service := &Service{manager: manager}
	_, output, err := service.impressionShare(context.Background(), nil, ImpressionShareInput{
		AccountInput: AccountInput{Profile: "owner", AdAccountID: "10"}, AdamID: "123", Country: "US",
		Start: "2026-08-01", End: "2026-08-07", Granularity: "DAILY", ReportType: "ALL_SLOTS",
	})
	if err != nil || output.Error == nil || output.Error.Type != "apple_api_error" || output.Error.HTTPStatus != 400 || output.Error.AppleBody["code"] != "INVALID_VALUE" {
		t.Fatalf("output=%+v err=%v", output, err)
	}
	_, report, err := service.keywordReport(context.Background(), nil, KeywordReportInput{QueryInput: QueryInput{AccountInput: AccountInput{Profile: "owner", AdAccountID: "10"}}})
	if err != nil || report.Error != nil || !strings.Contains(report.Summary, "keywords_query") {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestV036AppOpportunitiesSectionsAndEmptyAdsExplanation(t *testing.T) {
	manager := &v036Manager{
		responses: map[string][]appleads.Result{
			"search/apps":                   {{Data: []any{map[string]any{"adamId": "123"}}, Status: 200}},
			"eligibilities/apps/query":      {{Data: []any{map[string]any{"eligible": true}}, Status: 200}},
			"suggestions/keywords/query":    {{Data: []any{map[string]any{"text": "voice"}}, Status: 200}},
			"suggestions/categories/query":  {{Data: []any{}, Status: 200}},
			"suggestions/target-cpas/query": {{Data: []any{map[string]any{"amount": "1.00"}}, Status: 200}},
			"ads/query":                     {{Data: []any{}, Status: 200}},
		},
		failures: map[string]error{"suggestions/phrases/query": &appleads.APIError{HTTPStatus: 500, Code: "UPSTREAM", Message: "Internal Server Error"}},
	}
	service := &Service{manager: manager}
	_, output, err := service.appOpportunities(context.Background(), nil, AppOpportunityInput{
		AccountInput: AccountInput{Profile: "owner", AdAccountID: "10"}, AdamID: "123", CountriesOrRegions: []string{"US"},
	})
	if err != nil || output.Error != nil {
		t.Fatalf("output=%+v err=%v", output, err)
	}
	sections := output.Data.(map[string]any)
	if sections["phrases"].(map[string]any)["status"] != "upstream_error" || sections["categories"].(map[string]any)["status"] != "empty" || sections["keywords"].(map[string]any)["status"] != "ok" {
		t.Fatalf("sections=%#v", sections)
	}
	_, adsOutput, err := service.resourceQuery(context.Background(), AccountInput{Profile: "owner", AdAccountID: "10"}, QueryInput{}, "ads", "Ads loaded", nil, nil)
	if err != nil || !strings.Contains(adsOutput.Summary, "no explicit Ad objects") {
		t.Fatalf("output=%+v err=%v", adsOutput, err)
	}
}

func TestV036DeleteAndBulkUpdateShareHandlerScopes(t *testing.T) {
	t.Setenv("APPLE_ADS_ALLOW_DELETES", "true")
	manager := &receiptScopeManager{started: make(chan struct{}), release: make(chan struct{}), campaignStatus: "PAUSED"}
	service := &Service{manager: manager, allowWrites: true, allowDeletes: true}
	store := operations.NewStore()
	account := AccountInput{Profile: "owner", AdAccountID: "123"}
	_, deleteOutput, err := service.deletePreview(store, "keywords", "keyword_delete_preview")(context.Background(), nil, DeletePreviewInput{
		AccountInput: account, ID: "501", ExpectedText: "voice notes",
	})
	if err != nil || deleteOutput.Error != nil || deleteOutput.Preview == nil {
		t.Fatalf("delete output=%+v err=%v", deleteOutput, err)
	}
	status := "PAUSED"
	_, bulkOutput, err := service.keywordsBulkUpdatePreview(store)(context.Background(), nil, BulkKeywordUpdateInput{
		AccountInput: account, AdGroupID: "10", Items: []BulkKeywordUpdateItem{{CorrelationID: 0, ID: "501", Status: &status}},
	})
	if err != nil || bulkOutput.Error != nil || bulkOutput.Preview == nil {
		t.Fatalf("bulk output=%+v err=%v", bulkOutput, err)
	}
	assertHandlerReceiptsAllowOneDispatch(t, store, manager, deleteOutput.Preview.Receipt, bulkOutput.Preview.Receipt)
}

func TestV036ChildDeleteAndParentResumeShareHandlerScopes(t *testing.T) {
	t.Setenv("APPLE_ADS_ALLOW_DELETES", "true")
	manager := &receiptScopeManager{started: make(chan struct{}), release: make(chan struct{}), campaignStatus: "PAUSED"}
	service := &Service{manager: manager, allowWrites: true, allowDeletes: true}
	store := operations.NewStore()
	account := AccountInput{Profile: "owner", AdAccountID: "123"}
	_, deleteOutput, err := service.deletePreview(store, "keywords", "keyword_delete_preview")(context.Background(), nil, DeletePreviewInput{
		AccountInput: account, ID: "501", ExpectedText: "voice notes",
	})
	if err != nil || deleteOutput.Error != nil || deleteOutput.Preview == nil {
		t.Fatalf("delete output=%+v err=%v", deleteOutput, err)
	}
	_, resumeOutput, err := service.campaignStatePreview(store, "ENABLED", "campaign_resume")(context.Background(), nil, CampaignStatePreviewInput{
		AccountInput: account, CampaignID: "100",
	})
	if err != nil || resumeOutput.Error != nil || resumeOutput.Preview == nil {
		t.Fatalf("resume output=%+v err=%v", resumeOutput, err)
	}
	assertHandlerReceiptsAllowOneDispatch(t, store, manager, deleteOutput.Preview.Receipt, resumeOutput.Preview.Receipt)
}

func TestV036KeywordBidAndOptimizerShareHandlerScopes(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	policyPath := filepath.Join(root, "optimization-policies.json")
	policy := optimization.Policy{
		Name: "scope-test", Profile: "owner", AdAccountID: "123", PromotedObjectID: "456",
		CampaignIDs: []string{"100"}, Mode: "active",
		TargetInstallCPA:       &appleads.Money{Amount: "10.00", Currency: "USD"},
		MaxTotalDailyBudget:    appleads.Money{Amount: "100.00", Currency: "USD"},
		MaxCampaignDailyBudget: appleads.Money{Amount: "50.00", Currency: "USD"},
		MaxBid:                 &appleads.Money{Amount: "5.00", Currency: "USD"},
		Permissions:            optimization.Permissions{Bid: true}, Preset: "balanced",
	}
	if err := optimization.SavePolicies(policyPath, optimization.PolicyFile{Policies: []optimization.Policy{policy}}); err != nil {
		t.Fatal(err)
	}
	manager := &receiptScopeManager{started: make(chan struct{}), release: make(chan struct{}), campaignStatus: "ENABLED", now: now}
	service := &Service{manager: manager, allowWrites: true, policyPath: policyPath, historyRoot: filepath.Join(root, "history"), now: func() time.Time { return now }}
	store := operations.NewStore()
	account := AccountInput{Profile: "owner", AdAccountID: "123"}
	_, bidOutput, err := service.keywordBidPreview(store)(context.Background(), nil, KeywordBidPreviewInput{
		AccountInput: account, KeywordID: "501", CampaignID: "100", AdGroupID: "10", Bid: appleads.Money{Amount: "1.10", Currency: "USD"},
	})
	if err != nil || bidOutput.Error != nil || bidOutput.Preview == nil {
		t.Fatalf("bid output=%+v err=%v", bidOutput, err)
	}
	_, optimizerOutput, err := service.optimizationPlanPreview(store)(context.Background(), nil, OptimizationPolicyInput{AccountInput: account, Policy: policy.Name})
	if err != nil || optimizerOutput.Error != nil || optimizerOutput.Preview == nil {
		t.Fatalf("optimizer output=%+v err=%v", optimizerOutput, err)
	}
	if len(optimizerOutput.Preview.Items) != 1 || optimizerOutput.Preview.Items[0].TargetID != "501" {
		t.Fatalf("optimizer preview=%+v", optimizerOutput.Preview)
	}
	assertHandlerReceiptsAllowOneDispatch(t, store, manager, bidOutput.Preview.Receipt, optimizerOutput.Preview.Receipt)
}

func TestV036SharedBudgetUnassignAndUpdateShareHandlerScopes(t *testing.T) {
	manager := &receiptScopeManager{started: make(chan struct{}), release: make(chan struct{}), campaignStatus: "PAUSED", budgetAssigned: true}
	service := &Service{manager: manager, allowWrites: true}
	store := operations.NewStore()
	account := AccountInput{Profile: "owner", AdAccountID: "123"}
	_, unassignOutput, err := service.campaignSharedBudgetPreview(store, false)(context.Background(), nil, CampaignSharedBudgetPreviewInput{
		AccountInput: account, CampaignID: "100", SharedBudgetID: "700",
	})
	if err != nil || unassignOutput.Error != nil || unassignOutput.Preview == nil {
		t.Fatalf("unassign output=%+v err=%v", unassignOutput, err)
	}
	name := "renamed scope budget"
	_, updateOutput, err := service.sharedBudgetUpdatePreview(store)(context.Background(), nil, SharedBudgetUpdatePreviewInput{
		AccountInput: account, SharedBudgetID: "700", Name: &name,
	})
	if err != nil || updateOutput.Error != nil || updateOutput.Preview == nil {
		t.Fatalf("update output=%+v err=%v", updateOutput, err)
	}
	assertHandlerReceiptsAllowOneDispatch(t, store, manager, unassignOutput.Preview.Receipt, updateOutput.Preview.Receipt)
}

func TestV036SharedBudgetAssignAndDeleteShareHandlerScopes(t *testing.T) {
	t.Setenv("APPLE_ADS_ALLOW_DELETES", "true")
	manager := &receiptScopeManager{started: make(chan struct{}), release: make(chan struct{}), campaignStatus: "PAUSED"}
	service := &Service{manager: manager, allowWrites: true, allowDeletes: true}
	store := operations.NewStore()
	account := AccountInput{Profile: "owner", AdAccountID: "123"}
	_, assignOutput, err := service.campaignSharedBudgetPreview(store, true)(context.Background(), nil, CampaignSharedBudgetPreviewInput{
		AccountInput: account, CampaignID: "100", SharedBudgetID: "700",
	})
	if err != nil || assignOutput.Error != nil || assignOutput.Preview == nil {
		t.Fatalf("assign output=%+v err=%v", assignOutput, err)
	}
	_, deleteOutput, err := service.deletePreview(store, "shared-budgets", "shared_budget_delete_preview")(context.Background(), nil, DeletePreviewInput{
		AccountInput: account, ID: "700", ExpectedText: "scope budget",
	})
	if err != nil || deleteOutput.Error != nil || deleteOutput.Preview == nil {
		t.Fatalf("delete output=%+v err=%v", deleteOutput, err)
	}
	assertHandlerReceiptsAllowOneDispatch(t, store, manager, assignOutput.Preview.Receipt, deleteOutput.Preview.Receipt)
}

func assertHandlerReceiptsAllowOneDispatch(t *testing.T, store *operations.Store, manager *receiptScopeManager, firstReceipt, secondReceipt string) {
	t.Helper()
	firstResult := make(chan error, 1)
	go func() {
		_, err := store.Apply(context.Background(), manager, firstReceipt)
		firstResult <- err
	}()
	select {
	case <-manager.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first handler receipt did not reach mutation dispatch")
	}
	if _, err := store.Apply(context.Background(), manager, secondReceipt); !errors.Is(err, operations.ErrStateDrift) {
		t.Fatalf("second overlapping handler receipt error=%v", err)
	}
	close(manager.release)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	if writes := manager.writes.Load(); writes != 1 {
		t.Fatalf("mutation dispatches=%d", writes)
	}
}

func scopeCampaignFixture(status string, budgetAssigned bool) map[string]any {
	result := map[string]any{
		"id": "100", "name": "scope fixture", "status": status, "systemStatus": "RUNNING", "promotedObjectType": "APPSTORE_APP",
		"dailyBudget": map[string]any{"amount": "50.00", "currency": "USD"},
		"bidStrategy": map[string]any{"bidStrategyType": "MANUAL_CPT"},
		"targeting":   map[string]any{"supplyPlacement": map[string]any{"include": []any{"APPSTORE_SEARCH_RESULTS"}}},
	}
	if budgetAssigned {
		result["sharedBudgets"] = []any{map[string]any{"budgetId": "700"}}
	}
	return result
}

func optimizationReportRow(now time.Time, id, name, spend string, installs int64) map[string]any {
	metrics := make([]any, 0, 28)
	for day := 28; day >= 1; day-- {
		metrics = append(metrics, map[string]any{
			"date":       now.AddDate(0, 0, -day).Format("2006-01-02"),
			"localSpend": map[string]any{"amount": spend, "currency": "USD"},
			"taps":       max64Tool(installs*3, 1), "impressions": int64(100), "tapInstalls": installs,
		})
	}
	metadata := map[string]any{"id": id, "name": name, "status": "ENABLED"}
	if id == "501" {
		metadata["text"] = name
		metadata["bid"] = map[string]any{"amount": "1.00", "currency": "USD"}
	}
	return map[string]any{"metadata": metadata, "granularMetrics": metrics}
}

func max64Tool(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func searchResultsCampaign(id string) map[string]any {
	return map[string]any{
		"id": id, "promotedObjectType": "APPSTORE_APP",
		"targeting": map[string]any{"supplyPlacement": map[string]any{"include": []any{"APPSTORE_SEARCH_RESULTS"}}},
	}
}

func localeFixture() []any {
	return []any{map[string]any{
		"adamId": "123", "language": "ja", "languageCode": "ja-JP", "appName": "セットログ", "deviceClasses": []any{"IPHONE"},
		"assetsByDevice": map[string]any{"iphone_6_7": map[string]any{
			"assets": []any{map[string]any{"assetId": "a"}, map[string]any{"assetId": "b"}}, "appPreviewDeviceFallBackDevices": []any{"iphone_6_5"},
		}},
	}}
}

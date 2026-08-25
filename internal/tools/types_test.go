package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

func TestQueryInputBoundedRequestNormalizesAdamIDs(t *testing.T) {
	input := QueryInput{Filters: []QueryFilterInput{
		{Field: "adamId", Operator: "EQUALS", Value: "7654321098"},
		{Field: "adamId", Operator: "IN", Value: []string{"1", "2"}},
		{Field: "promotedObjectId", Operator: "EQUALS", Value: []string{"7654321098"}},
	}}

	request, err := input.boundedRequest()
	if err != nil {
		t.Fatal(err)
	}
	filters := request["filters"].([]QueryFilterInput)
	if filters[0].Value != json.Number("7654321098") {
		t.Fatalf("adamId was not normalized: %#v", filters[0].Value)
	}
	if !reflect.DeepEqual(filters[1].Value, []any{json.Number("1"), json.Number("2")}) {
		t.Fatalf("adamId array was not normalized: %#v", filters[1].Value)
	}
	if !reflect.DeepEqual(filters[2].Value, []string{"7654321098"}) {
		t.Fatalf("promotedObjectId must remain a public string: %#v", filters[2].Value)
	}

	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	expected := `"field":"adamId","operator":"EQUALS","value":7654321098`
	if !strings.Contains(string(encoded), expected) {
		t.Fatalf("wire request does not contain numeric adamId: %s", encoded)
	}
}

func TestQueryInputBoundedRequestRejectsInvalidAdamID(t *testing.T) {
	for _, value := range []any{"not-an-id", "0", -1, float64(1.5)} {
		input := QueryInput{Filters: []QueryFilterInput{{Field: "adamId", Operator: "EQUALS", Value: value}}}
		if _, err := input.boundedRequest(); err == nil {
			t.Fatalf("expected invalid adamId error for %#v", value)
		}
	}
}

func TestCampaignReportSelectorAndEndpointRules(t *testing.T) {
	request, err := (QueryInput{
		Filters: []QueryFilterInput{{Field: "id", Operator: "EQUALS", Value: "123"}},
		Fields:  []string{"id", "localSpend", "dailyBudget"},
	}).reportRequest("campaigns")
	if err != nil || request == nil {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	if _, err := (QueryInput{Filters: []QueryFilterInput{{Field: "campaignId", Operator: "EQUALS", Value: "123"}}}).reportRequest("campaigns"); err == nil {
		t.Fatal("campaign report must use id rather than campaignId")
	}
	if _, err := (QueryInput{TimeRange: &TimeRangeInput{Start: "2026-08-01", End: "2026-08-02", TimeZone: "UTC"}}).reportRequest("searchterms"); err == nil {
		t.Fatal("search-term report must reject UTC")
	}
	if _, err := (QueryInput{
		Fields:    []string{"date", "localSpend"},
		TimeRange: &TimeRangeInput{Start: "2026-08-01", End: "2026-08-14", Granularity: "DAILY", TimeZone: "UTC"},
	}).reportRequest("campaigns"); err == nil {
		t.Fatal("Apple rejects date as a selected report field")
	}
}

func TestNullQueryOperatorOmitsValueAndNormalizesIDs(t *testing.T) {
	request, err := (QueryInput{Filters: []QueryFilterInput{
		{Field: "campaignId", Operator: "equals", Value: "123"},
		{Field: "adGroupId", Operator: "is_null"},
	}}).boundedRequest()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"filters":[{"field":"campaignId","operator":"EQUALS","value":123},{"field":"adGroupId","operator":"IS_NULL"}],"pagination":{"offset":0,"pageSize":50}}` {
		t.Fatalf("request=%s", data)
	}
}

func TestPublicOutputRemovesBillingPIIRecursively(t *testing.T) {
	value := sanitizePublicData(map[string]any{
		"name":             "Shared budget",
		"invoiceDetail":    map[string]any{"billingEmail": "private@example.com"},
		"invoiceContacts":  []any{map[string]any{"email": "invoice@example.com"}},
		"billing_contact":  map[string]any{"email": "billing@example.com"},
		"primaryBuyerName": "Private Buyer",
		"nested":           []any{map[string]any{"primaryBuyerEmail": "buyer@example.com", "amount": "10.00"}},
	}).(map[string]any)
	if _, exists := value["invoiceDetail"]; exists {
		t.Fatal("invoice detail must be removed")
	}
	for _, key := range []string{"invoiceContacts", "billing_contact", "primaryBuyerName"} {
		if _, exists := value[key]; exists {
			t.Fatalf("%s must be removed", key)
		}
	}
	nested := value["nested"].([]any)[0].(map[string]any)
	if _, exists := nested["primaryBuyerEmail"]; exists || nested["amount"] != "10.00" {
		t.Fatalf("nested=%#v", nested)
	}
}

func TestOptimizationRecommendationMoneyFields(t *testing.T) {
	recommendation := map[string]any{
		"suggestedDailyBudgetAmount": map[string]any{"amount": "25.00", "currency": "usd"},
		"recommendedTargetCPA":       map[string]any{"amount": "8.50", "currency": "USD"},
	}
	budget, err := moneyFromObject(recommendation["suggestedDailyBudgetAmount"])
	if err != nil || budget.Amount != "25.00" || budget.Currency != "USD" {
		t.Fatalf("budget=%+v err=%v", budget, err)
	}
	target, err := moneyFromObject(recommendation["recommendedTargetCPA"])
	if err != nil || target.Amount != "8.50" || target.Currency != "USD" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
}

func TestAppleAPIErrorOutputRemainsStructured(t *testing.T) {
	output := errorOutput(&appleads.APIError{
		HTTPStatus:     400,
		Code:           "INVALID_VALUE",
		Message:        "Invalid selector value",
		ResponseFormat: "non_json",
		Details: map[string]any{
			"details": []any{map[string]any{"code": "INVALID", "message": "Invalid filter"}},
		},
	})
	if output.Error == nil || output.Error.Type != "apple_api_error" || output.Error.HTTPStatus != 400 || output.Error.Code != "INVALID_VALUE" {
		t.Fatalf("output=%#v", output)
	}
	if !strings.Contains(output.Summary, "HTTP 400 (INVALID_VALUE)") {
		t.Fatalf("summary=%q", output.Summary)
	}
	if output.Error.Message != "Invalid selector value" || output.Error.Details == nil || output.Error.ResponseFormat != "non_json" {
		t.Fatalf("structured diagnostics missing: %#v", output.Error)
	}
}

func TestContentProviderDelegationsComeFromAdAccount(t *testing.T) {
	account := map[string]any{"delegations": []any{
		map[string]any{"resourceId": "555666777", "resourceType": "CONTENT_PROVIDER"},
		map[string]any{"resourceId": "maps-brand", "resourceType": "BUSINESS_BRAND"},
	}}
	resources := []any{}
	accountCPIDs := contentProviderResourceIDs(account)
	availableCPIDs := contentProviderResourceIDs(resources)
	if len(accountCPIDs) != 1 || accountCPIDs[0] != "555666777" {
		t.Fatalf("accountCPIDs=%v", accountCPIDs)
	}
	if len(availableCPIDs) != 0 || len(intersectStrings(accountCPIDs, availableCPIDs)) != 0 {
		t.Fatalf("available=%v", availableCPIDs)
	}
	if len(accountCPIDs) == 0 {
		t.Fatal("an assigned ad-account delegation must remain ready even when advertiser-resources has no unassigned rows")
	}
}

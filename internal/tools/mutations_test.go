package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

func TestFailedPreviewOmitsInvalidEmptyPreview(t *testing.T) {
	_, output, err := failedPreview(errors.New("invalid keyword"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if output.Preview != nil || !json.Valid(data) {
		t.Fatalf("output=%+v json=%s", output, data)
	}
}

func TestTypedCampaignAndCreativeValidation(t *testing.T) {
	campaign := map[string]any{
		"name": "Fixture", "billingEvent": "TAPS", "promotedObjectType": "APPSTORE_APP", "promotedObjectId": "1234567890", "status": "PAUSED",
		"dailyBudget": map[string]any{"value": map[string]any{"amount": "10.00", "currency": "USD"}},
		"targeting": map[string]any{
			"supplySource":    map[string]any{"include": []any{"APPSTORE"}},
			"supplyPlacement": map[string]any{"include": []any{"APPSTORE_SEARCH_RESULTS"}},
			"countryOrRegion": map[string]any{"include": []any{"US"}},
		},
	}
	if err := validateCampaignPayload(campaign, true); err != nil {
		t.Fatal(err)
	}
	campaign["targeting"].(map[string]any)["supplyPlacement"] = map[string]any{"include": []any{"MAPS_SEARCH_RESULTS"}}
	if err := validateCampaignPayload(campaign, true); err == nil {
		t.Fatal("expected Maps placement rejection")
	}

	creative, err := typedPayloadMap(CreativeCreatePayload{
		Name: "Default Product Page", CreativeType: "DEFAULT_PRODUCT_PAGE",
		Destination: Destination{DestinationType: "APP_STORE_PRODUCT_PAGE", Parameters: &DestinationParameters{AdamID: "1234567890"}},
	})
	if err != nil || validateCreativePayload(creative, true) != nil {
		t.Fatalf("creative=%#v err=%v", creative, err)
	}
	creative["destination"].(map[string]any)["parameters"].(map[string]any)["productPageId"] = "unexpected"
	if err := validateCreativePayload(creative, true); err == nil {
		t.Fatal("expected DPP productPageId rejection")
	}
}

func TestTypedPayloadRejectsAppleInvalidNameSeparator(t *testing.T) {
	payload := map[string]any{
		"name": "Validation | Search Results", "billingEvent": "TAPS", "promotedObjectType": "APPSTORE_APP", "promotedObjectId": "1234567890",
		"dailyBudget": map[string]any{"value": map[string]any{"amount": "10.00", "currency": "USD"}},
		"targeting": map[string]any{
			"supplyPlacement": map[string]any{"include": []any{"APPSTORE_SEARCH_RESULTS"}},
			"countryOrRegion": map[string]any{"include": []any{"US"}},
		},
	}
	if err := validateTypedResourcePayload("campaigns", true, payload); err == nil || !strings.Contains(err.Error(), "vertical-bar") {
		t.Fatalf("err=%v", err)
	}
}

func TestExplicitAdsPlacementMatrix(t *testing.T) {
	if explicitAdsSupported("APPSTORE_SEARCH_RESULTS") {
		t.Fatal("Apple rejects explicit ad resources for Search Results campaigns")
	}
	for _, placement := range []string{"APPSTORE_SEARCH_TAB", "APPSTORE_TODAY_TAB", "APPSTORE_PRODUCT_PAGES"} {
		if !explicitAdsSupported(placement) {
			t.Fatalf("%s must remain eligible for Apple validation", placement)
		}
	}
}

func TestBulkScopeQueryBindsWholeInventory(t *testing.T) {
	request := scopeQuery("adGroupId", "123")
	filters, ok := request["filters"].([]any)
	if !ok || len(filters) != 1 {
		t.Fatalf("filters=%#v", request["filters"])
	}
	filter, ok := filters[0].(map[string]any)
	if !ok || filter["field"] != "adGroupId" || fmt.Sprint(filter["value"]) != "123" {
		t.Fatalf("filter=%#v", filters[0])
	}
	campaignRequest := scopeQuery("campaignId", "456")
	campaignFilters := campaignRequest["filters"].([]any)
	if len(campaignFilters) != 2 || campaignFilters[1].(map[string]any)["operator"] != "IS_NULL" {
		t.Fatalf("campaign filters=%#v", campaignFilters)
	}
}

func TestAdGroupCreateRequiresStartTime(t *testing.T) {
	payload, err := typedPayloadMap(AdGroupCreatePayload{Name: "Fixture", CampaignID: "123", PricingModel: "CPT", Status: "PAUSED"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTypedResourcePayload("adgroups", true, payload); err == nil || !strings.Contains(err.Error(), "startTime") {
		t.Fatalf("err=%v", err)
	}
}

func TestBulkAndRecommendationSafetyValidation(t *testing.T) {
	seen := map[string]struct{}{}
	if err := validateCorrelationID("1", seen); err != nil {
		t.Fatal(err)
	}
	if err := validateCorrelationID("1", seen); err == nil {
		t.Fatal("expected duplicate correlationId rejection")
	}
	if err := validateCorrelationID("opaque", map[string]struct{}{}); err == nil {
		t.Fatal("expected non-numeric correlationId rejection")
	}
	if err := validateBulkCount(101); err == nil {
		t.Fatal("expected bulk count rejection")
	}
	left := appleads.Money{Amount: "10.01", Currency: "USD"}
	right := appleads.Money{Amount: "10.00", Currency: "USD"}
	if compared, err := compareMoney(left, right); err != nil || compared <= 0 {
		t.Fatalf("compared=%d err=%v", compared, err)
	}
	if _, err := compareMoney(left, appleads.Money{Amount: "10.00", Currency: "EUR"}); err == nil {
		t.Fatal("expected currency mismatch rejection")
	}
}

func TestCreateVerificationQueryUsesExplicitParent(t *testing.T) {
	request := createVerificationQuery("keywords", map[string]any{"adGroupId": "123"})
	filters, ok := request["filters"].([]any)
	if !ok || len(filters) != 1 {
		t.Fatalf("request=%#v", request)
	}
	filter, ok := filters[0].(map[string]any)
	if !ok || filter["field"] != "adGroupId" || fmt.Sprint(filter["value"]) != "123" {
		t.Fatalf("filter=%#v", filters[0])
	}
}

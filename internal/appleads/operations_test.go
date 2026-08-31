package appleads

import (
	"net/http"
	"testing"
)

func TestV02OperationPathsAndScopes(t *testing.T) {
	account, err := AdAccount("123456789")
	if err != nil || account.Path() != "ad-accounts/123456789" || !account.RequiresAccount() || account.IsMutation() {
		t.Fatalf("account=%+v err=%v", account, err)
	}
	resources := AdvertiserResources()
	if resources.Path() != "advertiser-resources" || resources.RequiresAccount() || resources.query.Get("resourceType") != "CONTENT_PROVIDER" {
		t.Fatalf("resources=%+v", resources)
	}
	geo, err := AppStoreGeoSearch(GeoSearchParams{Query: "San", Entity: "Locality", CountryCode: "US", PageSize: 200})
	if err != nil || geo.Path() != "search/geo" || !geo.RequiresAccount() || geo.query.Get("supplySource") != "APPSTORE" {
		t.Fatalf("geo=%+v err=%v", geo, err)
	}
	bulk, err := BulkResource("keywords", "create", map[string]any{"items": []any{}})
	if err != nil || bulk.Path() != "keywords/bulk-create" || !bulk.IsMutation() {
		t.Fatalf("bulk=%+v err=%v", bulk, err)
	}
	recommendation, err := RecommendationAction("target-cpas", "dismiss", []any{})
	if err != nil || recommendation.Path() != "recommendations/target-cpas/dismiss" || !recommendation.IsMutation() {
		t.Fatalf("recommendation=%+v err=%v", recommendation, err)
	}
}

func TestV02OperationValidation(t *testing.T) {
	for _, params := range []GeoSearchParams{
		{Query: "x", PageSize: 20},
		{Query: "valid", Entity: "PostalCode", PageSize: 20},
		{Query: "valid", CountryCode: "USA", PageSize: 20},
		{Query: "valid", PageSize: 201},
	} {
		if _, err := AppStoreGeoSearch(params); err == nil {
			t.Fatalf("expected geo rejection for %+v", params)
		}
	}
	if _, err := BulkResource("campaigns", "create", nil); err == nil {
		t.Fatal("campaign bulk must not be exposed")
	}
	if _, err := RecommendationAction("daily-budgets", "apply-all", nil); err == nil {
		t.Fatal("apply-all must not be exposed")
	}
}

func TestV03DeleteIsTypedMutationWithoutRetry(t *testing.T) {
	operation, err := ResourceDelete("campaigns", "123")
	if err != nil || operation.Path() != "campaigns/123" || operation.Method() != http.MethodDelete || !operation.IsMutation() || operation.retryReads {
		t.Fatalf("operation=%+v err=%v", operation, err)
	}
	if _, err := ResourceDelete("accounts", "123"); err == nil {
		t.Fatal("account delete must remain unavailable")
	}
}

func TestOperationEncodedBodySize(t *testing.T) {
	operation, err := ResourceCreate("shared-budgets", map[string]any{"invoiceDetail": map[string]any{"billingEmail": "private@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	size, err := operation.EncodedBodySize()
	if err != nil {
		t.Fatal(err)
	}
	if size < len("private@example.com") {
		t.Fatalf("encoded body size=%d", size)
	}
}

func TestVerificationScopeKeyBindsCanonicalRequest(t *testing.T) {
	first, _ := ResourceQuery("keywords", map[string]any{
		"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 10}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	second, _ := ResourceQuery("keywords", map[string]any{
		"pagination": map[string]any{"pageSize": 200, "offset": 0},
		"filters":    []any{map[string]any{"value": 10, "operator": "EQUALS", "field": "adGroupId"}},
	})
	different, _ := ResourceQuery("keywords", map[string]any{
		"filters":    []any{map[string]any{"field": "adGroupId", "operator": "EQUALS", "value": 20}},
		"pagination": map[string]any{"offset": 0, "pageSize": 200},
	})
	firstKey, err := first.VerificationScopeKey()
	if err != nil {
		t.Fatal(err)
	}
	secondKey, _ := second.VerificationScopeKey()
	differentKey, _ := different.VerificationScopeKey()
	if firstKey != secondKey || firstKey == differentKey {
		t.Fatalf("first=%s second=%s different=%s", firstKey, secondKey, differentKey)
	}
}

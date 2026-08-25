package tools

import (
	"strings"
	"testing"
)

func TestToolCatalogIsUniqueAndClassified(t *testing.T) {
	seen := make(map[string]struct{})
	all := append(ReadSpecs(), MutationSpecs()...)
	for _, item := range all {
		if item.Name == "raw_request" {
			t.Fatal("raw_request must not exist")
		}
		if strings.Contains(item.Name, "delete") || strings.Contains(item.Name, "shared_budget_create") || strings.Contains(item.Name, "shared_budget_update") || strings.Contains(item.Name, "ad_account_update") {
			t.Fatalf("out-of-scope mutation tool exposed: %s", item.Name)
		}
		if item.Name == "" || item.Description == "" || item.Class == "" {
			t.Fatalf("incomplete tool spec: %+v", item)
		}
		if _, ok := seen[item.Name]; ok {
			t.Fatalf("duplicate tool name %q", item.Name)
		}
		seen[item.Name] = struct{}{}
		switch item.Class {
		case "read", "mutation_preview", "mutation":
		default:
			t.Fatalf("tool %q has unknown class %q", item.Name, item.Class)
		}
	}
	if len(all) < 85 {
		t.Fatalf("unexpectedly small tool catalog: %d", len(all))
	}
}

func TestBoundedResponse(t *testing.T) {
	items := make([]any, MaxItems+50)
	bounded, truncated := boundData(map[string]any{"items": items})
	if !truncated {
		t.Fatal("expected truncation")
	}
	if got := len(bounded.(map[string]any)["items"].([]any)); got != MaxItems {
		t.Fatalf("bounded size=%d", got)
	}
}

func TestAccountWriteRoles(t *testing.T) {
	data := map[string]any{"acls": []any{
		map[string]any{"adAccount": map[string]any{"id": "123"}, "roles": []any{"API Account Read Only"}},
		map[string]any{"adAccount": map[string]any{"id": "456"}, "roles": []any{"API Campaign Manager"}},
	}}
	roles, found := accountRoles(data, "456")
	if !found || len(roles) != 1 || roles[0] != "API Campaign Manager" {
		t.Fatalf("roles=%v found=%v", roles, found)
	}
	if !isWriteRole(roles[0]) || isWriteRole("API Account Read Only") {
		t.Fatalf("unexpected write-role classification: %v", roles)
	}
	if _, found := accountRoles(data, "789"); found {
		t.Fatal("unexpected account match")
	}
}

func TestOwnedAppMatchUsesStringIdentifiers(t *testing.T) {
	data := []any{map[string]any{"adamId": "7654321098", "name": "Example App"}}
	if !containsAdamID(data, "7654321098") || containsAdamID(data, "123") {
		t.Fatal("owned app matching failed")
	}
}

func TestAppleMapsPayloadsAreRejectedByGuard(t *testing.T) {
	for _, payload := range []map[string]any{
		{"supplySource": "MAPS"},
		{"creativeType": "LOCAL_ADS_SEARCH_CREATIVE"},
		{"targeting": map[string]any{"businessBrandId": "brand"}},
	} {
		if !containsMapsValue(payload) && payload["creativeType"] != "LOCAL_ADS_SEARCH_CREATIVE" {
			t.Fatalf("Maps payload was not detected: %v", payload)
		}
	}
	if containsMapsValue(map[string]any{"promotedObjectType": "APPSTORE_APP"}) {
		t.Fatal("App Store payload was incorrectly rejected")
	}
	if stringField(map[string]any{}, "campaignId") != "" {
		t.Fatal("missing ID must remain empty")
	}
}

func TestMutationPayloadBounds(t *testing.T) {
	if err := validateMutationPayload(map[string]any{"name": strings.Repeat("x", 1<<20)}); err == nil {
		t.Fatal("expected payload size rejection")
	}
	value := any("leaf")
	for range 22 {
		value = map[string]any{"nested": value}
	}
	if err := validateMutationPayload(map[string]any{"root": value}); err == nil {
		t.Fatal("expected payload depth rejection")
	}
}

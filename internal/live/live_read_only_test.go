//go:build live_read_only

package live

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
)

func TestReadOnlyAcceptance(t *testing.T) {
	if os.Getenv("APPLE_ADS_LIVE_READ_ONLY") != "1" {
		t.Skip("live read-only acceptance is opt-in")
	}
	cfg, source, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	profile := os.Getenv("APPLE_ADS_PROFILE")
	account := os.Getenv("APPLE_ADS_AD_ACCOUNT_ID")
	query := os.Getenv("APPLE_ADS_LIVE_APP_QUERY")
	if query == "" {
		t.Fatal("APPLE_ADS_LIVE_APP_QUERY is required for live acceptance")
	}
	storefront := os.Getenv("APPLE_ADS_LIVE_STOREFRONT")
	if storefront == "" {
		storefront = "US"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	manager := appleads.NewManager(cfg, source)
	search, err := appleads.SearchApps(appleads.SearchAppsParams{Query: query, ReturnOwnedApps: true, Storefronts: []string{storefront}, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := manager.Do(ctx, profile, account, search)
	if err != nil {
		t.Fatalf("owned app search: %v", err)
	}
	adamID := os.Getenv("APPLE_ADS_LIVE_ADAM_ID")
	if adamID == "" {
		t.Fatal("APPLE_ADS_LIVE_ADAM_ID is required for live acceptance")
	}
	if !containsFieldValue(searchResult.Data, "adamId", adamID) {
		t.Fatalf("owned app search did not return Adam ID %s", adamID)
	}
	adamIDNumber, err := strconv.ParseInt(adamID, 10, 64)
	if err != nil {
		t.Fatalf("APPLE_ADS_LIVE_ADAM_ID must be a decimal int64: %v", err)
	}
	filters := []any{
		map[string]any{"field": "promotedObjectId", "operator": "EQUALS", "value": []string{adamID}},
		map[string]any{"field": "promotedObjectType", "operator": "EQUALS", "value": []string{"APPSTORE_APP"}},
		map[string]any{"field": "countriesOrRegions", "operator": "IN", "value": []string{storefront}},
	}
	body := map[string]any{"filters": filters, "pagination": map[string]any{"offset": 0, "pageSize": 50}}
	eligibility := appleads.AppsEligibility(map[string]any{"filters": []any{
		map[string]any{"field": "adamId", "operator": "EQUALS", "value": adamIDNumber},
		map[string]any{"field": "countryOrRegion", "operator": "IN", "value": []string{storefront}},
	}, "pagination": map[string]any{"offset": 0, "pageSize": 50}})
	eligibilityResult, err := manager.Do(ctx, profile, account, eligibility)
	if err != nil {
		t.Fatalf("app eligibility: %v", err)
	}
	if !hasNonEmptyArray(eligibilityResult.Data) {
		t.Fatal("app eligibility returned no rows")
	}
	suggestions, _ := appleads.Suggestion("keywords", body)
	suggestionResult, err := manager.Do(ctx, profile, account, suggestions)
	if err != nil {
		t.Fatalf("keyword suggestions: %v", err)
	}
	if !hasNonEmptyArray(suggestionResult.Data) {
		t.Fatal("keyword suggestions returned no opportunities")
	}
}

func containsFieldValue(value any, field, expected string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if current, ok := typed[field]; ok && fmt.Sprint(current) == expected {
			return true
		}
		for _, item := range typed {
			if containsFieldValue(item, field, expected) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsFieldValue(item, field, expected) {
				return true
			}
		}
	}
	return false
}

func hasNonEmptyArray(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) > 0
	case map[string]any:
		for _, item := range typed {
			if hasNonEmptyArray(item) {
				return true
			}
		}
	}
	return false
}

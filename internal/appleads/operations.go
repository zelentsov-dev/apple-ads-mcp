package appleads

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Operation struct {
	method     string
	path       string
	query      url.Values
	body       any
	mutation   bool
	retryReads bool
	scoped     bool
}

func (o Operation) Method() string        { return o.method }
func (o Operation) Path() string          { return o.path }
func (o Operation) IsMutation() bool      { return o.mutation }
func (o Operation) RequiresAccount() bool { return o.scoped }

type SearchAppsParams struct {
	Query           string
	ReturnOwnedApps bool
	CPIDs           []string
	Storefronts     []string
	Offset          int
	Limit           int
}

func Me() Operation   { return unscopedRead(http.MethodGet, "me", nil, nil) }
func ACLs() Operation { return unscopedRead(http.MethodGet, "acls", nil, nil) }

func Org(id string) (Operation, error) {
	path, err := resourcePath("orgs", id)
	if err != nil {
		return Operation{}, err
	}
	return unscopedRead(http.MethodGet, path, nil, nil), nil
}

func SearchApps(params SearchAppsParams) (Operation, error) {
	if params.Limit < 1 || params.Limit > 200 {
		return Operation{}, errors.New("limit must be between 1 and 200")
	}
	if params.Offset < 0 {
		return Operation{}, errors.New("offset must be non-negative")
	}
	if strings.TrimSpace(params.Query) == "" && len(params.CPIDs) == 0 && !params.ReturnOwnedApps {
		return Operation{}, errors.New("query, cpids, or returnOwnedApps=true is required")
	}
	if len(params.Query) > 256 {
		return Operation{}, errors.New("query must not exceed 256 characters")
	}
	if err := validateQueryValues("cpids", params.CPIDs, 50, 64); err != nil {
		return Operation{}, err
	}
	if err := validateQueryValues("storefronts", params.Storefronts, 50, 8); err != nil {
		return Operation{}, err
	}
	query := url.Values{
		"returnOwnedApps": {strconv.FormatBool(params.ReturnOwnedApps)},
		"offset":          {strconv.Itoa(params.Offset)},
		"limit":           {strconv.Itoa(params.Limit)},
	}
	if value := strings.TrimSpace(params.Query); value != "" {
		query.Set("query", value)
	}
	if len(params.CPIDs) > 0 {
		query.Set("cpids", strings.Join(params.CPIDs, ","))
	}
	for _, storefront := range params.Storefronts {
		if value := strings.TrimSpace(storefront); value != "" {
			query.Add("storeFronts", value)
		}
	}
	return read(http.MethodGet, "search/apps", query, nil), nil
}

func validateQueryValues(name string, values []string, maxCount, maxLength int) error {
	if len(values) > maxCount {
		return fmt.Errorf("%s must contain at most %d values", name, maxCount)
	}
	for _, value := range values {
		if len(strings.TrimSpace(value)) > maxLength {
			return fmt.Errorf("%s values must not exceed %d characters", name, maxLength)
		}
	}
	return nil
}

func App(adamID string) (Operation, error) {
	path, err := resourcePath("apps", adamID)
	if err != nil {
		return Operation{}, err
	}
	return read(http.MethodGet, path, nil, nil), nil
}

func AppsEligibility(body any) Operation {
	return read(http.MethodPost, "eligibilities/apps/query", nil, body)
}

func Suggestion(kind string, body any) (Operation, error) {
	allowed := map[string]string{
		"categories":  "suggestions/categories/query",
		"keywords":    "suggestions/keywords/query",
		"phrases":     "suggestions/phrases/query",
		"target-cpas": "suggestions/target-cpas/query",
	}
	path, ok := allowed[kind]
	if !ok {
		return Operation{}, fmt.Errorf("unsupported suggestion kind %q", kind)
	}
	return read(http.MethodPost, path, nil, body), nil
}

func Insight(kind string, body any) (Operation, error) {
	allowed := map[string]string{
		"impression-share":       "insights/apps/impression-share/query",
		"search-term-popularity": "insights/apps/search-term-popularity/query",
	}
	path, ok := allowed[kind]
	if !ok {
		return Operation{}, fmt.Errorf("unsupported insight kind %q", kind)
	}
	return read(http.MethodPost, path, nil, body), nil
}

func Report(kind string, body any) (Operation, error) {
	allowed := map[string]string{
		"campaigns":   "reports/apps/campaigns/query",
		"adgroups":    "reports/apps/adgroups/query",
		"ads":         "reports/apps/ads/query",
		"keywords":    "reports/apps/keywords/query",
		"searchterms": "reports/apps/searchterms/query",
	}
	path, ok := allowed[kind]
	if !ok {
		return Operation{}, fmt.Errorf("unsupported report kind %q", kind)
	}
	return read(http.MethodPost, path, nil, body), nil
}

func Recommendation(kind string, body any) (Operation, error) {
	allowed := map[string]string{
		"daily-budgets": "recommendations/daily-budgets/query",
		"target-cpas":   "recommendations/target-cpas/query",
	}
	path, ok := allowed[kind]
	if !ok {
		return Operation{}, fmt.Errorf("unsupported recommendation kind %q", kind)
	}
	return read(http.MethodPost, path, nil, body), nil
}

func ChangeHistory(body any) Operation {
	return read(http.MethodPost, "change-history/query", nil, body)
}

func ProductPage(id string) (Operation, error) {
	path, err := resourcePath("product-pages", id)
	if err != nil {
		return Operation{}, err
	}
	return read(http.MethodGet, path, nil, nil), nil
}

func ProductPagesQuery(body any) Operation {
	return read(http.MethodPost, "product-pages/query", nil, body)
}

func ProductPageLocales(body any) Operation {
	return read(http.MethodPost, "product-pages/locale-details/query", nil, body)
}

func ResourceGet(resource, id string) (Operation, error) {
	if !allowedResource(resource) {
		return Operation{}, fmt.Errorf("unsupported resource %q", resource)
	}
	path, err := resourcePath(resource, id)
	if err != nil {
		return Operation{}, err
	}
	return read(http.MethodGet, path, nil, nil), nil
}

func ResourceQuery(resource string, body any) (Operation, error) {
	if !allowedResource(resource) {
		return Operation{}, fmt.Errorf("unsupported resource %q", resource)
	}
	return read(http.MethodPost, resource+"/query", nil, body), nil
}

func ResourceCreate(resource string, body any) (Operation, error) {
	if !allowedResource(resource) {
		return Operation{}, fmt.Errorf("unsupported resource %q", resource)
	}
	cloned, err := cloneJSONValue(body)
	if err != nil {
		return Operation{}, err
	}
	return write(http.MethodPost, resource, cloned), nil
}

func ResourceUpdate(resource, id string, body any) (Operation, error) {
	if !allowedResource(resource) {
		return Operation{}, fmt.Errorf("unsupported resource %q", resource)
	}
	path, err := resourcePath(resource, id)
	if err != nil {
		return Operation{}, err
	}
	cloned, err := cloneJSONValue(body)
	if err != nil {
		return Operation{}, err
	}
	return write(http.MethodPut, path, cloned), nil
}

func allowedResource(resource string) bool {
	switch resource {
	case "campaigns", "adgroups", "keywords", "negative-keywords", "ads", "creatives", "shared-budgets":
		return true
	default:
		return false
	}
}

func resourcePath(resource, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/?#") {
		return "", errors.New("resource ID must be a non-empty path segment")
	}
	return resource + "/" + url.PathEscape(id), nil
}

func read(method, path string, query url.Values, body any) Operation {
	return Operation{method: method, path: path, query: query, body: body, retryReads: true, scoped: true}
}

func write(method, path string, body any) Operation {
	return Operation{method: method, path: path, body: body, mutation: true, scoped: true}
}

func unscopedRead(method, path string, query url.Values, body any) Operation {
	return Operation{method: method, path: path, query: query, body: body, retryReads: true}
}

func cloneJSONValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode operation payload: %w", err)
	}
	var cloned any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&cloned); err != nil {
		return nil, fmt.Errorf("clone operation payload: %w", err)
	}
	return normalizeMutationIDs(cloned, ""), nil
}

func normalizeMutationIDs(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			typed[childKey] = normalizeMutationIDs(child, childKey)
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = normalizeMutationIDs(typed[i], key)
		}
		return typed
	case string:
		if numericMutationIDKey(key) {
			if _, err := strconv.ParseInt(typed, 10, 64); err == nil {
				return json.Number(typed)
			}
		}
	}
	return value
}

func numericMutationIDKey(key string) bool {
	switch key {
	case "id", "adamId", "campaignId", "adGroupId", "keywordId", "negativeKeywordId", "sharedBudgetId", "creativeId", "adId", "orgId", "adAccountId", "parentOrgId":
		return true
	default:
		return false
	}
}

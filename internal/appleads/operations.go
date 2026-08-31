package appleads

import (
	"crypto/sha256"
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

func (o Operation) VerificationScopeKey() (string, error) {
	body, err := json.Marshal(o.body)
	if err != nil {
		return "", fmt.Errorf("encode verification scope: %w", err)
	}
	value := o.method + "\x00" + o.path + "\x00" + o.query.Encode() + "\x00" + string(body)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value))), nil
}

func (o Operation) EncodedBodySize() (int, error) {
	data, err := json.Marshal(o.body)
	if err != nil {
		return 0, fmt.Errorf("encode operation body: %w", err)
	}
	return len(data), nil
}

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

func AdAccount(id string) (Operation, error) {
	path, err := resourcePath("ad-accounts", id)
	if err != nil {
		return Operation{}, err
	}
	return read(http.MethodGet, path, nil, nil), nil
}

func AdvertiserResources() Operation {
	return unscopedRead(http.MethodGet, "advertiser-resources", url.Values{"resourceType": {"CONTENT_PROVIDER"}}, nil)
}

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

func AppLocaleDetails(adamID string, body any) (Operation, error) {
	path, err := resourcePath("apps", adamID)
	if err != nil {
		return Operation{}, err
	}
	return read(http.MethodPost, path+"/locale-details/query", nil, body), nil
}

func SupportedAppLanguages(body any) Operation {
	return read(http.MethodPost, "metadata/apps/supported-languages/query", nil, body)
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

func ChangeHistoryDetail(detailID string, offset, limit int) (Operation, error) {
	path, err := resourcePath("change-history", detailID)
	if err != nil {
		return Operation{}, err
	}
	if offset < 0 {
		return Operation{}, errors.New("offset must be non-negative")
	}
	if limit < 1 || limit > 200 {
		return Operation{}, errors.New("limit must be between 1 and 200")
	}
	query := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}
	return read(http.MethodGet, path, query, nil), nil
}

func CampaignStatusReasonDetails(campaignID string) (Operation, error) {
	path, err := resourcePath("campaigns", campaignID)
	if err != nil {
		return Operation{}, err
	}
	return read(http.MethodGet, path+"/legacy-app-limited-status-reason-details", nil, nil), nil
}

func RejectionReasonsQuery(body any) Operation {
	return read(http.MethodPost, "rejection-reasons/apps/query", nil, body)
}

func RejectionReason(id string) (Operation, error) {
	path, err := resourcePath("rejection-reasons/apps", id)
	if err != nil {
		return Operation{}, err
	}
	return read(http.MethodGet, path, nil, nil), nil
}

type GeoSearchParams struct {
	Query       string
	Entity      string
	CountryCode string
	Eligible    bool
	Offset      int
	PageSize    int
}

func AppStoreGeoSearch(params GeoSearchParams) (Operation, error) {
	queryText := strings.TrimSpace(params.Query)
	if queryText == "" {
		queryText = "*"
	}
	if queryText != "*" && len([]rune(queryText)) < 2 {
		return Operation{}, errors.New("geo query must contain at least two characters or be *")
	}
	if params.Offset < 0 {
		return Operation{}, errors.New("offset must be non-negative")
	}
	if params.PageSize == 0 {
		params.PageSize = 20
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		return Operation{}, errors.New("pageSize must be between 1 and 200")
	}
	entity := strings.TrimSpace(params.Entity)
	if entity != "" && entity != "Country" && entity != "AdminArea" && entity != "Locality" {
		return Operation{}, errors.New("entity must be Country, AdminArea, or Locality")
	}
	country := strings.ToUpper(strings.TrimSpace(params.CountryCode))
	if country != "" && (len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z') {
		return Operation{}, errors.New("countryCode must be an ISO 3166-1 alpha-2 code")
	}
	query := url.Values{
		"supplySource": {"APPSTORE"},
		"query":        {queryText},
		"offset":       {strconv.Itoa(params.Offset)},
		"pageSize":     {strconv.Itoa(params.PageSize)},
		"eligible":     {strconv.FormatBool(params.Eligible)},
	}
	if entity != "" {
		query.Set("entity", entity)
	}
	if country != "" {
		query.Set("countrycode", country)
	}
	return read(http.MethodGet, "search/geo", query, nil), nil
}

func RecommendationAction(kind, action string, body any) (Operation, error) {
	if kind != "daily-budgets" && kind != "target-cpas" {
		return Operation{}, fmt.Errorf("unsupported recommendation kind %q", kind)
	}
	if action != "apply" && action != "dismiss" {
		return Operation{}, fmt.Errorf("unsupported recommendation action %q", action)
	}
	cloned, err := cloneJSONValue(body)
	if err != nil {
		return Operation{}, err
	}
	return write(http.MethodPost, "recommendations/"+kind+"/"+action, cloned), nil
}

func BulkResource(resource, action string, body any) (Operation, error) {
	if resource != "keywords" && resource != "negative-keywords" {
		return Operation{}, fmt.Errorf("unsupported bulk resource %q", resource)
	}
	if action != "create" && action != "update" {
		return Operation{}, fmt.Errorf("unsupported bulk action %q", action)
	}
	cloned, err := cloneJSONValue(body)
	if err != nil {
		return Operation{}, err
	}
	return write(http.MethodPost, resource+"/bulk-"+action, cloned), nil
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

func ResourceDelete(resource, id string) (Operation, error) {
	if !allowedResource(resource) {
		return Operation{}, fmt.Errorf("unsupported resource %q", resource)
	}
	path, err := resourcePath(resource, id)
	if err != nil {
		return Operation{}, err
	}
	return write(http.MethodDelete, path, nil), nil
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
	case "id", "adamId", "campaignId", "adGroupId", "keywordId", "negativeKeywordId", "sharedBudgetId", "creativeId", "adId", "orgId", "adAccountId", "parentOrgId", "correlationId":
		return true
	default:
		return false
	}
}

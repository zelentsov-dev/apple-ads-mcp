package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

const MaxItems = 200

type NoInput struct{}

type ProfileInput struct {
	Profile string `json:"profile" jsonschema:"local Apple Ads profile name"`
}

type AccountInput struct {
	Profile     string `json:"profile" jsonschema:"local Apple Ads profile name"`
	AdAccountID string `json:"adAccountId" jsonschema:"Apple Ads ad account ID"`
}

type OrgInput struct {
	Profile string `json:"profile" jsonschema:"local Apple Ads profile name"`
	OrgID   string `json:"orgId" jsonschema:"Apple Ads organization ID"`
}

type AppsSearchInput struct {
	AccountInput
	Query           string   `json:"query,omitempty" jsonschema:"app name or Adam ID search text"`
	ReturnOwnedApps bool     `json:"returnOwnedApps" jsonschema:"limit results to apps owned by the organization"`
	CPIDs           []string `json:"cpids,omitempty" jsonschema:"optional iTunes content provider IDs"`
	Storefronts     []string `json:"storefronts,omitempty" jsonschema:"ISO storefront country codes"`
	Offset          int      `json:"offset,omitempty" jsonschema:"zero-based result offset"`
	Limit           int      `json:"limit,omitempty" jsonschema:"result limit from 1 to 200"`
}

type AppInput struct {
	AccountInput
	AdamID string `json:"adamId" jsonschema:"App Store Adam ID as a string"`
}

type ProductPageInput struct {
	AccountInput
	ProductPageID string `json:"productPageId" jsonschema:"Default or Custom Product Page ID"`
}

type ResourceInput struct {
	AccountInput
	ID string `json:"id" jsonschema:"resource ID as a string"`
}

type QueryInput struct {
	AccountInput
	Filters    []QueryFilterInput `json:"filters,omitempty" jsonschema:"endpoint-specific filter conditions"`
	Sorting    []QuerySortInput   `json:"sorting,omitempty" jsonschema:"ordered sort fields"`
	Pagination *PaginationInput   `json:"pagination,omitempty" jsonschema:"bounded result window"`
	Fields     []string           `json:"fields,omitempty" jsonschema:"report fields to return"`
	GroupBy    []string           `json:"groupBy,omitempty" jsonschema:"report dimensions"`
	TimeRange  *TimeRangeInput    `json:"timeRange,omitempty" jsonschema:"report or insight time range"`
	Options    *QueryOptionsInput `json:"options,omitempty" jsonschema:"report or insight options"`
}

type QueryFilterInput struct {
	Field      string `json:"field" jsonschema:"Apple filter field name"`
	Operator   string `json:"operator" jsonschema:"Apple filter operator such as EQUALS or IN"`
	Value      any    `json:"value" jsonschema:"scalar or array value accepted by the endpoint"`
	IgnoreCase *bool  `json:"ignoreCase,omitempty" jsonschema:"case-insensitive string comparison when supported"`
}

type QuerySortInput struct {
	Field string `json:"field" jsonschema:"Apple field name"`
	Order string `json:"order" jsonschema:"endpoint-specific order such as ASC, DESC, ASCENDING, or DESCENDING"`
}

type PaginationInput struct {
	Offset   int `json:"offset,omitempty" jsonschema:"zero-based offset"`
	PageSize int `json:"pageSize,omitempty" jsonschema:"page size from 1 to 200"`
}

type TimeRangeInput struct {
	Start       string `json:"start" jsonschema:"start date in YYYY-MM-DD"`
	End         string `json:"end" jsonschema:"end date in YYYY-MM-DD"`
	TimeZone    string `json:"timeZone,omitempty" jsonschema:"Apple timezone mode such as UTC or ORTZ"`
	Granularity string `json:"granularity,omitempty" jsonschema:"report granularity such as DAILY or WEEKLY"`
}

type QueryOptionsInput struct {
	IncludeRows               []string `json:"includeRows,omitempty" jsonschema:"optional report row totals"`
	ImpressionShareReportType string   `json:"impressionShareReportType,omitempty" jsonschema:"FIRST_SLOT or ALL_SLOTS"`
}

type AppOpportunityInput struct {
	AccountInput
	AdamID             string   `json:"adamId" jsonschema:"App Store Adam ID as a string"`
	CountriesOrRegions []string `json:"countriesOrRegions" jsonschema:"ISO country or region codes"`
	Terms              []string `json:"terms,omitempty" jsonschema:"optional seed terms"`
}

type Output struct {
	Summary    string               `json:"summary"`
	Data       any                  `json:"data,omitempty"`
	Pagination *appleads.Pagination `json:"pagination,omitempty"`
	RateLimit  *appleads.RateLimit  `json:"rateLimit,omitempty"`
	Error      *ErrorOutput         `json:"error,omitempty"`
}

type ErrorOutput struct {
	Type       string         `json:"type"`
	Message    string         `json:"message"`
	HTTPStatus int            `json:"httpStatus,omitempty"`
	Code       string         `json:"code,omitempty"`
	Retryable  bool           `json:"retryable,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

type Spec struct {
	Name        string
	Description string
	Class       string
}

func validateAccount(input AccountInput) error {
	profile := strings.TrimSpace(input.Profile)
	if profile == "" {
		return errors.New("profile is required")
	}
	if len(profile) > 128 || strings.ContainsAny(profile, "\r\n") {
		return errors.New("profile is invalid")
	}
	accountID := strings.TrimSpace(input.AdAccountID)
	if accountID == "" {
		return errors.New("adAccountId is required")
	}
	if len(accountID) > 64 || strings.ContainsAny(accountID, "\r\n") {
		return errors.New("adAccountId is invalid")
	}
	return nil
}

func validateTextValues(name string, values []string, maxCount, maxLength int) error {
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

func success(summary string, result appleads.Result) Output {
	data, truncated := boundData(result.Data)
	if truncated {
		summary += "; response arrays were capped at 200 items"
	}
	return Output{Summary: summary, Data: data, Pagination: &result.Pagination, RateLimit: &result.RateLimit}
}

func boundData(value any) (any, bool) {
	switch typed := value.(type) {
	case []any:
		truncated := len(typed) > MaxItems
		if truncated {
			typed = typed[:MaxItems]
		}
		result := make([]any, len(typed))
		for i, item := range typed {
			var nested bool
			result[i], nested = boundData(item)
			truncated = truncated || nested
		}
		return result, truncated
	case map[string]any:
		result := make(map[string]any, len(typed))
		truncated := false
		for key, item := range typed {
			var nested bool
			result[key], nested = boundData(item)
			truncated = truncated || nested
		}
		return result, truncated
	default:
		return value, false
	}
}

func errorOutput(err error) Output {
	output := ErrorOutput{Type: "request_error", Message: err.Error()}
	summary := "Apple Ads request failed"
	var apiError *appleads.APIError
	if errors.As(err, &apiError) {
		output.Type = "apple_api_error"
		output.Message = apiError.Message
		output.HTTPStatus = apiError.HTTPStatus
		output.Code = apiError.Code
		output.Retryable = apiError.Retryable
		output.Details = apiError.Details
		summary = fmt.Sprintf("Apple Ads API request failed with HTTP %d", apiError.HTTPStatus)
		if apiError.Code != "" {
			summary += " (" + apiError.Code + ")"
		}
	}
	return Output{Summary: summary, Error: &output}
}

func queryRequest(input map[string]any) map[string]any {
	request := make(map[string]any, len(input))
	for key, value := range input {
		request[key] = value
	}
	return request
}

func boundedQueryRequest(input map[string]any) (map[string]any, error) {
	request := queryRequest(input)
	pagination, ok := request["pagination"].(map[string]any)
	if !ok {
		pagination = map[string]any{}
		request["pagination"] = pagination
	}
	offset := stringInt(pagination["offset"])
	if offset < 0 {
		return nil, errors.New("pagination offset must be non-negative")
	}
	pageSize := stringInt(pagination["pageSize"])
	if pageSize == 0 {
		pageSize = stringInt(pagination["limit"])
	}
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > MaxItems {
		return nil, fmt.Errorf("pagination pageSize must be between 1 and %d", MaxItems)
	}
	delete(pagination, "limit")
	pagination["offset"] = offset
	pagination["pageSize"] = pageSize
	return request, nil
}

func (input QueryInput) boundedRequest() (map[string]any, error) {
	if len(input.Filters) > 50 {
		return nil, errors.New("at most 50 filters are allowed")
	}
	if len(input.Sorting) > 5 {
		return nil, errors.New("at most 5 sorting fields are allowed")
	}
	if len(input.Fields) > 50 || len(input.GroupBy) > 50 {
		return nil, errors.New("at most 50 fields and groupBy values are allowed")
	}
	request := make(map[string]any)
	if len(input.Filters) > 0 {
		filters, err := normalizeQueryFilters(input.Filters)
		if err != nil {
			return nil, err
		}
		request["filters"] = filters
	}
	if len(input.Sorting) > 0 {
		request["sorting"] = input.Sorting
	}
	if input.Pagination != nil {
		request["pagination"] = map[string]any{"offset": input.Pagination.Offset, "pageSize": input.Pagination.PageSize}
	}
	if len(input.Fields) > 0 {
		request["fields"] = input.Fields
	}
	if len(input.GroupBy) > 0 {
		request["groupBy"] = input.GroupBy
	}
	if input.TimeRange != nil {
		if _, err := time.Parse("2006-01-02", input.TimeRange.Start); err != nil {
			return nil, errors.New("timeRange.start must use YYYY-MM-DD")
		}
		if _, err := time.Parse("2006-01-02", input.TimeRange.End); err != nil {
			return nil, errors.New("timeRange.end must use YYYY-MM-DD")
		}
		request["timeRange"] = input.TimeRange
	}
	if input.Options != nil {
		request["options"] = input.Options
	}
	return boundedQueryRequest(request)
}

func normalizeQueryFilters(filters []QueryFilterInput) ([]QueryFilterInput, error) {
	result := make([]QueryFilterInput, len(filters))
	copy(result, filters)
	for i := range result {
		if result[i].Field != "adamId" {
			continue
		}
		value, err := numericAdamID(result[i].Value)
		if err != nil {
			return nil, fmt.Errorf("filters[%d].value: %w", i, err)
		}
		result[i].Value = value
	}
	return result, nil
}

func numericAdamID(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return decimalAdamID(typed)
	case []string:
		result := make([]any, len(typed))
		for i, item := range typed {
			converted, err := numericAdamID(item)
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			converted, err := numericAdamID(item)
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil
	case json.Number:
		return decimalAdamID(typed.String())
	case int:
		return signedAdamID(int64(typed))
	case int32:
		return signedAdamID(int64(typed))
	case int64:
		return signedAdamID(typed)
	case uint:
		return unsignedAdamID(uint64(typed))
	case uint32:
		return unsignedAdamID(uint64(typed))
	case uint64:
		return unsignedAdamID(typed)
	case float64:
		const maxExactJSONInteger = 1<<53 - 1
		if typed <= 0 || typed > maxExactJSONInteger || typed != float64(uint64(typed)) {
			return nil, errors.New("adamId must be a decimal identifier")
		}
		return json.Number(strconv.FormatUint(uint64(typed), 10)), nil
	default:
		return nil, errors.New("adamId must be a decimal identifier")
	}
}

func decimalAdamID(value string) (json.Number, error) {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return "", errors.New("adamId must be a decimal identifier")
	}
	return json.Number(value), nil
}

func signedAdamID(value int64) (json.Number, error) {
	if value <= 0 {
		return "", errors.New("adamId must be a decimal identifier")
	}
	return json.Number(strconv.FormatInt(value, 10)), nil
}

func unsignedAdamID(value uint64) (json.Number, error) {
	if value == 0 {
		return "", errors.New("adamId must be a decimal identifier")
	}
	return json.Number(strconv.FormatUint(value, 10)), nil
}

func stringInt(value any) int {
	var result int
	_, _ = fmt.Sscan(fmt.Sprint(value), &result)
	return result
}

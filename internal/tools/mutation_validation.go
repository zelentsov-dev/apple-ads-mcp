package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
)

var decimalIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

func typedPayloadMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode typed mutation payload: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode typed mutation payload: %w", err)
	}
	return payload, nil
}

func validateTypedResourcePayload(resource string, create bool, payload map[string]any) error {
	if len(payload) == 0 {
		return errors.New("payload must not be empty")
	}
	if name, exists := payload["name"]; exists {
		if err := validateResourceName(fmt.Sprint(name)); err != nil {
			return err
		}
	}
	switch resource {
	case "campaigns":
		return validateCampaignPayload(payload, create)
	case "adgroups":
		return validateAdGroupPayload(payload, create)
	case "keywords":
		return validateKeywordPayload(payload, create)
	case "negative-keywords":
		return validateNegativeKeywordPayload(payload, create)
	case "ads":
		return validateAdPayload(payload, create)
	case "creatives":
		return validateCreativePayload(payload, create)
	default:
		return fmt.Errorf("unsupported typed mutation resource %q", resource)
	}
}

func validateResourceName(name string) error {
	if strings.ContainsRune(name, '|') {
		return errors.New("resource name must not contain the vertical-bar character, which Apple Ads rejects")
	}
	for _, value := range name {
		if value < 0x20 || value == 0x7f {
			return errors.New("resource name must not contain control characters")
		}
	}
	return nil
}

func validateCampaignPayload(payload map[string]any, create bool) error {
	if create {
		if strings.TrimSpace(stringField(payload, "name")) == "" {
			return errors.New("campaign name is required")
		}
		if stringField(payload, "promotedObjectType") != "APPSTORE_APP" {
			return errors.New("campaign promotedObjectType must be APPSTORE_APP")
		}
		if !decimalIDPattern.MatchString(stringField(payload, "promotedObjectId")) {
			return errors.New("campaign promotedObjectId must be a decimal Adam ID string")
		}
		if strings.TrimSpace(stringField(payload, "billingEvent")) == "" {
			return errors.New("campaign billingEvent is required")
		}
		if err := validateMoneyValueField(payload, "dailyBudget"); err != nil {
			return err
		}
	}
	if status := stringField(payload, "status"); status != "" && !allowedStatus(status) {
		return fmt.Errorf("unsupported campaign status %q", status)
	}
	if _, exists := payload["dailyBudget"]; exists {
		if err := validateMoneyValueField(payload, "dailyBudget"); err != nil {
			return err
		}
	}
	if targeting, ok := payload["targeting"].(map[string]any); ok {
		if err := normalizeCampaignTargeting(targeting, create); err != nil {
			return err
		}
	} else if create {
		return errors.New("campaign targeting is required")
	}
	return validateSchedule(payload)
}

func normalizeCampaignTargeting(targeting map[string]any, create bool) error {
	source, _ := targeting["supplySource"].(map[string]any)
	if source == nil {
		source = map[string]any{"include": []any{"APPSTORE"}}
		targeting["supplySource"] = source
	}
	if err := validateTargetValues("supplySource", source, map[string]struct{}{"APPSTORE": {}}); err != nil {
		return err
	}
	placements := map[string]struct{}{
		"APPSTORE_SEARCH_RESULTS": {}, "APPSTORE_SEARCH_TAB": {},
		"APPSTORE_TODAY_TAB": {}, "APPSTORE_PRODUCT_PAGES": {},
	}
	if placement, ok := targeting["supplyPlacement"].(map[string]any); ok {
		if err := validateTargetValues("supplyPlacement", placement, placements); err != nil {
			return err
		}
		if len(stringSlice(placement["include"])) != 1 {
			return errors.New("campaign targeting requires exactly one App Store supplyPlacement")
		}
	} else if create {
		return errors.New("campaign targeting requires supplyPlacement")
	}
	if countries, ok := targeting["countryOrRegion"].(map[string]any); ok {
		include := stringSlice(countries["include"])
		if create && len(include) == 0 {
			return errors.New("campaign targeting requires at least one countryOrRegion")
		}
		if len(include) > 100 {
			return errors.New("campaign targeting supports at most 100 countriesOrRegions per operation")
		}
		for i, country := range include {
			country = strings.ToUpper(strings.TrimSpace(country))
			if !alpha2(country) {
				return fmt.Errorf("countryOrRegion %q is not an ISO alpha-2 code", country)
			}
			include[i] = country
		}
		countries["include"] = include
	} else if create {
		return errors.New("campaign targeting requires countryOrRegion")
	}
	return nil
}

func validateAdGroupPayload(payload map[string]any, create bool) error {
	if create {
		if strings.TrimSpace(stringField(payload, "name")) == "" {
			return errors.New("ad group name is required")
		}
		if !decimalIDPattern.MatchString(stringField(payload, "campaignId")) {
			return errors.New("ad group campaignId must be a decimal string")
		}
		if strings.TrimSpace(stringField(payload, "pricingModel")) == "" {
			return errors.New("ad group pricingModel is required")
		}
		if strings.TrimSpace(stringField(payload, "startTime")) == "" {
			return errors.New("ad group startTime is required by Apple Ads")
		}
	}
	if status := stringField(payload, "status"); status != "" && !allowedStatus(status) {
		return fmt.Errorf("unsupported ad group status %q", status)
	}
	if err := validateBidStrategy(payload["bidStrategy"]); err != nil {
		return err
	}
	if err := validateCPAGoal(payload["cpaCap"]); err != nil {
		return err
	}
	if targeting, ok := payload["targeting"].(map[string]any); ok {
		if err := validateAdGroupTargeting(targeting); err != nil {
			return err
		}
	}
	return validateSchedule(payload)
}

func validateKeywordPayload(payload map[string]any, create bool) error {
	if create {
		if !decimalIDPattern.MatchString(stringField(payload, "adGroupId")) {
			return errors.New("keyword adGroupId must be a decimal string")
		}
		if text := strings.TrimSpace(stringField(payload, "text")); text == "" || len([]rune(text)) > 80 {
			return errors.New("keyword text must contain 1 to 80 characters")
		}
		if !allowedMatchType(stringField(payload, "matchType")) {
			return errors.New("keyword matchType must be EXACT or BROAD")
		}
	}
	if status := stringField(payload, "status"); status != "" && !allowedStatus(status) {
		return fmt.Errorf("unsupported keyword status %q", status)
	}
	return validateOptionalMoneyFields(payload, "bid")
}

func validateNegativeKeywordPayload(payload map[string]any, create bool) error {
	if create {
		campaignID := stringField(payload, "campaignId")
		adGroupID := stringField(payload, "adGroupId")
		if (campaignID == "") == (adGroupID == "") {
			return errors.New("negative keyword requires exactly one of campaignId or adGroupId")
		}
		if campaignID != "" && !decimalIDPattern.MatchString(campaignID) || adGroupID != "" && !decimalIDPattern.MatchString(adGroupID) {
			return errors.New("negative keyword parent ID must be a decimal string")
		}
		if text := strings.TrimSpace(stringField(payload, "text")); text == "" || len([]rune(text)) > 80 {
			return errors.New("negative keyword text must contain 1 to 80 characters")
		}
		if !allowedMatchType(stringField(payload, "matchType")) {
			return errors.New("negative keyword matchType must be EXACT or BROAD")
		}
	}
	if status := stringField(payload, "status"); status != "" && !allowedStatus(status) {
		return fmt.Errorf("unsupported negative keyword status %q", status)
	}
	return nil
}

func validateAdPayload(payload map[string]any, create bool) error {
	if create {
		for _, field := range []string{"adGroupId", "creativeId"} {
			if !decimalIDPattern.MatchString(stringField(payload, field)) {
				return fmt.Errorf("ad %s must be a decimal string", field)
			}
		}
		if strings.TrimSpace(stringField(payload, "name")) == "" {
			return errors.New("ad name is required")
		}
	}
	if status := stringField(payload, "status"); status != "" && !allowedStatus(status) {
		return fmt.Errorf("unsupported ad status %q", status)
	}
	return nil
}

func validateCreativePayload(payload map[string]any, create bool) error {
	if create {
		if strings.TrimSpace(stringField(payload, "name")) == "" {
			return errors.New("creative name is required")
		}
		creativeType := stringField(payload, "creativeType")
		if creativeType != "DEFAULT_PRODUCT_PAGE" && creativeType != "CUSTOM_PRODUCT_PAGE" {
			return errors.New("creativeType must be DEFAULT_PRODUCT_PAGE or CUSTOM_PRODUCT_PAGE")
		}
		destination, ok := payload["destination"].(map[string]any)
		if !ok || stringField(destination, "destinationType") != "APP_STORE_PRODUCT_PAGE" {
			return errors.New("creative destinationType must be APP_STORE_PRODUCT_PAGE")
		}
		parameters, ok := destination["parameters"].(map[string]any)
		if !ok || !decimalIDPattern.MatchString(stringField(parameters, "adamId")) {
			return errors.New("creative destination parameters require a decimal adamId string")
		}
		productPageID := strings.TrimSpace(stringField(parameters, "productPageId"))
		if creativeType == "CUSTOM_PRODUCT_PAGE" && productPageID == "" {
			return errors.New("CUSTOM_PRODUCT_PAGE creative requires productPageId")
		}
		if creativeType == "DEFAULT_PRODUCT_PAGE" && productPageID != "" {
			return errors.New("DEFAULT_PRODUCT_PAGE creative must not include productPageId")
		}
	}
	return nil
}

func validateAdGroupTargeting(targeting map[string]any) error {
	allowed := map[string]struct{}{
		"country": {}, "adminArea": {}, "locality": {}, "deviceClass": {}, "minAge": {}, "maxAge": {},
		"gender": {}, "appCategory": {}, "appDownloader": {}, "daypart": {},
	}
	for field, value := range targeting {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("targeting field %q is outside App Store scope", field)
		}
		data, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("targeting field %q must contain include or exclude arrays", field)
		}
		if len(stringSlice(data["include"]))+len(stringSlice(data["exclude"])) > 200 {
			return fmt.Errorf("targeting field %q exceeds 200 values", field)
		}
	}
	return nil
}

func validateTargetValues(name string, data map[string]any, allowed map[string]struct{}) error {
	for _, field := range []string{"include", "exclude"} {
		for _, value := range stringSlice(data[field]) {
			if _, ok := allowed[value]; !ok {
				return fmt.Errorf("%s value %q is not supported", name, value)
			}
		}
	}
	return nil
}

func validateSchedule(payload map[string]any) error {
	start := stringField(payload, "startTime")
	end := stringField(payload, "endTime")
	if start == "" {
		return nil
	}
	startTime, err := parseISOTimestamp(start)
	if err != nil {
		return errors.New("startTime must use ISO 8601 with optional timezone")
	}
	if end == "" {
		return nil
	}
	endTime, err := parseISOTimestamp(end)
	if err != nil {
		return errors.New("endTime must use ISO 8601 with optional timezone")
	}
	if !endTime.After(startTime) {
		return errors.New("endTime must be after startTime")
	}
	return nil
}

func parseISOTimestamp(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000", "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid ISO 8601 timestamp")
}

func validateBidStrategy(value any) error {
	strategy, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	strategyType := strings.ToUpper(stringField(strategy, "bidStrategyType"))
	if strategyType != "" && strategyType != "MANUAL_CPT" && strategyType != "MAX_CONVERSIONS" {
		return fmt.Errorf("bidStrategyType %q is outside App Store Ads scope", strategyType)
	}
	if strategyType == "MAX_CONVERSIONS" {
		if _, exists := strategy["bid"]; exists {
			return errors.New("MAX_CONVERSIONS must not include a manual bid")
		}
	}
	return validateOptionalMoneyFields(strategy, "bid")
}

func validateCPAGoal(value any) error {
	goal, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return validateMoneyField(goal, "value", true)
}

func validateOptionalMoneyFields(payload map[string]any, fields ...string) error {
	for _, field := range fields {
		if _, ok := payload[field]; !ok {
			continue
		}
		if err := validateMoneyField(payload, field, true); err != nil {
			return err
		}
	}
	return nil
}

func validateMoneyField(payload map[string]any, field string, positive bool) error {
	raw, ok := payload[field].(map[string]any)
	if !ok {
		return fmt.Errorf("%s must contain amount and currency", field)
	}
	money := appleads.Money{Amount: stringField(raw, "amount"), Currency: strings.ToUpper(stringField(raw, "currency"))}
	var err error
	if positive {
		err = money.ValidatePositive()
	} else {
		err = money.Validate()
	}
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	raw["currency"] = money.Currency
	return nil
}

func validateMoneyValueField(payload map[string]any, field string) error {
	wrapper, ok := payload[field].(map[string]any)
	if !ok {
		return fmt.Errorf("%s must contain a value object", field)
	}
	if err := validateMoneyField(wrapper, "value", true); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func compareMoney(left, right appleads.Money) (int, error) {
	if !strings.EqualFold(left.Currency, right.Currency) {
		return 0, errors.New("money currencies do not match")
	}
	leftValue, ok := new(big.Rat).SetString(left.Amount)
	if !ok {
		return 0, errors.New("left money amount is invalid")
	}
	rightValue, ok := new(big.Rat).SetString(right.Amount)
	if !ok {
		return 0, errors.New("right money amount is invalid")
	}
	return leftValue.Cmp(rightValue), nil
}

func allowedStatus(value string) bool {
	return value == "PAUSED" || value == "ENABLED"
}

func allowedMatchType(value string) bool {
	return value == "EXACT" || value == "BROAD"
}

func alpha2(value string) bool {
	return len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, fmt.Sprint(item))
		}
		return result
	default:
		return nil
	}
}

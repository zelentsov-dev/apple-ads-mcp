package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
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
	if err := validateBidStrategyForOperation(payload["bidStrategy"], create, create); err != nil {
		return err
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
	if err := validateBidStrategyForOperation(payload["bidStrategy"], create, false); err != nil {
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
	return validateBidStrategyForOperation(value, false, false)
}

func validateBidStrategyForOperation(value any, create, required bool) error {
	if value == nil {
		if required {
			return errors.New("campaign creation requires bidStrategy")
		}
		return nil
	}
	strategy, ok := value.(map[string]any)
	if !ok || len(strategy) == 0 {
		return errors.New("bidStrategy must be a nonempty object")
	}
	strategyType := stringField(strategy, "bidStrategyType")
	goal := stringField(strategy, "bidStrategyGoal")
	_, hasType := strategy["bidStrategyType"]
	_, hasGoal := strategy["bidStrategyGoal"]
	if hasType || hasGoal || create {
		if strategyType != "MANUAL_CPT" && strategyType != "MAX_CONVERSIONS" {
			return errors.New("bidStrategyType must be MANUAL_CPT or MAX_CONVERSIONS")
		}
		expected := "TAP"
		if strategyType == "MAX_CONVERSIONS" {
			expected = "INSTALL"
		}
		if goal != expected {
			return fmt.Errorf("%s requires bidStrategyGoal %s; send type and goal together", strategyType, expected)
		}
	}
	_, hasBid := strategy["bid"]
	if create && strategyType == "MANUAL_CPT" && !hasBid {
		return errors.New("MANUAL_CPT creation requires a positive bid")
	}
	if strategyType == "" && !hasBid {
		return errors.New("bidStrategy update requires a type and goal pair or a bid")
	}
	if hasBid {
		if err := validateMoneyField(strategy, "bid", create || strategyType != "MAX_CONVERSIONS"); err != nil {
			return err
		}
		bid := strategy["bid"].(map[string]any)
		amount, ok := new(big.Rat).SetString(stringField(bid, "amount"))
		if !ok || amount.Sign() < 0 {
			return errors.New("bid must not be negative")
		}
		return nil
	}
	return nil
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

func (s *Service) bidStrategyEvidence(ctx context.Context, account AccountInput, resource, id string, payload map[string]any, create bool) ([]operations.VerificationRead, []operations.OperationWarning, error) {
	warnings := mutationWarnings(payload)
	if resource != "campaigns" && resource != "adgroups" {
		return nil, warnings, nil
	}
	reads := make([]operations.VerificationRead, 0, 3)
	readObject := func(name, kind, objectID string) (map[string]any, error) {
		op, err := appleads.ResourceGet(kind, objectID)
		if err != nil {
			return nil, err
		}
		result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, op)
		if err != nil {
			return nil, err
		}
		object, ok := result.Data.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s strategy evidence must be an object", kind)
		}
		reads = append(reads, operations.VerificationRead{Name: name, Operation: op, Expected: cloneObject(object), Scopes: []string{operations.ObjectMutationScope(objectID)}})
		return object, nil
	}
	currencies := collectFieldValues(payload, "currency", MaxItems)
	if len(currencies) > 0 {
		op, err := appleads.AdAccount(account.AdAccountID)
		if err != nil {
			return nil, nil, err
		}
		result, err := s.manager.Do(ctx, account.Profile, account.AdAccountID, op)
		if err != nil {
			return nil, nil, err
		}
		accountObject, ok := result.Data.(map[string]any)
		if !ok {
			return nil, nil, errors.New("account currency evidence must be an object")
		}
		currency := strings.ToUpper(stringField(accountObject, "currency"))
		if currency == "" {
			return nil, nil, errors.New("account currency is unavailable")
		}
		for _, value := range currencies {
			if !strings.EqualFold(value, currency) {
				return nil, nil, errors.New("bid and budget currencies must match the ad account")
			}
		}
		reads = append(reads, operations.VerificationRead{Name: "strategy_account", Operation: op, Expected: map[string]any{"currency": accountObject["currency"]}, Fields: []string{"currency"}})
	}
	strategy, hasStrategy := payload["bidStrategy"].(map[string]any)
	if resource == "campaigns" {
		_, hasTargeting := payload["targeting"]
		if !hasStrategy && !hasTargeting {
			return reads, warnings, nil
		}
		campaign := payload
		if !create {
			var err error
			campaign, err = readObject("strategy_campaign", "campaigns", id)
			if err != nil {
				return nil, nil, err
			}
		}
		proposedCampaign := cloneObject(campaign)
		if targeting, ok := payload["targeting"].(map[string]any); ok {
			proposedTargeting := cloneObject(findMapField(campaign, "targeting"))
			if proposedTargeting == nil {
				proposedTargeting = map[string]any{}
			}
			for key, value := range targeting {
				proposedTargeting[key] = value
			}
			proposedCampaign["targeting"] = proposedTargeting
		}
		if err := validateCampaignStrategyContext(proposedCampaign, strategy); err != nil {
			return nil, nil, err
		}
		return reads, warnings, nil
	}
	if !create && !hasStrategy {
		return reads, warnings, nil
	}
	adGroup := payload
	if !create {
		var err error
		adGroup, err = readObject("strategy_adgroup", "adgroups", id)
		if err != nil {
			return nil, nil, err
		}
	}
	campaignID := stringField(adGroup, "campaignId")
	if !decimalIDPattern.MatchString(campaignID) {
		return nil, nil, errors.New("ad group strategy requires a confirmed parent campaign")
	}
	campaign, err := readObject("strategy_parent_campaign", "campaigns", campaignID)
	if err != nil {
		return nil, nil, err
	}
	if err := validateAdGroupStrategyContext(campaign, adGroup, strategy, create); err != nil {
		return nil, nil, err
	}
	return reads, warnings, nil
}

func mutationWarnings(payload map[string]any) []operations.OperationWarning {
	warnings := make([]operations.OperationWarning, 0, 3)
	if _, exists := payload["cpaCap"]; exists {
		warnings = append(warnings, operations.OperationWarning{Code: "CPA_CAP_DEPRECATED", Message: "Apple has deprecated cpaCap. This preview preserves the legacy field and does not convert the campaign strategy."})
	}
	strategy, _ := payload["bidStrategy"].(map[string]any)
	if stringField(strategy, "bidStrategyType") == "MAX_CONVERSIONS" {
		warnings = append(warnings,
			operations.OperationWarning{Code: "MAX_CONVERSIONS_HISTORY_UNCONFIRMED", Message: "This explicit strategy request does not verify the optimizer's minimum of 14 completed days and 70 tap installs. Allow at least two weeks to assess learning after the change."},
			operations.OperationWarning{Code: "MAX_CONVERSIONS_BUDGET_GUIDANCE", Message: "Apple recommends a daily budget sufficient for approximately five conversions. This guidance is not an eligibility requirement."},
		)
	}
	return warnings
}

func effectiveBidStrategy(current, requested map[string]any) (map[string]any, error) {
	strategy := cloneObject(current)
	if strategy == nil {
		strategy = map[string]any{}
	}
	for key, value := range requested {
		strategy[key] = value
	}
	if stringField(strategy, "bidStrategyType") == "" || stringField(strategy, "bidStrategyGoal") == "" {
		return nil, errors.New("current bid strategy type and goal are required for a partial update")
	}
	if err := validateBidStrategy(strategy); err != nil {
		return nil, err
	}
	return strategy, nil
}

func validateCampaignStrategyContext(campaign, requested map[string]any) error {
	if stringField(campaign, "promotedObjectType") != "APPSTORE_APP" {
		return errors.New("bid strategies require an APPSTORE_APP campaign")
	}
	current, _ := campaign["bidStrategy"].(map[string]any)
	strategy, err := effectiveBidStrategy(current, requested)
	if err != nil {
		return err
	}
	if stringField(campaign, "billingEvent") != "TAPS" {
		return errors.New("App Store bid strategies require campaign billingEvent TAPS")
	}
	targeting, _ := campaign["targeting"].(map[string]any)
	placement, _ := targeting["supplyPlacement"].(map[string]any)
	placements := stringSlice(placement["include"])
	if len(placements) != 1 {
		return errors.New("bid strategy requires exactly one confirmed App Store placement")
	}
	switch placements[0] {
	case "APPSTORE_SEARCH_RESULTS", "APPSTORE_SEARCH_TAB", "APPSTORE_TODAY_TAB", "APPSTORE_PRODUCT_PAGES":
	default:
		return errors.New("bid strategy placement is outside App Store scope")
	}
	if stringField(strategy, "bidStrategyType") == "MAX_CONVERSIONS" && placements[0] != "APPSTORE_SEARCH_RESULTS" {
		return errors.New("MAX_CONVERSIONS requires APPSTORE_SEARCH_RESULTS")
	}
	return nil
}

func validateAdGroupStrategyContext(campaign, adGroup, requested map[string]any, create bool) error {
	parent, _ := campaign["bidStrategy"].(map[string]any)
	if err := validateCampaignStrategyContext(campaign, parent); err != nil {
		return err
	}
	if stringField(adGroup, "pricingModel") != "CPT" {
		return errors.New("App Store ad group strategies require pricingModel CPT")
	}
	current, _ := adGroup["bidStrategy"].(map[string]any)
	if current == nil || create {
		current = parent
	}
	strategy, err := effectiveBidStrategy(current, requested)
	if err != nil {
		return err
	}
	if stringField(strategy, "bidStrategyType") != stringField(parent, "bidStrategyType") || stringField(strategy, "bidStrategyGoal") != stringField(parent, "bidStrategyGoal") {
		return errors.New("ad group bid strategy must match the parent campaign type and goal")
	}
	if !create && stringField(parent, "bidStrategyType") == "MAX_CONVERSIONS" && requested != nil {
		if stringField(requested, "bidStrategyType") != "MAX_CONVERSIONS" || stringField(requested, "bidStrategyGoal") != "INSTALL" {
			return errors.New("automated ad group updates must echo the parent MAX_CONVERSIONS and INSTALL pair")
		}
		if _, exists := requested["bid"]; exists {
			bid, _ := requested["bid"].(map[string]any)
			amount, ok := new(big.Rat).SetString(stringField(bid, "amount"))
			if !ok || amount.Sign() != 0 {
				return errors.New("automated ad group updates may only echo a zero bid")
			}
		}
	}
	return nil
}

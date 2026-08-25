package tools

import "github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"

type TypedCreatePreviewInput[T any] struct {
	AccountInput
	Payload T `json:"payload" jsonschema:"typed create payload for the resource"`
}

type TypedUpdatePreviewInput[T any] struct {
	AccountInput
	ID      string `json:"id" jsonschema:"target resource ID as a string"`
	Payload T      `json:"payload" jsonschema:"typed fields to update"`
}

type TargetingData struct {
	Include []string `json:"include,omitempty" jsonschema:"included targeting values"`
	Exclude []string `json:"exclude,omitempty" jsonschema:"excluded targeting values"`
}

type CampaignTargeting struct {
	SupplySource    *TargetingData `json:"supplySource,omitempty" jsonschema:"App Store supply source; server restricts this to APPSTORE"`
	SupplyPlacement *TargetingData `json:"supplyPlacement,omitempty" jsonschema:"one supported App Store placement"`
	CountryOrRegion *TargetingData `json:"countryOrRegion,omitempty" jsonschema:"ISO 3166-1 alpha-2 country codes"`
}

type BidStrategy struct {
	BidStrategyType string          `json:"bidStrategyType,omitempty" jsonschema:"Apple bid strategy type"`
	BidStrategyGoal string          `json:"bidStrategyGoal,omitempty" jsonschema:"Apple bid strategy goal"`
	Bid             *appleads.Money `json:"bid,omitempty" jsonschema:"manual bid amount and account currency"`
}

type CPAGoal struct {
	Value appleads.Money `json:"value" jsonschema:"CPA goal amount and account currency"`
}

type MoneyValue struct {
	Value appleads.Money `json:"value" jsonschema:"money amount and account currency"`
}

type CampaignCreatePayload struct {
	AdAccountID        string              `json:"adAccountId,omitempty" jsonschema:"must match the explicit adAccountId when supplied"`
	Name               string              `json:"name" jsonschema:"campaign name"`
	BillingEvent       string              `json:"billingEvent" jsonschema:"Apple billing event"`
	StartTime          *appleads.Timestamp `json:"startTime,omitempty" jsonschema:"campaign start time in ISO 8601"`
	EndTime            *appleads.Timestamp `json:"endTime,omitempty" jsonschema:"campaign end time in ISO 8601"`
	PromotedObjectType string              `json:"promotedObjectType" jsonschema:"must be APPSTORE_APP"`
	PromotedObjectID   string              `json:"promotedObjectId" jsonschema:"App Store Adam ID as a string"`
	Status             string              `json:"status" jsonschema:"campaign status"`
	DailyBudget        MoneyValue          `json:"dailyBudget" jsonschema:"daily budget value in account currency"`
	Targeting          CampaignTargeting   `json:"targeting" jsonschema:"App Store placement and countries"`
	BidStrategy        *BidStrategy        `json:"bidStrategy,omitempty" jsonschema:"optional campaign bid strategy"`
}

type CampaignUpdatePayload struct {
	Name        *string             `json:"name,omitempty" jsonschema:"campaign name"`
	StartTime   *appleads.Timestamp `json:"startTime,omitempty" jsonschema:"campaign start time in ISO 8601"`
	EndTime     *appleads.Timestamp `json:"endTime,omitempty" jsonschema:"campaign end time in ISO 8601"`
	Status      *string             `json:"status,omitempty" jsonschema:"campaign status"`
	DailyBudget *MoneyValue         `json:"dailyBudget,omitempty" jsonschema:"daily budget value in account currency"`
	Targeting   *CampaignTargeting  `json:"targeting,omitempty" jsonschema:"App Store placement and countries"`
	BidStrategy *BidStrategy        `json:"bidStrategy,omitempty" jsonschema:"campaign bid strategy"`
}

type AdGroupTargeting struct {
	Country       *TargetingData `json:"country,omitempty"`
	AdminArea     *TargetingData `json:"adminArea,omitempty"`
	Locality      *TargetingData `json:"locality,omitempty"`
	DeviceClass   *TargetingData `json:"deviceClass,omitempty"`
	MinAge        *TargetingData `json:"minAge,omitempty"`
	MaxAge        *TargetingData `json:"maxAge,omitempty"`
	Gender        *TargetingData `json:"gender,omitempty"`
	AppCategory   *TargetingData `json:"appCategory,omitempty"`
	AppDownloader *TargetingData `json:"appDownloader,omitempty"`
	Daypart       *TargetingData `json:"daypart,omitempty"`
}

type AdGroupCreatePayload struct {
	Name                      string              `json:"name"`
	CampaignID                string              `json:"campaignId"`
	StartTime                 appleads.Timestamp  `json:"startTime"`
	EndTime                   *appleads.Timestamp `json:"endTime,omitempty"`
	PricingModel              string              `json:"pricingModel"`
	AutomatedKeywordsOptIn    *bool               `json:"automatedKeywordsOptIn,omitempty"`
	Status                    string              `json:"status"`
	AutomatedKeywordsRequired *bool               `json:"automatedKeywordsRequired,omitempty"`
	BidStrategy               *BidStrategy        `json:"bidStrategy,omitempty"`
	CPACap                    *CPAGoal            `json:"cpaCap,omitempty"`
	Targeting                 *AdGroupTargeting   `json:"targeting,omitempty"`
}

type AdGroupUpdatePayload struct {
	Name                      *string             `json:"name,omitempty"`
	StartTime                 *appleads.Timestamp `json:"startTime,omitempty"`
	EndTime                   *appleads.Timestamp `json:"endTime,omitempty"`
	AutomatedKeywordsOptIn    *bool               `json:"automatedKeywordsOptIn,omitempty"`
	Status                    *string             `json:"status,omitempty"`
	AutomatedKeywordsRequired *bool               `json:"automatedKeywordsRequired,omitempty"`
	BidStrategy               *BidStrategy        `json:"bidStrategy,omitempty"`
	CPACap                    *CPAGoal            `json:"cpaCap,omitempty"`
	Targeting                 *AdGroupTargeting   `json:"targeting,omitempty"`
}

type KeywordCreatePayload struct {
	AdGroupID string          `json:"adGroupId"`
	Text      string          `json:"text"`
	MatchType string          `json:"matchType"`
	Bid       *appleads.Money `json:"bid,omitempty"`
	Status    string          `json:"status"`
}

type KeywordUpdatePayload struct {
	Bid    *appleads.Money `json:"bid,omitempty"`
	Status *string         `json:"status,omitempty"`
}

type NegativeKeywordCreatePayload struct {
	CampaignID string `json:"campaignId,omitempty"`
	AdGroupID  string `json:"adGroupId,omitempty"`
	Text       string `json:"text"`
	MatchType  string `json:"matchType"`
	Status     string `json:"status"`
}

type NegativeKeywordUpdatePayload struct {
	Status *string `json:"status,omitempty"`
}

type AdCreatePayload struct {
	AdGroupID  string `json:"adGroupId"`
	CreativeID string `json:"creativeId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
}

type AdUpdatePayload struct {
	Name   *string `json:"name,omitempty"`
	Status *string `json:"status,omitempty"`
}

type DestinationParameters struct {
	AdamID        string `json:"adamId,omitempty"`
	ProductPageID string `json:"productPageId,omitempty"`
}

type Destination struct {
	DestinationType string                 `json:"destinationType"`
	Parameters      *DestinationParameters `json:"parameters,omitempty"`
}

type AppStoreCreativeSpec struct{}

type CreativeCreatePayload struct {
	Name         string               `json:"name"`
	CreativeType string               `json:"creativeType"`
	CreativeSpec AppStoreCreativeSpec `json:"creativeSpec" jsonschema:"empty App Store creative specification; destination identifies the DPP or CPP"`
	Destination  Destination          `json:"destination"`
}

type CreativeUpdatePayload struct {
	Name *string `json:"name,omitempty"`
}

type CampaignMoneyPreviewInput struct {
	AccountInput
	CampaignID string         `json:"campaignId"`
	Amount     appleads.Money `json:"amount"`
}

type CampaignCountriesPreviewInput struct {
	AccountInput
	CampaignID string   `json:"campaignId"`
	Countries  []string `json:"countries"`
}

type SchedulePreviewInput struct {
	AccountInput
	ID        string              `json:"id"`
	StartTime *appleads.Timestamp `json:"startTime,omitempty"`
	EndTime   *appleads.Timestamp `json:"endTime,omitempty"`
}

type ResourceStatePreviewInput struct {
	AccountInput
	ID string `json:"id"`
}

type AdGroupSearchMatchPreviewInput struct {
	AccountInput
	AdGroupID string `json:"adGroupId"`
	Enabled   bool   `json:"enabled"`
}

type AdGroupTargetingPreviewInput struct {
	AccountInput
	AdGroupID string           `json:"adGroupId"`
	Targeting AdGroupTargeting `json:"targeting"`
}

type KeywordBidPreviewInput struct {
	AccountInput
	KeywordID string         `json:"keywordId"`
	Bid       appleads.Money `json:"bid"`
}

type BulkKeywordCreateItem struct {
	CorrelationID string          `json:"correlationId"`
	Text          string          `json:"text"`
	MatchType     string          `json:"matchType"`
	Bid           *appleads.Money `json:"bid,omitempty"`
	Status        string          `json:"status"`
}

type BulkKeywordCreateInput struct {
	AccountInput
	AdGroupID string                  `json:"adGroupId"`
	Items     []BulkKeywordCreateItem `json:"items"`
}

type BulkKeywordUpdateItem struct {
	CorrelationID string          `json:"correlationId"`
	ID            string          `json:"id"`
	Bid           *appleads.Money `json:"bid,omitempty"`
	Status        *string         `json:"status,omitempty"`
}

type BulkKeywordUpdateInput struct {
	AccountInput
	AdGroupID string                  `json:"adGroupId"`
	Items     []BulkKeywordUpdateItem `json:"items"`
}

type BulkNegativeKeywordCreateItem struct {
	CorrelationID string `json:"correlationId"`
	Text          string `json:"text"`
	MatchType     string `json:"matchType"`
	Status        string `json:"status"`
}

type BulkNegativeKeywordCreateInput struct {
	AccountInput
	CampaignID string                          `json:"campaignId,omitempty"`
	AdGroupID  string                          `json:"adGroupId,omitempty"`
	Items      []BulkNegativeKeywordCreateItem `json:"items"`
}

type BulkNegativeKeywordUpdateItem struct {
	CorrelationID string  `json:"correlationId"`
	ID            string  `json:"id"`
	Status        *string `json:"status,omitempty"`
}

type BulkNegativeKeywordUpdateInput struct {
	AccountInput
	CampaignID string                          `json:"campaignId,omitempty"`
	AdGroupID  string                          `json:"adGroupId,omitempty"`
	Items      []BulkNegativeKeywordUpdateItem `json:"items"`
}

type RecommendationActionPreviewInput struct {
	AccountInput
	RecommendationID string          `json:"recommendationId"`
	PromotedObjectID string          `json:"promotedObjectId"`
	AppliedAmount    *appleads.Money `json:"appliedAmount,omitempty"`
	MaximumAmount    *appleads.Money `json:"maximumAmount,omitempty"`
}

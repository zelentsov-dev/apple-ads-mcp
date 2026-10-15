package tools

import (
	"fmt"
	"math"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

const positiveBidAmountPattern = `^(0\.0*[1-9][0-9]*|[1-9][0-9]*(\.[0-9]+)?)$`

func mutationInputSchema[In any](toolName string) *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("infer input schema for %s: %v", toolName, err))
	}
	applyMutationSchemaConstraints(schema, "", false)
	if toolName == "ad_group_cpa_cap_preview" {
		schema.Deprecated = true
	}
	if toolName == "campaign_create_preview" {
		payload := schema.Properties["payload"]
		if !slices.Contains(payload.Required, "bidStrategy") {
			payload.Required = append(payload.Required, "bidStrategy")
		}
	}
	if toolName == "campaign_create_preview" || toolName == "ad_group_create_preview" {
		strategy := schema.Properties["payload"].Properties["bidStrategy"]
		strategy.Required = []string{"bidStrategyType", "bidStrategyGoal"}
		strategy.AllOf = append(strategy.AllOf, &jsonschema.Schema{
			If: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{
				"bidStrategyType": {Enum: []any{"MANUAL_CPT"}},
			}},
			Then: &jsonschema.Schema{Required: []string{"bid"}},
		})
	}
	if toolName == "ad_group_bid_preview" {
		if amount := schema.Properties["amount"]; amount != nil {
			amount.Pattern = positiveBidAmountPattern
		}
	}
	return schema
}

func applyMutationSchemaConstraints(schema *jsonschema.Schema, parentName string, inStrategy bool) {
	if schema == nil {
		return
	}
	inStrategy = inStrategy || parentName == "bidStrategy" || parentName == "strategy"
	for name, property := range schema.Properties {
		switch name {
		case "status":
			property.Enum = []any{"ENABLED", "PAUSED"}
		case "correlationId":
			description := property.Description
			*property = jsonschema.Schema{
				Description: description,
				OneOf: []*jsonschema.Schema{
					{Type: "integer", Minimum: jsonschema.Ptr(0.0), Maximum: jsonschema.Ptr(math.Exp2(53) - 1)},
					{Type: "string", MinLength: jsonschema.Ptr(1), Pattern: `^[0-9]+$`},
				},
			}
		case "cpaCap":
			property.Deprecated = true
		case "amount":
			if parentName == "bid" {
				property.Pattern = positiveBidAmountPattern
				if inStrategy {
					property.Pattern = `^(0|[1-9][0-9]*)(\.[0-9]+)?$`
				}
			}
		}
		applyMutationSchemaConstraints(property, name, inStrategy)
	}
	applyMutationSchemaConstraints(schema.Items, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.AdditionalItems, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.Contains, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.AdditionalProperties, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.PropertyNames, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.UnevaluatedItems, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.UnevaluatedProperties, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.Not, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.If, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.Then, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.Else, parentName, inStrategy)
	applyMutationSchemaConstraints(schema.ContentSchema, parentName, inStrategy)
	for _, collection := range [][]*jsonschema.Schema{schema.PrefixItems, schema.ItemsArray, schema.AllOf, schema.AnyOf, schema.OneOf} {
		for _, item := range collection {
			applyMutationSchemaConstraints(item, parentName, inStrategy)
		}
	}
	for _, collection := range []map[string]*jsonschema.Schema{
		schema.Defs,
		schema.Definitions,
		schema.DependencySchemas,
		schema.DependentSchemas,
		schema.PatternProperties,
	} {
		for _, item := range collection {
			applyMutationSchemaConstraints(item, parentName, inStrategy)
		}
	}
	if (parentName == "bidStrategy" || parentName == "strategy") && schema.Properties["bidStrategyType"] != nil && schema.Properties["bidStrategyGoal"] != nil {
		schema.Properties["bidStrategyType"].Enum = []any{"MANUAL_CPT", "MAX_CONVERSIONS"}
		schema.Properties["bidStrategyGoal"].Enum = []any{"TAP", "INSTALL"}
		schema.DependentRequired = map[string][]string{
			"bidStrategyType": {"bidStrategyGoal"},
			"bidStrategyGoal": {"bidStrategyType"},
		}
		for _, pair := range [][2]string{{"MANUAL_CPT", "TAP"}, {"MAX_CONVERSIONS", "INSTALL"}} {
			schema.AllOf = append(schema.AllOf, &jsonschema.Schema{
				If: &jsonschema.Schema{Required: []string{"bidStrategyType"}, Properties: map[string]*jsonschema.Schema{
					"bidStrategyType": {Enum: []any{pair[0]}},
				}},
				Then: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{
					"bidStrategyGoal": {Enum: []any{pair[1]}},
				}},
			})
		}
	}
}

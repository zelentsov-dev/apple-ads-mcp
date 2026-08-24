package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestQueryInputBoundedRequestNormalizesAdamIDs(t *testing.T) {
	input := QueryInput{Filters: []QueryFilterInput{
		{Field: "adamId", Operator: "EQUALS", Value: "7654321098"},
		{Field: "adamId", Operator: "IN", Value: []string{"1", "2"}},
		{Field: "promotedObjectId", Operator: "EQUALS", Value: []string{"7654321098"}},
	}}

	request, err := input.boundedRequest()
	if err != nil {
		t.Fatal(err)
	}
	filters := request["filters"].([]QueryFilterInput)
	if filters[0].Value != json.Number("7654321098") {
		t.Fatalf("adamId was not normalized: %#v", filters[0].Value)
	}
	if !reflect.DeepEqual(filters[1].Value, []any{json.Number("1"), json.Number("2")}) {
		t.Fatalf("adamId array was not normalized: %#v", filters[1].Value)
	}
	if !reflect.DeepEqual(filters[2].Value, []string{"7654321098"}) {
		t.Fatalf("promotedObjectId must remain a public string: %#v", filters[2].Value)
	}

	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	expected := `"field":"adamId","operator":"EQUALS","value":7654321098`
	if !strings.Contains(string(encoded), expected) {
		t.Fatalf("wire request does not contain numeric adamId: %s", encoded)
	}
}

func TestQueryInputBoundedRequestRejectsInvalidAdamID(t *testing.T) {
	for _, value := range []any{"not-an-id", "0", -1, float64(1.5)} {
		input := QueryInput{Filters: []QueryFilterInput{{Field: "adamId", Operator: "EQUALS", Value: value}}}
		if _, err := input.boundedRequest(); err == nil {
			t.Fatalf("expected invalid adamId error for %#v", value)
		}
	}
}

package tools

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestFailedPreviewOmitsInvalidEmptyPreview(t *testing.T) {
	_, output, err := failedPreview(errors.New("invalid keyword"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if output.Preview != nil || !json.Valid(data) {
		t.Fatalf("output=%+v json=%s", output, data)
	}
}

func TestCreateVerificationQueryUsesExplicitParent(t *testing.T) {
	request := createVerificationQuery("keywords", map[string]any{"adGroupId": "123"})
	filters, ok := request["filters"].([]any)
	if !ok || len(filters) != 1 {
		t.Fatalf("request=%#v", request)
	}
	filter, ok := filters[0].(map[string]any)
	if !ok || filter["field"] != "adGroupId" || filter["value"] != "123" {
		t.Fatalf("filter=%#v", filters[0])
	}
}

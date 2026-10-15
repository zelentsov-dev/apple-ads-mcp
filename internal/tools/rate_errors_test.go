package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
)

func TestRateLimitErrorClassification(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		kind      string
		retryable bool
	}{
		{"read wait budget", &appleads.RateLimitError{Code: "RATE_LIMIT_WAIT_EXCEEDED", RateLimit: appleads.RateLimit{RetryAfter: 37}}, "rate_limit_error", true},
		{"write cooldown", &appleads.RateLimitError{Code: "RATE_LIMIT_COOLDOWN", RateLimit: appleads.RateLimit{RetryAfter: 37}, Mutation: true}, "rate_limit_error", false},
		{"invalid timing", &appleads.RateLimitError{Code: "RATE_LIMIT_TIMING_INVALID"}, "rate_limit_error", false},
		{"canceled read", &appleads.RateLimitError{Code: "RATE_LIMIT_WAIT_CANCELED", Cause: context.Canceled}, "rate_limit_error", false},
		{"ambiguous takes precedence", &appleads.AmbiguousWriteError{Cause: &appleads.APIError{HTTPStatus: 503, Retryable: true}}, "ambiguous_write", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := classifyError(fmt.Errorf("request: %w", test.err))
			if output.Type != test.kind || output.Retryable != test.retryable {
				t.Fatalf("output=%+v", output)
			}
			if test.kind == "rate_limit_error" {
				if output.Details["nextRequestSent"] != false {
					t.Fatalf("not-sent diagnostic missing: %+v", output)
				}
			}
		})
	}
}

func TestAPIErrorPreservesRateMetadataWithoutChangingOriginalDetails(t *testing.T) {
	apiErr := &appleads.APIError{HTTPStatus: 429, Message: "Too Many Requests", Details: map[string]any{"code": "LIMIT"}, RateLimit: appleads.RateLimit{Limit: "100", Remaining: "0", Reset: "37", RetryAfter: 37}}
	output := classifyError(apiErr)
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if output.Details["code"] != "LIMIT" || !strings.Contains(string(encoded), `"retryAfterSeconds":37`) {
		t.Fatalf("output=%s", encoded)
	}
	if _, mutated := apiErr.Details["rateLimit"]; mutated {
		t.Fatal("classification mutated upstream error details")
	}
}

type rateReceiptExecutor struct {
	mutationErr error
	writes      int
	state       map[string]any
}

func (executor *rateReceiptExecutor) Do(_ context.Context, _, _ string, operation appleads.Operation) (appleads.Result, error) {
	if operation.IsMutation() {
		executor.writes++
		return appleads.Result{}, executor.mutationErr
	}
	return appleads.Result{Status: 200, Data: executor.state}, nil
}

func TestRateLimitRejectionConsumesReceiptWithoutAmbiguousOutcome(t *testing.T) {
	now := time.Now()
	store := operations.NewStoreForTest(func() time.Time { return now }, 10*time.Minute)
	executor := &rateReceiptExecutor{mutationErr: &appleads.RateLimitError{Code: "RATE_LIMIT_COOLDOWN", Mutation: true}, state: map[string]any{"status": "PAUSED"}}
	read, _ := appleads.ResourceGet("campaigns", "1")
	write, _ := appleads.ResourceUpdate("campaigns", "1", map[string]any{"status": "ENABLED"})
	preview, err := store.Preview(context.Background(), executor, "synthetic", "1", "campaign_update", []string{"1"}, map[string]any{"status": "ENABLED"}, read, write)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(context.Background(), executor, preview.Receipt)
	var rateErr *appleads.RateLimitError
	if !errors.As(err, &rateErr) || receipt.Status == "unknown" || executor.writes != 1 {
		t.Fatalf("receipt=%+v error=%v attempts=%d", receipt, err, executor.writes)
	}
	if _, err := store.Apply(context.Background(), executor, preview.Receipt); !errors.Is(err, operations.ErrReceiptUsed) {
		t.Fatalf("receipt could be reused: %v", err)
	}
}

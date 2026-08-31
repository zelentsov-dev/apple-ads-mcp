package appleads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTokens struct {
	value       string
	tokenErr    error
	invalidated atomic.Int32
}

func (f *fakeTokens) Token(context.Context) (string, error) { return f.value, f.tokenErr }
func (f *fakeTokens) Invalidate()                           { f.invalidated.Add(1) }

func TestClientHeadersAndIDStrings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/123" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-AP-Context") != "adAccountId=456" {
			t.Errorf("headers=%v", r.Header)
		}
		_, _ = w.Write([]byte(`{"data":{"id":9007199254740993,"amount":"1.25","currency":"USD"}}`))
	}))
	defer server.Close()
	client := testClient(t, server, &fakeTokens{value: "test-token"})
	op, _ := App("123")
	result, err := client.Do(context.Background(), "456", op)
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	if data["id"] != "9007199254740993" {
		t.Fatalf("ID lost precision: %#v", data["id"])
	}
}

func TestUnscopedRequestOmitsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("X-AP-Context"); value != "" {
			t.Errorf("unexpected account context %q", value)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	client := testClient(t, server, &fakeTokens{value: "token"})
	if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
		t.Fatal(err)
	}
}

func TestUnauthorizedRefreshOnce(t *testing.T) {
	var calls atomic.Int32
	tokens := &fakeTokens{value: "token"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"expired"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer server.Close()
	client := testClient(t, server, tokens)
	if _, err := client.Do(context.Background(), "", Me()); err != nil {
		t.Fatal(err)
	}
	if tokens.invalidated.Load() != 1 || calls.Load() != 2 {
		t.Fatalf("invalidations=%d calls=%d", tokens.invalidated.Load(), calls.Load())
	}
}

func TestRetryAfterAndRateLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMIT"}}`))
			return
		}
		w.Header().Set("X-Rate-Limit-Remaining", "9")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	client := testClient(t, server, &fakeTokens{value: "token"})
	var delay time.Duration
	client.sleep = func(_ context.Context, value time.Duration) error { delay = value; return nil }
	result, err := client.Do(context.Background(), "", ACLs())
	if err != nil {
		t.Fatal(err)
	}
	if delay != 2*time.Second || result.RateLimit.Remaining != "9" {
		t.Fatalf("delay=%v rate=%+v", delay, result.RateLimit)
	}
}

func TestAppleErrorAndMalformedJSON(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"role missing","details":[{"code":"ROLE_REQUIRED","message":"API role missing","info":{"field":"role"}}]}}`))
		}))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		_, err := client.Do(context.Background(), "", ACLs())
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusForbidden || apiErr.Code != "FORBIDDEN" {
			t.Fatalf("error=%#v", err)
		}
		if apiErr.Message != "role missing" {
			t.Fatalf("message=%q", apiErr.Message)
		}
		if apiErr.Body == nil || apiErr.Body["code"] != "FORBIDDEN" || apiErr.Body["message"] != "role missing" {
			t.Fatalf("safe 4xx body=%#v", apiErr.Body)
		}
		details, ok := apiErr.Details["details"].([]any)
		if !ok || len(details) != 1 {
			t.Fatalf("details=%#v", apiErr.Details)
		}
	})
	t.Run("server details redacted", func(t *testing.T) {
		err := apiError(http.StatusInternalServerError, map[string]any{
			"code":    "INTERNAL\nINJECTED",
			"message": "sensitive upstream message",
			"details": []any{map[string]any{"message": "sensitive detail"}},
		})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Message != http.StatusText(http.StatusInternalServerError) {
			t.Fatalf("error=%#v", err)
		}
		if strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("server details leaked: %v", err)
		}
		if apiErr.Code != "INTERNAL INJECTED" || apiErr.Details == nil || apiErr.Details["code"] != "INTERNAL INJECTED" {
			t.Fatalf("safe server code missing: %#v", apiErr)
		}
		if _, exists := apiErr.Details["details"]; exists {
			t.Fatalf("server details leaked: %#v", apiErr.Details)
		}
		if apiErr.Body != nil {
			t.Fatalf("server body leaked: %#v", apiErr.Body)
		}
	})
	t.Run("client error diagnostics bounded", func(t *testing.T) {
		details := make([]any, 25)
		for index := range details {
			details[index] = map[string]any{
				"code":    strings.Repeat("C", 140),
				"message": "invalid\nvalue " + strings.Repeat("m", 1100),
				"info": map[string]any{
					"field":       strings.Repeat("v", 1100),
					"requestBody": `{"authorization":"secret"}`,
				},
			}
		}
		err := apiError(http.StatusBadRequest, map[string]any{
			"code":    strings.Repeat("X", 140),
			"message": "bad\nrequest " + strings.Repeat("m", 1100),
			"details": details,
		})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error=%#v", err)
		}
		if len([]rune(apiErr.Code)) != 128 || len([]rune(apiErr.Message)) != 1024 || strings.ContainsAny(apiErr.Code+apiErr.Message, "\r\n") {
			t.Fatalf("unbounded error: code=%q messageLength=%d", apiErr.Code, len([]rune(apiErr.Message)))
		}
		safeDetails, ok := apiErr.Details["details"].([]any)
		if !ok || len(safeDetails) != 20 {
			t.Fatalf("details=%#v", apiErr.Details)
		}
		first := safeDetails[0].(map[string]any)
		if len([]rune(first["code"].(string))) != 128 || len([]rune(first["message"].(string))) != 1024 {
			t.Fatalf("detail not bounded: %#v", first)
		}
		info := first["info"].(map[string]string)
		if _, exists := info["field"]; !exists || len([]rune(info["field"])) != 1024 {
			t.Fatalf("info not sanitized: %#v", info)
		}
		if _, exists := info["requestBody"]; exists {
			t.Fatalf("request body leaked: %#v", info)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{")) }))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		_, err := client.Do(context.Background(), "", ACLs())
		if err == nil || !strings.Contains(err.Error(), "decode Apple Ads response") {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("non-json HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<html>not found</html>"))
		}))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		_, err := client.Do(context.Background(), "", ACLs())
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusNotFound || apiErr.Message != http.StatusText(http.StatusNotFound) {
			t.Fatalf("error=%#v", err)
		}
		if strings.Contains(err.Error(), "<html>") || apiErr.Details != nil || apiErr.ResponseFormat != "non_json" {
			t.Fatalf("raw response body leaked: %v", err)
		}
	})
	t.Run("empty HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		_, err := client.Do(context.Background(), "", ACLs())
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusBadRequest || apiErr.ResponseFormat != "empty" {
			t.Fatalf("error=%#v", err)
		}
	})
}

func TestNotFoundServerRetryAndPagination(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"missing"}}`))
		}))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		_, err := client.Do(context.Background(), "", ACLs())
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusNotFound {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("server retry and pagination", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"code":"UNAVAILABLE"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"result":[],"pagination":{"offset":0,"pageSize":50,"totalCount":125}}`))
		}))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		result, err := client.Do(context.Background(), "", ACLs())
		if err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 || result.Pagination.Next != "offset:50" || result.Pagination.Total != 125 {
			t.Fatalf("calls=%d pagination=%+v", calls.Load(), result.Pagination)
		}
	})
}

func TestMutationTransportErrorIsAmbiguousAndNotRetried(t *testing.T) {
	var calls atomic.Int32
	client, err := NewClientForTest(&fakeTokens{value: "token"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("network lost")
	})}, "https://example.invalid/v1/")
	if err != nil {
		t.Fatal(err)
	}
	op, _ := ResourceCreate("campaigns", map[string]any{"name": "test"})
	_, err = client.Do(context.Background(), "1", op)
	var ambiguous *AmbiguousWriteError
	if !errors.As(err, &ambiguous) || calls.Load() != 1 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
}

func TestMutationServerErrorIsAmbiguousAndNotRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"UNAVAILABLE","message":"retry this write"}}`))
	}))
	defer server.Close()
	client := testClient(t, server, &fakeTokens{value: "token"})
	op, _ := ResourceCreate("campaigns", map[string]any{"name": "test"})
	_, err := client.Do(context.Background(), "1", op)
	var ambiguous *AmbiguousWriteError
	if !errors.As(err, &ambiguous) || calls.Load() != 1 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
	if strings.Contains(err.Error(), "retry this write") {
		t.Fatalf("upstream message leaked: %v", err)
	}
}

func TestMutationAuthorizationFailureIsNotAmbiguous(t *testing.T) {
	client, err := NewClientForTest(&fakeTokens{tokenErr: errors.New("key unavailable")}, http.DefaultClient, "https://example.invalid/v1/")
	if err != nil {
		t.Fatal(err)
	}
	op, _ := ResourceCreate("campaigns", map[string]any{"name": "test"})
	_, err = client.Do(context.Background(), "1", op)
	var ambiguous *AmbiguousWriteError
	if err == nil || errors.As(err, &ambiguous) {
		t.Fatalf("error=%v", err)
	}
}

func TestMutationNumericIDsUseAppleWireTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Error(err)
		}
		if _, ok := body["campaignId"].(json.Number); !ok {
			t.Errorf("campaignId wire type=%T", body["campaignId"])
		}
		bid := body["bidStrategy"].(map[string]any)["bid"].(map[string]any)
		if _, ok := bid["amount"].(string); !ok {
			t.Errorf("money amount wire type=%T", bid["amount"])
		}
		_, _ = w.Write([]byte(`{"result":{"id":123}}`))
	}))
	defer server.Close()
	client := testClient(t, server, &fakeTokens{value: "token"})
	op, err := ResourceCreate("adgroups", map[string]any{"campaignId": "123", "bidStrategy": map[string]any{"bid": map[string]any{"amount": "1.25", "currency": "USD"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), "456", op); err != nil {
		t.Fatal(err)
	}
}

func TestMoneyAndTemporalTypes(t *testing.T) {
	money := Money{Amount: "12.50", Currency: "USD"}
	if err := money.Validate(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(money)
	if string(data) != `{"amount":"12.50","currency":"USD"}` {
		t.Fatalf("money=%s", data)
	}
	var date Date
	if err := json.Unmarshal([]byte(`"2026-08-24"`), &date); err != nil {
		t.Fatal(err)
	}
	var timestamp Timestamp
	if err := json.Unmarshal([]byte(`"2026-08-24T10:00:00Z"`), &timestamp); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`"2026-08-24T10:00:00.000"`), &timestamp); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`"2026-08-24 10:00:00"`), &timestamp); err == nil {
		t.Fatal("timestamp without ISO 8601 T separator must be rejected")
	}
}

func TestResponseSizeLimitAndUnknownEnum(t *testing.T) {
	t.Run("size limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"result":"` + strings.Repeat("x", maxResponseBody) + `"}`))
		}))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		if _, err := client.Do(context.Background(), "", ACLs()); err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("unknown enum", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"result":{"status":"FUTURE_APPLE_CASE"}}`))
		}))
		defer server.Close()
		client := testClient(t, server, &fakeTokens{value: "token"})
		result, err := client.Do(context.Background(), "", ACLs())
		if err != nil {
			t.Fatal(err)
		}
		if result.Data.(map[string]any)["status"] != "FUTURE_APPLE_CASE" {
			t.Fatalf("data=%v", result.Data)
		}
	})
}

func testClient(t *testing.T, server *httptest.Server, tokens TokenSource) *Client {
	t.Helper()
	client, err := NewClientForTest(tokens, server.Client(), server.URL+"/v1/")
	if err != nil {
		t.Fatal(err)
	}
	client.sleep = func(context.Context, time.Duration) error { return nil }
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

package appleads

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxRequestBody  = 1 << 20
	maxResponseBody = 4 << 20
)

type TokenSource interface {
	Token(context.Context) (string, error)
	Invalidate()
}

type Client struct {
	httpClient *http.Client
	tokens     TokenSource
	baseURL    *url.URL
	sleep      func(context.Context, time.Duration) error
}

func NewClient(tokens TokenSource) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 20
	transport.IdleConnTimeout = 90 * time.Second
	baseURL, _ := url.Parse(BaseURL)
	return &Client{
		httpClient: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: rejectRedirect},
		tokens:     tokens,
		baseURL:    baseURL,
		sleep:      sleepContext,
	}
}

func NewClientForTest(tokens TokenSource, client *http.Client, baseURL string) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{httpClient: client, tokens: tokens, baseURL: parsed, sleep: sleepContext}, nil
}

func (c *Client) Do(ctx context.Context, adAccountID string, operation Operation) (Result, error) {
	if operation.RequiresAccount() && strings.TrimSpace(adAccountID) == "" {
		return Result{}, errors.New("adAccountId is required")
	}
	body, err := marshalBody(operation.body)
	if err != nil {
		return Result{}, err
	}
	maxAttempts := 1
	if operation.retryReads {
		maxAttempts = 3
	}
	refreshed := false
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, response, sent, err := c.doOnce(ctx, adAccountID, operation, body)
		if err != nil {
			if operation.mutation && sent {
				return Result{}, &AmbiguousWriteError{Cause: err}
			}
			if attempt == maxAttempts {
				return Result{}, err
			}
			if err := c.sleep(ctx, retryDelay(attempt, 0)); err != nil {
				return Result{}, err
			}
			continue
		}
		if response.StatusCode == http.StatusUnauthorized && !refreshed {
			c.tokens.Invalidate()
			refreshed = true
			attempt--
			continue
		}
		if retryableStatus(response.StatusCode) && operation.retryReads && attempt < maxAttempts {
			delay := retryDelay(attempt, result.RateLimit.RetryAfter)
			if err := c.sleep(ctx, delay); err != nil {
				return Result{}, err
			}
			continue
		}
		if operation.mutation && response.StatusCode >= http.StatusInternalServerError {
			return Result{}, &AmbiguousWriteError{Cause: apiError(response.StatusCode, result.Data)}
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return Result{}, apiError(response.StatusCode, result.Data)
		}
		return result, nil
	}
	return Result{}, errors.New("request to Apple Ads exhausted retries")
}

func (c *Client) doOnce(ctx context.Context, adAccountID string, operation Operation, body []byte) (Result, *http.Response, bool, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return Result{}, nil, false, fmt.Errorf("authorize Apple Ads request: %w", err)
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: operation.path})
	endpoint.RawQuery = operation.query.Encode()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, operation.method, endpoint.String(), reader)
	if err != nil {
		return Result{}, nil, false, fmt.Errorf("create Apple Ads request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if operation.RequiresAccount() {
		req.Header.Set("X-AP-Context", "adAccountId="+adAccountID)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, nil, true, fmt.Errorf("execute Apple Ads request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return Result{}, resp, true, fmt.Errorf("read Apple Ads response: %w", err)
	}
	if len(data) > maxResponseBody {
		return Result{}, resp, true, errors.New("response from Apple Ads exceeds size limit")
	}
	if len(bytes.TrimSpace(data)) == 0 && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return Result{
			Data:      map[string]any{"responseFormat": "empty"},
			Status:    resp.StatusCode,
			RateLimit: rateLimitFromHeaders(resp.Header),
		}, resp, true, nil
	}
	decoded, err := decodeEnvelope(data)
	if err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return Result{
				Data:      map[string]any{"message": http.StatusText(resp.StatusCode), "responseFormat": "non_json"},
				Status:    resp.StatusCode,
				RateLimit: rateLimitFromHeaders(resp.Header),
			}, resp, true, nil
		}
		return Result{}, resp, true, err
	}
	decoded.Status = resp.StatusCode
	decoded.RateLimit = rateLimitFromHeaders(resp.Header)
	return decoded, resp, true, nil
}

func marshalBody(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Apple Ads request: %w", err)
	}
	if len(data) > maxRequestBody {
		return nil, errors.New("request to Apple Ads exceeds size limit")
	}
	return data, nil
}

func ValidateRequestBody(value any) error {
	_, err := marshalBody(value)
	return err
}

func decodeEnvelope(data []byte) (Result, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Result{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil {
		return Result{}, fmt.Errorf("decode Apple Ads response: %w", err)
	}
	envelope = normalizeJSON(envelope).(map[string]any)
	result := Result{Data: envelope}
	if value, ok := envelope["result"]; ok {
		result.Data = value
	} else if value, ok := envelope["data"]; ok {
		result.Data = value
	} else if value, ok := envelope["error"]; ok {
		result.Data = value
	}
	if raw, ok := envelope["pagination"].(map[string]any); ok {
		result.Pagination = parsePagination(raw)
	}
	return result, nil
}

func parsePagination(raw map[string]any) Pagination {
	page := Pagination{
		Offset:   stringInt(raw["offset"]),
		PageSize: stringInt(firstValue(raw, "pageSize", "limit")),
		Total:    stringInt(firstValue(raw, "totalCount", "totalResults", "total")),
	}
	if page.PageSize > 0 && page.Offset+page.PageSize < page.Total {
		page.Next = fmt.Sprintf("offset:%d", page.Offset+page.PageSize)
	}
	return page
}

func firstValue(raw map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			return value
		}
	}
	return nil
}

func stringInt(value any) int {
	var result int
	_, _ = fmt.Sscan(fmt.Sprint(value), &result)
	return result
}

func apiError(status int, data any) error {
	result := &APIError{HTTPStatus: status, Message: http.StatusText(status), Retryable: retryableStatus(status)}
	if raw, ok := data.(map[string]any); ok {
		result.Code = safeErrorText(firstString(raw, "code", "errorCode"), 128)
		result.ResponseFormat = safeResponseFormat(firstString(raw, "responseFormat"))
		var details []any
		if status >= 400 && status < 500 {
			if message := safeErrorText(firstString(raw, "message"), 1024); message != "" {
				result.Message = message
			}
			details = safeErrorDetails(raw["details"])
		}
		if result.Code != "" || len(details) > 0 {
			result.Details = map[string]any{}
			if result.Code != "" {
				result.Details["code"] = result.Code
			}
			if len(details) > 0 {
				result.Details["details"] = details
			}
		}
		if status >= 400 && status < 500 && result.ResponseFormat == "" {
			result.Body = map[string]any{"message": result.Message}
			if result.Code != "" {
				result.Body["code"] = result.Code
			}
			if len(details) > 0 {
				result.Body["details"] = details
			}
		}
	}
	return result
}

func safeResponseFormat(value string) string {
	switch value {
	case "empty", "non_json":
		return value
	default:
		return ""
	}
}

func safeErrorDetails(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	if len(items) > 20 {
		items = items[:20]
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		detail := map[string]any{}
		for _, key := range []string{"code", "message"} {
			limit := 1024
			if key == "code" {
				limit = 128
			}
			if text := safeErrorText(firstString(raw, key), limit); text != "" {
				detail[key] = text
			}
		}
		if info, ok := raw["info"].(map[string]any); ok {
			safeInfo := map[string]string{}
			for key, value := range info {
				if len(safeInfo) == 20 {
					break
				}
				text, ok := value.(string)
				if !ok {
					continue
				}
				key = safeErrorText(key, 128)
				if !safeErrorInfoKey(key) {
					continue
				}
				text = safeErrorText(text, 1024)
				if key == "" || text == "" {
					continue
				}
				safeInfo[key] = text
			}
			if len(safeInfo) > 0 {
				detail["info"] = safeInfo
			}
		}
		if len(detail) > 0 {
			result = append(result, detail)
		}
	}
	return result
}

func safeErrorInfoKey(value string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(value))
	switch normalized {
	case "field", "parameter", "path", "location", "reason", "index", "correlationid", "resource", "resourceid", "selector":
		return true
	default:
		return false
	}
}

func safeErrorText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError && status <= 599
}

func retryDelay(attempt, retryAfter int) time.Duration {
	if retryAfter > 0 {
		if retryAfter > 5 {
			retryAfter = 5
		}
		return time.Duration(retryAfter) * time.Second
	}
	return time.Duration(attempt*200) * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func rejectRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

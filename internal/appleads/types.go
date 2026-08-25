package appleads

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const BaseURL = "https://api.ads.apple.com/v1/"

type APIError struct {
	HTTPStatus     int            `json:"httpStatus"`
	Code           string         `json:"code,omitempty"`
	Message        string         `json:"message"`
	Details        map[string]any `json:"details,omitempty"`
	ResponseFormat string         `json:"responseFormat,omitempty"`
	Retryable      bool           `json:"retryable"`
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("Apple Ads API HTTP %d: %s", e.HTTPStatus, e.Message)
	}
	return fmt.Sprintf("Apple Ads API HTTP %d (%s): %s", e.HTTPStatus, e.Code, e.Message)
}

type RateLimit struct {
	Limit      string `json:"limit,omitempty"`
	Remaining  string `json:"remaining,omitempty"`
	Reset      string `json:"reset,omitempty"`
	RetryAfter int    `json:"retryAfterSeconds,omitempty"`
}

type Pagination struct {
	Offset   int    `json:"offset,omitempty"`
	PageSize int    `json:"pageSize,omitempty"`
	Total    int    `json:"totalResults,omitempty"`
	Next     string `json:"next,omitempty"`
}

type Result struct {
	Data       any        `json:"data,omitempty"`
	Pagination Pagination `json:"pagination,omitempty"`
	RateLimit  RateLimit  `json:"rateLimit,omitempty"`
	Status     int        `json:"httpStatus"`
}

type AmbiguousWriteError struct {
	Cause error
}

func (e *AmbiguousWriteError) Error() string {
	return "write result is unknown and must be verified: " + e.Cause.Error()
}

func (e *AmbiguousWriteError) Unwrap() error {
	return e.Cause
}

func rateLimitFromHeaders(header http.Header) RateLimit {
	return RateLimit{
		Limit:      firstHeader(header, "X-Rate-Limit-Limit", "X-RateLimit-Limit"),
		Remaining:  firstHeader(header, "X-Rate-Limit-Remaining", "X-RateLimit-Remaining"),
		Reset:      firstHeader(header, "X-Rate-Limit-Reset", "X-RateLimit-Reset"),
		RetryAfter: retryAfterSeconds(header.Get("Retry-After")),
	}
}

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func retryAfterSeconds(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return seconds
	}
	if parsed, err := http.ParseTime(value); err == nil {
		seconds := int(time.Until(parsed).Seconds())
		if seconds > 0 {
			return seconds
		}
	}
	return 0
}

func normalizeJSON(value any) any {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case []any:
		for i := range typed {
			typed[i] = normalizeJSON(typed[i])
		}
		return typed
	case map[string]any:
		for key := range typed {
			typed[key] = normalizeJSON(typed[key])
		}
		return typed
	default:
		return value
	}
}

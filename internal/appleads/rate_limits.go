package appleads

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxRateLimitWait = 60 * time.Second
const maxRateLimitSeconds = int64(math.MaxInt64 / int64(time.Second))

type RateLimitError struct {
	Code      string
	RateLimit RateLimit
	Mutation  bool
	Cause     error
}

func (e *RateLimitError) Error() string {
	switch e.Code {
	case "RATE_LIMIT_COOLDOWN":
		return "Apple Ads mutation was not sent because the profile is rate limited"
	case "RATE_LIMIT_WAIT_CANCELED":
		return "Apple Ads rate-limit wait was canceled before dispatch"
	case "RATE_LIMIT_TIMING_INVALID":
		return "Apple Ads rate-limit timing exceeds the supported range; profile requests remain blocked until timing is verified and the server is restarted"
	default:
		return "Apple Ads rate-limit wait exceeds the available wait budget; request was not dispatched"
	}
}

func (e *RateLimitError) Unwrap() error { return e.Cause }

func rateLimitFromHeadersAt(header http.Header, now time.Time) RateLimit {
	result := RateLimit{
		Limit:     numericHeader(header, "RateLimit-Limit", "X-Rate-Limit-Limit", "X-RateLimit-Limit"),
		Remaining: numericHeader(header, "RateLimit-Remaining", "X-Rate-Limit-Remaining", "X-RateLimit-Remaining"),
		Reset:     numericHeader(header, "RateLimit-Reset", "X-Rate-Limit-Reset", "X-RateLimit-Reset"),
	}
	var resetUnsafe, retryUnsafe bool
	result.resetSeconds, result.resetSet, resetUnsafe = durationSeconds(header.Get("RateLimit-Reset"))
	result.RetryAfter, result.retrySet, retryUnsafe = durationSeconds(header.Get("Retry-After"))
	if !result.retrySet && !retryUnsafe {
		if date, err := http.ParseTime(strings.TrimSpace(header.Get("Retry-After"))); err == nil && date.After(now) {
			delta := date.Sub(now)
			seconds := ceilSeconds(delta)
			if seconds <= maxRateLimitSeconds {
				result.RetryAfter, result.retrySet = int(seconds), true
			} else {
				retryUnsafe = true
			}
		}
	}
	result.timingUnsafe = retryUnsafe || !result.retrySet && resetUnsafe
	return result
}

func numericHeader(header http.Header, names ...string) string {
	for _, name := range names {
		value := strings.TrimSpace(header.Get(name))
		if !decimalDigits(value) {
			continue
		}
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			return strconv.FormatUint(parsed, 10)
		}
	}
	return ""
}

func durationSeconds(value string) (int, bool, bool) {
	value = strings.TrimSpace(value)
	if !decimalDigits(value) {
		return 0, false, false
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed > uint64(maxRateLimitSeconds) || parsed > uint64(^uint(0)>>1) {
		return 0, false, true
	}
	return int(parsed), true, false
}

func decimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func ceilSeconds(duration time.Duration) int64 {
	seconds := int64(duration / time.Second)
	if duration%time.Second > 0 {
		seconds++
	}
	return seconds
}

func (rate RateLimit) retryDelay(attempt int) time.Duration {
	if rate.retrySet {
		return time.Duration(rate.RetryAfter) * time.Second
	}
	if rate.resetSet {
		return time.Duration(rate.resetSeconds) * time.Second
	}
	return retryDelay(attempt, 0)
}

func retryDelay(attempt, retryAfter int) time.Duration {
	if retryAfter > 0 && int64(retryAfter) <= maxRateLimitSeconds {
		return time.Duration(retryAfter) * time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 4 {
		attempt = 4
	}
	return (time.Second * 2) << (attempt - 1)
}

type rateLimiter struct {
	mu         sync.Mutex
	until      time.Time
	active     bool
	probing    bool
	generation uint64
	changed    chan struct{}
	rate       RateLimit
}

type rateProbe struct {
	active     bool
	generation uint64
}

type waitBudget struct {
	remaining time.Duration
}

func (c *Client) acquireRateLimit(ctx context.Context, mutation bool, budget *waitBudget) (rateProbe, error) {
	for {
		if err := ctx.Err(); err != nil {
			c.limits.mu.Lock()
			active, rate := c.limits.active, c.limits.rate
			c.limits.mu.Unlock()
			if active {
				return rateProbe{}, &RateLimitError{Code: "RATE_LIMIT_WAIT_CANCELED", RateLimit: rate, Mutation: mutation, Cause: err}
			}
			return rateProbe{}, err
		}
		c.limits.mu.Lock()
		if !c.limits.active {
			c.limits.mu.Unlock()
			return rateProbe{}, nil
		}
		delay := c.limits.until.Sub(c.now())
		rate := c.limits.rate
		if delay > 0 && !rate.timingUnsafe {
			rate.RetryAfter = int(ceilSeconds(delay))
		}
		if rate.timingUnsafe {
			c.limits.mu.Unlock()
			return rateProbe{}, &RateLimitError{Code: "RATE_LIMIT_TIMING_INVALID", RateLimit: rate, Mutation: mutation}
		}
		if mutation {
			c.limits.mu.Unlock()
			return rateProbe{}, &RateLimitError{Code: "RATE_LIMIT_COOLDOWN", RateLimit: rate, Mutation: true}
		}
		if delay <= 0 && !c.limits.probing {
			c.limits.probing = true
			c.limits.changed = make(chan struct{})
			probe := rateProbe{active: true, generation: c.limits.generation}
			c.limits.mu.Unlock()
			return probe, nil
		}
		changed := c.limits.changed
		c.limits.mu.Unlock()
		if changed == nil {
			if err := c.waitForRetry(ctx, delay, rate, budget); err != nil {
				return rateProbe{}, err
			}
		} else if err := c.waitForProbe(ctx, changed, rate, budget); err != nil {
			return rateProbe{}, err
		}
	}
}

func (c *Client) observeRateLimit(result Result, response *http.Response, probe rateProbe, attempt int) {
	c.limits.mu.Lock()
	defer c.limits.mu.Unlock()
	rate := result.RateLimit
	limited := response != nil && response.StatusCode == http.StatusTooManyRequests
	if remaining, err := strconv.ParseUint(rate.Remaining, 10, 64); err == nil && remaining < 5 {
		limited = true
	}
	if limited {
		delay := rate.retryDelay(attempt)
		if response.StatusCode != http.StatusTooManyRequests && rate.resetSet {
			delay = time.Duration(rate.resetSeconds) * time.Second
		}
		if rate.timingUnsafe {
			delay = maxRateLimitWait
		}
		until := c.now().Add(delay)
		if !c.limits.active || until.After(c.limits.until) || c.limits.rate.timingUnsafe {
			c.limits.until = until
			c.limits.rate = rate
		}
		if rate.timingUnsafe {
			c.limits.rate = rate
		}
		c.limits.active = true
		c.limits.generation++
	}
	if probe.active {
		if !limited && probe.generation == c.limits.generation {
			if response == nil {
				c.limits.until = c.now().Add(retryDelay(attempt, 0))
			} else {
				c.limits.active = false
			}
		}
		c.limits.probing = false
		close(c.limits.changed)
		c.limits.changed = nil
	}
}

func (c *Client) waitForRetry(ctx context.Context, delay time.Duration, rate RateLimit, budget *waitBudget) error {
	if rate.timingUnsafe {
		return &RateLimitError{Code: "RATE_LIMIT_TIMING_INVALID", RateLimit: rate}
	}
	if err := ctx.Err(); err != nil {
		return &RateLimitError{Code: "RATE_LIMIT_WAIT_CANCELED", RateLimit: rate, Cause: err}
	}
	if delay <= 0 {
		return nil
	}
	if delay > budget.remaining {
		return &RateLimitError{Code: "RATE_LIMIT_WAIT_EXCEEDED", RateLimit: rate}
	}
	if deadline, ok := ctx.Deadline(); ok && delay > deadline.Sub(c.now()) {
		return &RateLimitError{Code: "RATE_LIMIT_WAIT_EXCEEDED", RateLimit: rate, Cause: context.DeadlineExceeded}
	}
	started := c.now()
	if err := c.sleep(ctx, delay); err != nil {
		budget.remaining -= c.now().Sub(started)
		return &RateLimitError{Code: "RATE_LIMIT_WAIT_CANCELED", RateLimit: rate, Cause: err}
	}
	elapsed := c.now().Sub(started)
	if elapsed < delay {
		elapsed = delay
	}
	budget.remaining -= elapsed
	return nil
}

func (c *Client) waitForProbe(ctx context.Context, changed <-chan struct{}, rate RateLimit, budget *waitBudget) error {
	if budget.remaining <= 0 {
		return &RateLimitError{Code: "RATE_LIMIT_WAIT_EXCEEDED", RateLimit: rate}
	}
	started := c.now()
	timer := time.NewTimer(budget.remaining)
	defer timer.Stop()
	defer func() { budget.remaining -= c.now().Sub(started) }()
	select {
	case <-changed:
		return nil
	case <-ctx.Done():
		return &RateLimitError{Code: "RATE_LIMIT_WAIT_CANCELED", RateLimit: rate, Cause: ctx.Err()}
	case <-timer.C:
		return &RateLimitError{Code: "RATE_LIMIT_WAIT_EXCEEDED", RateLimit: rate}
	}
}

func apiErrorWithRateLimit(status int, data any, rate RateLimit) *APIError {
	err := apiError(status, data)
	err.RateLimit = rate
	return err
}

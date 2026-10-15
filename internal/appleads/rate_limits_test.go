package appleads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestRateLimitHeadersAreValidated(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 250000000, time.UTC)
	for _, test := range []struct {
		name      string
		headers   map[string]string
		limit     string
		remaining string
		reset     string
		delay     time.Duration
		unsafe    bool
	}{
		{"modern precedence", map[string]string{"RateLimit-Limit": "100", "X-Rate-Limit-Limit": "200", "RateLimit-Remaining": "004", "RateLimit-Reset": "37", "X-Rate-Limit-Reset": "1700000000"}, "100", "4", "37", 37 * time.Second, false},
		{"legacy output only", map[string]string{"X-RateLimit-Limit": "100", "X-Rate-Limit-Remaining": "4", "X-RateLimit-Reset": "1700000000"}, "100", "4", "1700000000", 2 * time.Second, false},
		{"retry preferred", map[string]string{"Retry-After": "37", "RateLimit-Reset": "9"}, "", "", "9", 37 * time.Second, false},
		{"zero retry preferred", map[string]string{"Retry-After": "0", "RateLimit-Reset": "9"}, "", "", "9", 0, false},
		{"date rounds up", map[string]string{"Retry-After": now.Add(37 * time.Second).Format(http.TimeFormat)}, "", "", "", 37 * time.Second, false},
		{"negative and injection rejected", map[string]string{"Retry-After": "-1", "RateLimit-Reset": "-9", "RateLimit-Limit": "secret", "RateLimit-Remaining": "2\nprivate"}, "", "", "", 2 * time.Second, false},
		{"malformed reset ignored", map[string]string{"RateLimit-Reset": "3.2", "Retry-After": "tomorrow"}, "", "", "", 2 * time.Second, false},
		{"overflow fails closed", map[string]string{"Retry-After": "999999999999999999999999"}, "", "", "", 2 * time.Second, true},
		{"duration overflow fails closed", map[string]string{"RateLimit-Reset": "9223372037"}, "", "", "9223372037", 2 * time.Second, true},
		{"valid retry beats invalid reset", map[string]string{"Retry-After": "37", "RateLimit-Reset": "999999999999999999999999"}, "", "", "", 37 * time.Second, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{}
			for name, value := range test.headers {
				header.Set(name, value)
			}
			rate := rateLimitFromHeadersAt(header, now)
			if rate.Limit != test.limit || rate.Remaining != test.remaining || rate.Reset != test.reset || rate.retryDelay(1) != test.delay || rate.timingUnsafe != test.unsafe {
				t.Fatalf("rate=%+v delay=%v", rate, rate.retryDelay(1))
			}
		})
	}
}

func TestReadRetryWaitBudgetAndTiming(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		headers   map[string]string
		failures  int32
		wantCalls int32
		wantDelay time.Duration
		wantCode  string
		deadline  time.Duration
	}{
		{"37 second retry", 429, map[string]string{"Retry-After": "37"}, 1, 2, 37 * time.Second, "", 0},
		{"37 second reset", 429, map[string]string{"RateLimit-Reset": "37"}, 1, 2, 37 * time.Second, "", 0},
		{"three attempts maximum", 429, nil, 10, 3, 6 * time.Second, "", 0},
		{"total wait budget", 429, map[string]string{"RateLimit-Reset": "37"}, 10, 2, 37 * time.Second, "RATE_LIMIT_WAIT_EXCEEDED", 0},
		{"long wait refused", 429, map[string]string{"Retry-After": "61"}, 1, 1, 0, "RATE_LIMIT_WAIT_EXCEEDED", 0},
		{"deadline refused before dispatch", 429, map[string]string{"Retry-After": "37"}, 1, 1, 0, "RATE_LIMIT_WAIT_EXCEEDED", 10 * time.Second},
		{"overflow refused", 429, map[string]string{"Retry-After": "999999999999999999999999"}, 1, 1, 0, "RATE_LIMIT_TIMING_INVALID", 0},
		{"server retry honors long header", 503, map[string]string{"Retry-After": "37"}, 1, 2, 37 * time.Second, "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls atomic.Int32
				client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
					if calls.Add(1) <= test.failures {
						return rateResponse(test.status, test.headers), nil
					}
					return rateResponse(200, map[string]string{"RateLimit-Remaining": "20"}), nil
				})
				ctx := context.Background()
				if test.deadline > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.deadline)
					defer cancel()
				}
				started := time.Now()
				result, err := client.Do(ctx, "", ACLs())
				if calls.Load() != test.wantCalls || time.Since(started) != test.wantDelay {
					t.Fatalf("calls=%d elapsed=%v err=%v", calls.Load(), time.Since(started), err)
				}
				if test.wantCode != "" {
					var rateErr *RateLimitError
					if !errors.As(err, &rateErr) || rateErr.Code != test.wantCode {
						t.Fatalf("error=%v", err)
					}
					if test.deadline > 0 && !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("deadline cause missing: %v", err)
					}
				} else if test.failures < test.wantCalls {
					if err != nil || result.RateLimit.Remaining != "20" {
						t.Fatalf("result=%+v error=%v", result, err)
					}
				} else {
					var apiErr *APIError
					if !errors.As(err, &apiErr) || apiErr.HTTPStatus != test.status {
						t.Fatalf("error=%v", err)
					}
				}
			})
		})
	}
}

func TestLowRemainingSharedCooldownAndSingleProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		probeStarted := make(chan struct{})
		finishProbe := make(chan struct{})
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			switch calls.Add(1) {
			case 1:
				return rateResponse(200, map[string]string{"RateLimit-Remaining": "4", "RateLimit-Reset": "37"}), nil
			case 2:
				close(probeStarted)
				<-finishProbe
			}
			return rateResponse(200, map[string]string{"RateLimit-Remaining": "100"}), nil
		})
		if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
			t.Fatal(err)
		}
		other := rateTestClient(t, func(*http.Request) (*http.Response, error) { return rateResponse(200, nil), nil })
		if _, err := other.Do(context.Background(), "", ACLs()); err != nil {
			t.Fatalf("unrelated profile blocked: %v", err)
		}
		mutation, _ := ResourceCreate("campaigns", map[string]any{"name": "synthetic"})
		_, err := client.Do(context.Background(), "1", mutation)
		var rateErr *RateLimitError
		var ambiguous *AmbiguousWriteError
		if !errors.As(err, &rateErr) || !rateErr.Mutation || errors.As(err, &ambiguous) || calls.Load() != 1 {
			t.Fatalf("mutation error=%v calls=%d", err, calls.Load())
		}
		var group sync.WaitGroup
		for range 8 {
			group.Go(func() {
				if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
					t.Error(err)
				}
			})
		}
		<-probeStarted
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatalf("concurrent probes escaped: %d", calls.Load())
		}
		_, err = client.Do(context.Background(), "1", mutation)
		if !errors.As(err, &rateErr) || calls.Load() != 2 {
			t.Fatalf("mutation during probe error=%v calls=%d", err, calls.Load())
		}
		close(finishProbe)
		group.Wait()
		if calls.Load() != 9 {
			t.Fatalf("calls=%d", calls.Load())
		}
	})
}

func TestRateLimitCancellationDoesNotDispatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return rateResponse(200, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "37"}), nil
		})
		_, _ = client.Do(context.Background(), "", ACLs())
		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan error, 1)
		go func() { _, err := client.Do(ctx, "", ACLs()); finished <- err }()
		synctest.Wait()
		cancel()
		err := <-finished
		if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
			t.Fatalf("error=%v calls=%d", err, calls.Load())
		}
	})
}

func TestRateDetailsSurviveAPIError(t *testing.T) {
	client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
		return rateResponse(403, map[string]string{"RateLimit-Limit": "100", "RateLimit-Remaining": "9", "RateLimit-Reset": "37"}), nil
	})
	_, err := client.Do(context.Background(), "", ACLs())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RateLimit.Limit != "100" || apiErr.RateLimit.Reset != "37" {
		t.Fatalf("error=%+v", err)
	}
}

func rateResponse(status int, headers map[string]string) *http.Response {
	header := http.Header{}
	for name, value := range headers {
		header.Set(name, value)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}
}

func rateTestClient(t *testing.T, transport func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	client, err := NewClientForTest(&fakeTokens{value: "synthetic"}, &http.Client{Transport: roundTripFunc(transport)}, "https://example.invalid/v1/")
	if err != nil {
		t.Fatal(fmt.Errorf("create test client: %w", err))
	}
	return client
}

func TestMutationCannotProbeExpiredCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return rateResponse(200, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "1"}), nil
			}
			return rateResponse(200, map[string]string{"RateLimit-Remaining": "100"}), nil
		})
		_, _ = client.Do(context.Background(), "", ACLs())
		time.Sleep(time.Second)
		mutation, _ := ResourceCreate("campaigns", map[string]any{"name": "synthetic"})
		_, err := client.Do(context.Background(), "1", mutation)
		var rateErr *RateLimitError
		if !errors.As(err, &rateErr) || !rateErr.Mutation || calls.Load() != 1 {
			t.Fatalf("mutation became probe: error=%v calls=%d", err, calls.Load())
		}
		if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Do(context.Background(), "1", mutation); err != nil || calls.Load() != 3 {
			t.Fatalf("healthy probe did not release cooldown: error=%v calls=%d", err, calls.Load())
		}
	})
}

func TestProbeWaitSharesTheSixtySecondBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		probeStarted := make(chan struct{})
		finishProbe := make(chan struct{})
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			switch calls.Add(1) {
			case 1:
				return rateResponse(200, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "1"}), nil
			case 2:
				close(probeStarted)
				<-finishProbe
			}
			return rateResponse(200, nil), nil
		})
		_, _ = client.Do(context.Background(), "", ACLs())
		probeDone := make(chan error, 1)
		go func() { _, err := client.Do(context.Background(), "", ACLs()); probeDone <- err }()
		<-probeStarted
		started := time.Now()
		_, err := client.Do(context.Background(), "", ACLs())
		var rateErr *RateLimitError
		if !errors.As(err, &rateErr) || rateErr.Code != "RATE_LIMIT_WAIT_EXCEEDED" || time.Since(started) != 60*time.Second || calls.Load() != 2 {
			t.Fatalf("error=%v elapsed=%v calls=%d", err, time.Since(started), calls.Load())
		}
		close(finishProbe)
		if err := <-probeDone; err != nil {
			t.Fatal(err)
		}
	})
}

func TestOverflowTimingNeverBecomesShortAutomaticCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return rateResponse(429, map[string]string{"Retry-After": "999999999999999999999999"}), nil
		})
		_, _ = client.Do(context.Background(), "", ACLs())
		time.Sleep(61 * time.Second)
		_, err := client.Do(context.Background(), "", ACLs())
		var rateErr *RateLimitError
		if !errors.As(err, &rateErr) || rateErr.Code != "RATE_LIMIT_TIMING_INVALID" || calls.Load() != 1 {
			t.Fatalf("unsafe early retry: error=%v calls=%d", err, calls.Load())
		}
	})
}

func TestMutation429PreservesTimingWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return rateResponse(429, map[string]string{"Retry-After": "37"}), nil
	})
	mutation, _ := ResourceCreate("campaigns", map[string]any{"name": "synthetic"})
	_, err := client.Do(context.Background(), "1", mutation)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Retryable || apiErr.RateLimit.RetryAfter != 37 || calls.Load() != 1 {
		t.Fatalf("error=%+v calls=%d", err, calls.Load())
	}
}

func TestProbeDoesNotClearNewerCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		firstStarted, finishFirst := make(chan struct{}), make(chan struct{})
		probeStarted, finishProbe := make(chan struct{}), make(chan struct{})
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			switch calls.Add(1) {
			case 1:
				close(firstStarted)
				<-finishFirst
				return rateResponse(200, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "37"}), nil
			case 2:
				return rateResponse(200, map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "1"}), nil
			case 3:
				close(probeStarted)
				<-finishProbe
			}
			return rateResponse(200, map[string]string{"RateLimit-Remaining": "100"}), nil
		})
		firstDone, probeDone := make(chan error, 1), make(chan error, 1)
		go func() { _, err := client.Do(context.Background(), "", ACLs()); firstDone <- err }()
		<-firstStarted
		_, _ = client.Do(context.Background(), "", ACLs())
		go func() { _, err := client.Do(context.Background(), "", ACLs()); probeDone <- err }()
		<-probeStarted
		close(finishFirst)
		if err := <-firstDone; err != nil {
			t.Fatal(err)
		}
		close(finishProbe)
		if err := <-probeDone; err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
			t.Fatal(err)
		}
		if time.Since(started) != 37*time.Second || calls.Load() != 4 {
			t.Fatalf("newer cooldown lost: elapsed=%v calls=%d", time.Since(started), calls.Load())
		}
	})
}

func TestRemainingFiveDoesNotThrottle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			return rateResponse(200, map[string]string{"RateLimit-Remaining": "5", "RateLimit-Reset": "37"}), nil
		})
		started := time.Now()
		for range 2 {
			if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
				t.Fatal(err)
			}
		}
		if time.Since(started) != 0 {
			t.Fatalf("unexpected throttle: %v", time.Since(started))
		}
	})
}

func TestRetryBackoffIsExponentialAndBounded(t *testing.T) {
	for attempt, want := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 16 * time.Second} {
		if got := retryDelay(attempt+1, 0); got != want {
			t.Fatalf("attempt=%d delay=%v want=%v", attempt+1, got, want)
		}
	}
}

func TestLowRemainingWithoutResetUsesFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		client := rateTestClient(t, func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return rateResponse(200, map[string]string{"RateLimit-Remaining": "4"}), nil
			}
			return rateResponse(200, map[string]string{"RateLimit-Remaining": "20"}), nil
		})
		if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		if _, err := client.Do(context.Background(), "", ACLs()); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 || time.Since(started) != 2*time.Second {
			t.Fatalf("calls=%d elapsed=%v", calls.Load(), time.Since(started))
		}
	})
}

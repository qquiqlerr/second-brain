package openrouter

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"time"
)

// HTTPError is returned by callers to communicate an HTTP-shaped failure
// without coupling them to a specific HTTP client.
type HTTPError struct {
	Status int
	Msg    string
}

func (e HTTPError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Msg) }

// RetryPolicy describes the back-off used by WithRetry.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      float64 // 0..1, fraction of computed delay added randomly
}

// DefaultRetryPolicy mirrors the values in the design spec (§5.3).
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 8 * time.Second, Jitter: 0.3}
}

var retryableStatuses = []int{429, 500, 502, 503, 504}

// WithRetry invokes op until it succeeds, the context is cancelled, or
// MaxAttempts is exhausted. Only retryable HTTP statuses and transient
// network errors are retried.
func WithRetry(ctx context.Context, p RetryPolicy, op func(context.Context) error) error {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	var lastErr error
	for attempt := range p.MaxAttempts {
		err := op(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// If the context was cancelled, surface that regardless of the error
		// returned by op — the caller cares about why we stopped.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return errors.Join(lastErr, ctxErr)
		}

		if !isRetryable(err) {
			return err
		}
		if attempt == p.MaxAttempts-1 {
			break
		}
		delay := backoffDelay(p, attempt)
		select {
		case <-ctx.Done():
			return errors.Join(lastErr, ctx.Err())
		case <-time.After(delay):
		}
	}
	return lastErr
}

func isRetryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if httpErr, ok := errors.AsType[HTTPError](err); ok {
		return slices.Contains(retryableStatuses, httpErr.Status)
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func backoffDelay(p RetryPolicy, attempt int) time.Duration {
	d := p.BaseDelay * (1 << attempt) //nolint:gosec // bounded by MaxAttempts
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	if p.Jitter > 0 {
		jitter := time.Duration(rand.Float64() * p.Jitter * float64(d))
		d += jitter
	}
	return d
}

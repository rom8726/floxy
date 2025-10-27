package floxy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateRetryDelay(t *testing.T) {
	tests := []struct {
		name         string
		strategy     RetryStrategy
		baseDelay    time.Duration
		retryAttempt int
		want         time.Duration
	}{
		{
			name:         "fixed strategy - first retry",
			strategy:     RetryStrategyFixed,
			baseDelay:    time.Second,
			retryAttempt: 1,
			want:         time.Second,
		},
		{
			name:         "fixed strategy - second retry",
			strategy:     RetryStrategyFixed,
			baseDelay:    time.Second,
			retryAttempt: 2,
			want:         time.Second,
		},
		{
			name:         "exponential strategy - first retry",
			strategy:     RetryStrategyExponential,
			baseDelay:    time.Second,
			retryAttempt: 1,
			want:         2 * time.Second, // base * 2^1
		},
		{
			name:         "exponential strategy - second retry",
			strategy:     RetryStrategyExponential,
			baseDelay:    time.Second,
			retryAttempt: 2,
			want:         4 * time.Second, // base * 2^2
		},
		{
			name:         "exponential strategy - third retry",
			strategy:     RetryStrategyExponential,
			baseDelay:    time.Second,
			retryAttempt: 3,
			want:         8 * time.Second, // base * 2^3
		},
		{
			name:         "exponential strategy with 100ms base - first retry",
			strategy:     RetryStrategyExponential,
			baseDelay:    100 * time.Millisecond,
			retryAttempt: 1,
			want:         200 * time.Millisecond, // 100ms * 2^1
		},
		{
			name:         "linear strategy - first retry",
			strategy:     RetryStrategyLinear,
			baseDelay:    time.Second,
			retryAttempt: 1,
			want:         time.Second, // base * 1
		},
		{
			name:         "linear strategy - second retry",
			strategy:     RetryStrategyLinear,
			baseDelay:    time.Second,
			retryAttempt: 2,
			want:         2 * time.Second, // base * 2
		},
		{
			name:         "linear strategy - third retry",
			strategy:     RetryStrategyLinear,
			baseDelay:    time.Second,
			retryAttempt: 3,
			want:         3 * time.Second, // base * 3
		},
		{
			name:         "linear strategy with 500ms base - second retry",
			strategy:     RetryStrategyLinear,
			baseDelay:    500 * time.Millisecond,
			retryAttempt: 2,
			want:         time.Second, // 500ms * 2
		},
		{
			name:         "default strategy (empty) - should use fixed",
			strategy:     0,
			baseDelay:    time.Second,
			retryAttempt: 5,
			want:         time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateRetryDelay(tt.strategy, tt.baseDelay, tt.retryAttempt)
			assert.Equalf(t, tt.want, got, "CalculateRetryDelay(%v, %v, %v)", tt.strategy, tt.baseDelay, tt.retryAttempt)
		})
	}
}

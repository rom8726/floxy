package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type JSONHandler struct {
	name string
	fn   func(ctx context.Context, stepCtx StepContext, data map[string]any) (map[string]any, error)
}

func NewJSONHandler(
	name string,
	fn func(ctx context.Context, stepCtx StepContext, data map[string]any) (map[string]any, error),
) *JSONHandler {
	return &JSONHandler{
		name: name,
		fn:   fn,
	}
}

func (h *JSONHandler) Name() string {
	return h.name
}

func (h *JSONHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	var data map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &data); err != nil {
			return nil, fmt.Errorf("unmarshal input: %w", err)
		}
	} else {
		data = make(map[string]any)
	}

	result, err := h.fn(ctx, stepCtx, data)
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

// === TypedHandler ===
type TypedHandler[IN, OUT any] struct {
	name string
	fn   func(ctx context.Context, stepCtx StepContext, input IN) (OUT, error)
}

func NewTypedHandler[I, O any](
	name string,
	fn func(ctx context.Context, stepCtx StepContext, input I) (O, error),
) *TypedHandler[I, O] {
	return &TypedHandler[I, O]{
		name: name,
		fn:   fn,
	}
}

func (h *TypedHandler[IN, OUT]) Name() string {
	return h.name
}

func (h *TypedHandler[IN, OUT]) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	var data IN
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, fmt.Errorf("unmarshal input: %w", err)
	}

	result, err := h.fn(ctx, stepCtx, data)
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

// === CalculateRetryDelay ===

func CalculateRetryDelay(strategy RetryStrategy, baseDelay time.Duration, retryAttempt int) time.Duration {
	switch strategy {
	case RetryStrategyExponential:
		// Exponential backoff: baseDelay * 2^retryAttempt
		multiplier := math.Pow(2, float64(retryAttempt))
		return time.Duration(float64(baseDelay) * multiplier)

	case RetryStrategyLinear:
		// Linear backoff: baseDelay * retryAttempt
		return baseDelay * time.Duration(retryAttempt)

	case RetryStrategyFixed:
		fallthrough
	default:
		// Fixed delay: always use baseDelay
		return baseDelay
	}
}

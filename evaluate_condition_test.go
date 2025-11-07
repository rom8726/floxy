package floxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateCondition(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		data     map[string]any
		expected bool
		hasError bool
	}{
		{
			name:     "simple_gt_true",
			expr:     "{{ gt .count 0 }}",
			data:     map[string]any{"count": 5},
			expected: true,
			hasError: false,
		},
		{
			name:     "simple_gt_false",
			expr:     "{{ gt .count 0 }}",
			data:     map[string]any{"count": 0},
			expected: false,
			hasError: false,
		},
		{
			name:     "simple_lt_true",
			expr:     "{{ lt .count 10 }}",
			data:     map[string]any{"count": 5},
			expected: true,
			hasError: false,
		},
		{
			name:     "simple_lt_false",
			expr:     "{{ lt .count 10 }}",
			data:     map[string]any{"count": 15},
			expected: false,
			hasError: false,
		},
		{
			name:     "simple_eq_true",
			expr:     "{{ eq .count 5 }}",
			data:     map[string]any{"count": 5},
			expected: true,
			hasError: false,
		},
		{
			name:     "simple_eq_false",
			expr:     "{{ eq .count 5 }}",
			data:     map[string]any{"count": 3},
			expected: false,
			hasError: false,
		},
		{
			name:     "simple_ne_true",
			expr:     "{{ ne .count 5 }}",
			data:     map[string]any{"count": 3},
			expected: true,
			hasError: false,
		},
		{
			name:     "simple_ne_false",
			expr:     "{{ ne .count 5 }}",
			data:     map[string]any{"count": 5},
			expected: false,
			hasError: false,
		},
		{
			name:     "simple_ge_true",
			expr:     "{{ ge .count 5 }}",
			data:     map[string]any{"count": 5},
			expected: true,
			hasError: false,
		},
		{
			name:     "simple_ge_false",
			expr:     "{{ ge .count 5 }}",
			data:     map[string]any{"count": 3},
			expected: false,
			hasError: false,
		},
		{
			name:     "simple_le_true",
			expr:     "{{ le .count 5 }}",
			data:     map[string]any{"count": 5},
			expected: true,
			hasError: false,
		},
		{
			name:     "simple_le_false",
			expr:     "{{ le .count 5 }}",
			data:     map[string]any{"count": 7},
			expected: false,
			hasError: false,
		},
		{
			name:     "string_eq_true",
			expr:     "{{ eq .status \"active\" }}",
			data:     map[string]any{"status": "active"},
			expected: true,
			hasError: false,
		},
		{
			name:     "string_eq_false",
			expr:     "{{ eq .status \"active\" }}",
			data:     map[string]any{"status": "inactive"},
			expected: false,
			hasError: false,
		},
		{
			name:     "boolean_true",
			expr:     "{{ eq .enabled true }}",
			data:     map[string]any{"enabled": true},
			expected: true,
			hasError: false,
		},
		{
			name:     "boolean_false",
			expr:     "{{ eq .enabled true }}",
			data:     map[string]any{"enabled": false},
			expected: false,
			hasError: false,
		},
		{
			name:     "nested_field",
			expr:     "{{ gt .user.age 18 }}",
			data:     map[string]any{"user": map[string]any{"age": 25}},
			expected: true,
			hasError: false,
		},
		{
			name:     "nested_field_false",
			expr:     "{{ gt .user.age 18 }}",
			data:     map[string]any{"user": map[string]any{"age": 16}},
			expected: false,
			hasError: false,
		},
		{
			name:     "missing_field",
			expr:     "{{ gt .count 0 }}",
			data:     map[string]any{"other": "value"},
			expected: false, // 0 > 0 is false
			hasError: false, // No error, just returns false
		},
		{
			name:     "invalid_expression",
			expr:     "{{ invalid .count 0 }}",
			data:     map[string]any{"count": 5},
			expected: false,
			hasError: true,
		},
		{
			name:     "malformed_template",
			expr:     "{{ gt .count }}",
			data:     map[string]any{"count": 5},
			expected: false,
			hasError: true,
		},
		{
			name:     "empty_expression",
			expr:     "",
			data:     map[string]any{"count": 5},
			expected: false,
			hasError: true,
		},
		{
			name:     "complex_condition",
			expr:     "{{ and (gt .count 0) (lt .count 100) }}",
			data:     map[string]any{"count": 50},
			expected: true, // and function works! 50 > 0 and 50 < 100 is true
			hasError: false,
		},
		{
			name:     "float_comparison",
			expr:     "{{ gt .price 10.5 }}",
			data:     map[string]any{"price": 15.7},
			expected: true,
			hasError: false,
		},
		{
			name:     "float_comparison_false",
			expr:     "{{ gt .price 10.5 }}",
			data:     map[string]any{"price": 5.2},
			expected: false,
			hasError: false,
		},
		{
			name:     "zero_comparison",
			expr:     "{{ eq .count 0 }}",
			data:     map[string]any{"count": 0},
			expected: true,
			hasError: false,
		},
		{
			name:     "negative_numbers",
			expr:     "{{ lt .count 0 }}",
			data:     map[string]any{"count": -5},
			expected: true,
			hasError: false,
		},
		{
			name:     "negative_numbers_false",
			expr:     "{{ lt .count 0 }}",
			data:     map[string]any{"count": 5},
			expected: false,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stepCtx := &executionContext{
				instanceID: 1,
				stepName:   "test_step",
				variables:  tt.data,
			}

			result, err := evaluateCondition(tt.expr, stepCtx)

			if tt.hasError {
				assert.Error(t, err, "Expected error for expression: %s", tt.expr)
			} else {
				assert.NoError(t, err, "Unexpected error for expression: %s", tt.expr)
				assert.Equal(t, tt.expected, result, "Expected %v, got %v for expression: %s", tt.expected, result, tt.expr)
			}
		})
	}
}

func TestEvaluateConditionWithRealWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
		WithEngineCancelInterval(time.Minute),
	)
	defer engine.Shutdown()

	engine.RegisterHandler(&ConditionTestHandler{})

	testCases := []struct {
		name        string
		expr        string
		input       json.RawMessage
		expectTrue  bool
		expectSteps []string
	}{
		{
			name:        "gt_condition_true",
			expr:        "{{ gt .count 0 }}",
			input:       json.RawMessage(`{"count": 5}`),
			expectTrue:  true,
			expectSteps: []string{"start", "condition", "next_step"},
		},
		{
			name:        "gt_condition_false",
			expr:        "{{ gt .count 0 }}",
			input:       json.RawMessage(`{"count": 0}`),
			expectTrue:  false,
			expectSteps: []string{"start", "condition", "else_step"},
		},
		{
			name:        "lt_condition_true",
			expr:        "{{ lt .count 10 }}",
			input:       json.RawMessage(`{"count": 5}`),
			expectTrue:  true,
			expectSteps: []string{"start", "condition", "next_step"},
		},
		{
			name:        "lt_condition_false",
			expr:        "{{ lt .count 10 }}",
			input:       json.RawMessage(`{"count": 15}`),
			expectTrue:  false,
			expectSteps: []string{"start", "condition", "else_step"},
		},
		{
			name:        "eq_condition_true",
			expr:        "{{ eq .status \"active\" }}",
			input:       json.RawMessage(`{"status": "active"}`),
			expectTrue:  true,
			expectSteps: []string{"start", "condition", "next_step"},
		},
		{
			name:        "eq_condition_false",
			expr:        "{{ eq .status \"active\" }}",
			input:       json.RawMessage(`{"status": "inactive"}`),
			expectTrue:  false,
			expectSteps: []string{"start", "condition", "else_step"},
		},
		{
			name:        "nested_field_true",
			expr:        "{{ gt .user.age 18 }}",
			input:       json.RawMessage(`{"user": {"age": 25}}`),
			expectTrue:  true,
			expectSteps: []string{"start", "condition", "next_step"},
		},
		{
			name:        "nested_field_false",
			expr:        "{{ gt .user.age 18 }}",
			input:       json.RawMessage(`{"user": {"age": 16}}`),
			expectTrue:  false,
			expectSteps: []string{"start", "condition", "else_step"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			workflowDef, err := NewBuilder("condition_test", 1).
				Step("start", "condition-test", WithStepMaxRetries(1)).
				Condition("condition", tc.expr, func(elseBranch *Builder) {
					elseBranch.Step("else_step", "condition-test", WithStepMaxRetries(1))
				}).
				Then("next_step", "condition-test", WithStepMaxRetries(1)).
				Build()

			require.NoError(t, err)
			err = engine.RegisterWorkflow(ctx, workflowDef)
			require.NoError(t, err)

			instanceID, err := engine.Start(ctx, "condition_test-v1", tc.input)
			require.NoError(t, err)

			for i := 0; i < 10; i++ {
				empty, err := engine.ExecuteNext(ctx, "worker1")
				require.NoError(t, err)
				if empty {
					break
				}
			}

			steps, err := engine.GetSteps(ctx, instanceID)
			require.NoError(t, err)

			stepNames := make([]string, len(steps))
			for i, step := range steps {
				stepNames[i] = step.StepName
			}

			for _, expectedStep := range tc.expectSteps {
				assert.Contains(t, stepNames, expectedStep, "Expected step %s to be executed", expectedStep)
			}

			if tc.expectTrue {
				assert.NotContains(t, stepNames, "else_step", "Else step should not be executed when condition is true")
			} else {
				assert.NotContains(t, stepNames, "next_step", "Next step should not be executed when condition is false")
			}
		})
	}
}

type ConditionTestHandler struct{}

func (h *ConditionTestHandler) Name() string { return "condition-test" }

func (h *ConditionTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Just pass through the input
	return input, nil
}

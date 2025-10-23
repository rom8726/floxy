package floxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//type MockRefundHandler struct {
//	executionCount int
//}
//
//func (h *MockRefundHandler) Name() string {
//	return "refund_handler"
//}
//
//func (h *MockRefundHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
//	h.executionCount++
//
//	// Simulate refund success
//	return json.RawMessage(`{"status": "refunded", "amount": 100}`), nil
//}
//
//type MockReleaseHandler struct {
//	executionCount int
//}
//
//func (h *MockReleaseHandler) Name() string {
//	return "release_handler"
//}
//
//func (h *MockReleaseHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
//	h.executionCount++
//
//	// Simulate release success
//	return json.RawMessage(`{"status": "released", "items": 5}`), nil
//}

func TestCompensationFlow_NonIdempotentSteps(t *testing.T) {
	// Create a workflow with non-idempotent compensation steps
	workflow, err := NewBuilder("compensation-test", 1).
		Step("step1", "handler1", WithStepMaxRetries(2)).
		OnFailure("compensation1", "compensation_handler1", WithStepNoIdempotent()).
		Then("step2", "handler2", WithStepMaxRetries(1)).
		OnFailure("compensation2", "compensation_handler2", WithStepNoIdempotent()).
		Then("step3", "handler3", WithStepMaxRetries(1)).
		OnFailure("compensation3", "compensation_handler3", WithStepNoIdempotent()).
		Build()

	require.NoError(t, err)

	// Verify workflow structure
	assert.Equal(t, "step1", workflow.Definition.Steps["step1"].Name)
	assert.Equal(t, "compensation1", workflow.Definition.Steps["step1"].OnFailure)
	assert.True(t, workflow.Definition.Steps["compensation1"].NoIdempotent)
	assert.Equal(t, 1, workflow.Definition.Steps["compensation1"].MaxRetries)

	assert.Equal(t, "step2", workflow.Definition.Steps["step2"].Name)
	assert.Equal(t, "compensation2", workflow.Definition.Steps["step2"].OnFailure)
	assert.True(t, workflow.Definition.Steps["compensation2"].NoIdempotent)
	assert.Equal(t, 1, workflow.Definition.Steps["compensation2"].MaxRetries)

	assert.Equal(t, "step3", workflow.Definition.Steps["step3"].Name)
	assert.Equal(t, "compensation3", workflow.Definition.Steps["step3"].OnFailure)
	assert.True(t, workflow.Definition.Steps["compensation3"].NoIdempotent)
	assert.Equal(t, 1, workflow.Definition.Steps["compensation3"].MaxRetries)
}

func TestCompensationFlow_NonIdempotentSteps_Integration(t *testing.T) {
	// Create a workflow with non-idempotent compensation
	workflow, err := NewBuilder("non-idempotent-compensation", 1).
		Step("payment", "payment_handler", WithStepMaxRetries(1)).
		OnFailure("refund", "refund_handler", WithStepNoIdempotent()).
		Then("inventory", "inventory_handler", WithStepMaxRetries(1)).
		OnFailure("release", "release_handler", WithStepNoIdempotent()).
		Build()

	require.NoError(t, err)

	// Verify that compensation steps are marked as non-idempotent
	refundStep := workflow.Definition.Steps["refund"]
	assert.True(t, refundStep.NoIdempotent, "Refund step should be non-idempotent")
	assert.Equal(t, 1, refundStep.MaxRetries, "Non-idempotent step should have MaxRetries=1")

	releaseStep := workflow.Definition.Steps["release"]
	assert.True(t, releaseStep.NoIdempotent, "Release step should be non-idempotent")
	assert.Equal(t, 1, releaseStep.MaxRetries, "Non-idempotent step should have MaxRetries=1")

	// Verify that regular steps are not affected
	paymentStep := workflow.Definition.Steps["payment"]
	assert.False(t, paymentStep.NoIdempotent, "Payment step should be idempotent")
	assert.Equal(t, 1, paymentStep.MaxRetries, "Payment step should have configured MaxRetries")

	inventoryStep := workflow.Definition.Steps["inventory"]
	assert.False(t, inventoryStep.NoIdempotent, "Inventory step should be idempotent")
	assert.Equal(t, 1, inventoryStep.MaxRetries, "Inventory step should have configured MaxRetries")
}

func TestCompensationFlow_NonIdempotentSteps_WithStepNoIdempotent(t *testing.T) {
	// Test that WithStepNoIdempotent correctly sets NoIdempotent=true and MaxRetries=1
	step := &StepDefinition{
		Name:       "test-step",
		Type:       StepTypeTask,
		Handler:    "test-handler",
		MaxRetries: 5, // This should be overridden
	}

	// Apply the option
	WithStepNoIdempotent()(step)

	// Verify the changes
	assert.True(t, step.NoIdempotent, "Step should be marked as non-idempotent")
	assert.Equal(t, 1, step.MaxRetries, "MaxRetries should be set to 1 for non-idempotent steps")
}

func TestCompensationFlow_NonIdempotentSteps_WithStepMaxRetries(t *testing.T) {
	// Test that WithStepMaxRetries cannot be applied to non-idempotent steps
	step := &StepDefinition{
		Name:         "test-step",
		Type:         StepTypeTask,
		Handler:      "test-handler",
		NoIdempotent: true,
		MaxRetries:   1,
	}

	// Try to apply WithStepMaxRetries to a non-idempotent step
	WithStepMaxRetries(5)(step)

	// Verify that MaxRetries was not changed
	assert.Equal(t, 1, step.MaxRetries, "MaxRetries should not be changed for non-idempotent steps")
	assert.True(t, step.NoIdempotent, "Step should remain non-idempotent")
}

func TestCompensationFlow_NonIdempotentSteps_WithStepNoIdempotent_Behavior(t *testing.T) {
	// Test the behavior of WithStepNoIdempotent option

	// Create a step definition
	step := &StepDefinition{
		Name:       "test-step",
		Type:       StepTypeTask,
		Handler:    "test-handler",
		MaxRetries: 5, // This should be overridden
	}

	// Apply WithStepNoIdempotent
	WithStepNoIdempotent()(step)

	// Verify the changes
	assert.True(t, step.NoIdempotent, "Step should be marked as non-idempotent")
	assert.Equal(t, 1, step.MaxRetries, "MaxRetries should be set to 1 for non-idempotent steps")

	// Test that WithStepMaxRetries cannot override non-idempotent steps
	WithStepMaxRetries(10)(step)

	// MaxRetries should remain 1
	assert.Equal(t, 1, step.MaxRetries, "MaxRetries should remain 1 for non-idempotent steps")
	assert.True(t, step.NoIdempotent, "Step should remain non-idempotent")
}

package rolldepth

import (
	"context"
	"testing"
)

func TestRollbackDepthPlugin_OnRollbackStepChain(t *testing.T) {
	plugin := New()

	tests := []struct {
		name       string
		instanceID int64
		stepName   string
		depth      int
		expected   int
	}{
		{
			name:       "single call",
			instanceID: 1,
			stepName:   "step1",
			depth:      0,
			expected:   0,
		},
		{
			name:       "increasing depth",
			instanceID: 1,
			stepName:   "step2",
			depth:      1,
			expected:   1,
		},
		{
			name:       "max depth",
			instanceID: 1,
			stepName:   "step3",
			depth:      5,
			expected:   5,
		},
		{
			name:       "lower depth (should not update max)",
			instanceID: 1,
			stepName:   "step4",
			depth:      2,
			expected:   5, // max should remain 5
		},
		{
			name:       "new instance",
			instanceID: 2,
			stepName:   "step1",
			depth:      3,
			expected:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := plugin.OnRollbackStepChain(ctx, tt.instanceID, tt.stepName, tt.depth)
			if err != nil {
				t.Fatalf("OnRollbackStepChain() error = %v", err)
			}

			maxDepth := plugin.GetMaxDepth(tt.instanceID)
			if maxDepth != tt.expected {
				t.Errorf("GetMaxDepth() = %v, want %v", maxDepth, tt.expected)
			}
		})
	}
}

func TestRollbackDepthPlugin_ResetMaxDepth(t *testing.T) {
	plugin := New()
	instanceID := int64(1)

	ctx := context.Background()
	_ = plugin.OnRollbackStepChain(ctx, instanceID, "step1", 5)

	maxDepth := plugin.GetMaxDepth(instanceID)
	if maxDepth != 5 {
		t.Errorf("GetMaxDepth() = %v, want 5", maxDepth)
	}

	plugin.ResetMaxDepth(instanceID)

	maxDepth = plugin.GetMaxDepth(instanceID)
	if maxDepth != 0 {
		t.Errorf("GetMaxDepth() after reset = %v, want 0", maxDepth)
	}
}

func TestRollbackDepthPlugin_GetAllMaxDepths(t *testing.T) {
	plugin := New()
	ctx := context.Background()

	_ = plugin.OnRollbackStepChain(ctx, 1, "step1", 3)
	_ = plugin.OnRollbackStepChain(ctx, 2, "step1", 5)
	_ = plugin.OnRollbackStepChain(ctx, 3, "step1", 2)

	allDepths := plugin.GetAllMaxDepths()

	if len(allDepths) != 3 {
		t.Errorf("GetAllMaxDepths() returned %d entries, want 3", len(allDepths))
	}

	if allDepths[1] != 3 {
		t.Errorf("GetAllMaxDepths()[1] = %v, want 3", allDepths[1])
	}
	if allDepths[2] != 5 {
		t.Errorf("GetAllMaxDepths()[2] = %v, want 5", allDepths[2])
	}
	if allDepths[3] != 2 {
		t.Errorf("GetAllMaxDepths()[3] = %v, want 2", allDepths[3])
	}
}

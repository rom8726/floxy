package floxy

import (
	"context"
)

type Monitor interface {
	GetWorkflowStats(ctx context.Context) ([]WorkflowStats, error)
}

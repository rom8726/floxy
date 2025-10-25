package floxy

import (
	"context"
)

type IEngine interface {
	MakeHumanDecision(
		ctx context.Context,
		stepID int64,
		decidedBy string,
		decision HumanDecision,
		comment *string,
	) error
	CancelWorkflow(
		ctx context.Context,
		instanceID int64,
		requestedBy string,
		reason string,
	) error
	AbortWorkflow(
		ctx context.Context,
		instanceID int64,
		requestedBy string,
		reason string,
	) error
}

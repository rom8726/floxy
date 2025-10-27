package floxy

import (
	"context"
	"encoding/json"
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
	// RequeueFromDLQ extracts a record from DLQ and enqueues the step again.
	// If newInput is provided, it will override step input before enqueueing.
	RequeueFromDLQ(
		ctx context.Context,
		dlqID int64,
		newInput *json.RawMessage,
	) error
}

package floxy

import (
	"context"
	"encoding/json"
)

type StepHandler interface {
	Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error)
	Name() string
}

type StepContext interface {
	InstanceID() int64
	StepName() string
	RetryCount() int
	GetVariable(key string) (string, bool)
	//SetVariable(key string, value string)
}

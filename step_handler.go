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
	CloneData() map[string]any
	GetVariable(key string) (any, bool)
	GetVariableAsString(key string) (string, bool)
}

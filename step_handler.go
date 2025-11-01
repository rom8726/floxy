package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
)

type StepHandler interface {
	Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error)
	Name() string
}

type StepContext interface {
	InstanceID() int64
	StepName() string
	IdempotencyKey() string
	RetryCount() int
	CloneData() map[string]any
	GetVariable(key string) (any, bool)
	GetVariableAsString(key string) (string, bool)
}

type noPanicStepHandler struct {
	handler StepHandler
}

func wrapProcessPanicHandler(handler StepHandler) *noPanicStepHandler {
	return &noPanicStepHandler{handler: handler}
}

func (handler *noPanicStepHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (out json.RawMessage, errRes error) {
	defer func() {
		if r := recover(); r != nil {
			errRes = fmt.Errorf("panic in handler %q: %v\n%s", handler.Name(), r, debug.Stack())
		}
	}()

	return handler.handler.Execute(ctx, stepCtx, input)
}

func (handler *noPanicStepHandler) Name() string {
	return handler.handler.Name()
}

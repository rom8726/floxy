package floxy

import (
	"fmt"
	"sync"
)

var _ StepContext = (*executionContext)(nil)

type executionContext struct {
	instanceID     int64
	stepName       string
	idempotencyKey string
	retryCount     int
	variables      map[string]any
	mu             sync.RWMutex
}

func (c *executionContext) InstanceID() int64 {
	return c.instanceID
}

func (c *executionContext) StepName() string {
	return c.stepName
}

func (c *executionContext) IdempotencyKey() string {
	return c.idempotencyKey
}

func (c *executionContext) RetryCount() int {
	return c.retryCount
}

func (c *executionContext) CloneData() map[string]any {
	result := make(map[string]any, len(c.variables))
	for k, v := range c.variables {
		result[k] = v
	}

	return result
}

func (c *executionContext) GetVariable(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.variables[key]

	return val, ok
}

func (c *executionContext) GetVariableAsString(key string) (string, bool) {
	val, ok := c.GetVariable(key)
	if !ok {
		return "", false
	}

	return fmt.Sprint(val), true
}

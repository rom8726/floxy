package floxy

import (
	"sync"
)

var _ StepContext = (*executionContext)(nil)

type executionContext struct {
	instanceID int64
	stepName   string
	retryCount int
	variables  map[string]string
	mu         sync.RWMutex
}

func (c *executionContext) InstanceID() int64 {
	return c.instanceID
}

func (c *executionContext) StepName() string {
	return c.stepName
}

func (c *executionContext) RetryCount() int {
	return c.retryCount
}

func (c *executionContext) GetVariable(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.variables[key]

	return val, ok
}

//func (c *executionContext) SetVariable(key string, value string) {
//	c.mu.Lock()
//	defer c.mu.Unlock()
//	c.variables[key] = value
//}

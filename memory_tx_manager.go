package floxy

import (
	"context"
)

type MemoryTxManager struct{}

func NewMemoryTxManager() *MemoryTxManager {
	return &MemoryTxManager{}
}

func (m *MemoryTxManager) ReadCommitted(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (m *MemoryTxManager) RepeatableRead(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

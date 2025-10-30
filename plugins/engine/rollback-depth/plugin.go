package rolldepth

import (
	"context"
	"log/slog"
	"sync"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*RollbackDepthPlugin)(nil)

type RollbackDepthPlugin struct {
	floxy.BasePlugin

	mu                 sync.RWMutex
	maxDepthByInstance map[int64]int
}

func New() *RollbackDepthPlugin {
	return &RollbackDepthPlugin{
		BasePlugin:         floxy.NewBasePlugin("rollback-depth", floxy.PriorityNormal),
		maxDepthByInstance: make(map[int64]int),
	}
}

func (p *RollbackDepthPlugin) OnRollbackStepChain(
	ctx context.Context,
	instanceID int64,
	stepName string,
	depth int,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentMax, exists := p.maxDepthByInstance[instanceID]
	if !exists || depth > currentMax {
		p.maxDepthByInstance[instanceID] = depth
		slog.Info("[floxy] rollback depth updated",
			"instance_id", instanceID,
			"step_name", stepName,
			"depth", depth,
			"max_depth", depth,
		)
	} else {
		slog.Debug("[floxy] rollback step chain",
			"instance_id", instanceID,
			"step_name", stepName,
			"depth", depth,
			"max_depth", currentMax,
		)
	}

	return nil
}

func (p *RollbackDepthPlugin) GetMaxDepth(instanceID int64) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.maxDepthByInstance[instanceID]
}

func (p *RollbackDepthPlugin) ResetMaxDepth(instanceID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.maxDepthByInstance, instanceID)
}

func (p *RollbackDepthPlugin) GetAllMaxDepths() map[int64]int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[int64]int, len(p.maxDepthByInstance))
	for k, v := range p.maxDepthByInstance {
		result[k] = v
	}

	return result
}

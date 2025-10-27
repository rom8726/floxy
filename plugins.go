package floxy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// Plugin represents a lifecycle hook system for workflows
type Plugin interface {
	// Name returns unique plugin identifier
	Name() string

	// Priority determines execution order (higher = earlier)
	Priority() int

	// Lifecycle hooks
	OnWorkflowStart(ctx context.Context, instance *WorkflowInstance) error
	OnWorkflowComplete(ctx context.Context, instance *WorkflowInstance) error
	OnWorkflowFailed(ctx context.Context, instance *WorkflowInstance) error
	OnStepStart(ctx context.Context, step *WorkflowStep) error
	OnStepComplete(ctx context.Context, step *WorkflowStep) error
	OnStepFailed(ctx context.Context, step *WorkflowStep, err error) error
}

// PluginManager manages plugin lifecycle
type PluginManager struct {
	plugins []Plugin
	mu      sync.RWMutex
}

func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins: make([]Plugin, 0),
	}
}

func (pm *PluginManager) Register(plugin Plugin) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.plugins = append(pm.plugins, plugin)

	sort.Slice(pm.plugins, func(i, j int) bool {
		return pm.plugins[i].Priority() < pm.plugins[j].Priority()
	})
}

func (pm *PluginManager) ExecuteWorkflowStart(ctx context.Context, instance *WorkflowInstance) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if err := plugin.OnWorkflowStart(ctx, instance); err != nil {
			return fmt.Errorf("plugin %s failed: %w", plugin.Name(), err)
		}
	}

	return nil
}

func (pm *PluginManager) ExecuteWorkflowComplete(ctx context.Context, instance *WorkflowInstance) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if err := plugin.OnWorkflowComplete(ctx, instance); err != nil {
			slog.Error("[floxy] plugin error on workflow complete", "plugin", plugin.Name(), "error", err)
		}
	}

	return nil
}

func (pm *PluginManager) ExecuteWorkflowFailed(ctx context.Context, instance *WorkflowInstance) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if err := plugin.OnWorkflowFailed(ctx, instance); err != nil {
			slog.Error("[floxy] plugin error on workflow failed", "plugin", plugin.Name(), "error", err)
		}
	}

	return nil
}

func (pm *PluginManager) ExecuteStepStart(ctx context.Context, step *WorkflowStep) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if err := plugin.OnStepStart(ctx, step); err != nil {
			return fmt.Errorf("plugin %s failed: %w", plugin.Name(), err)
		}
	}

	return nil
}

func (pm *PluginManager) ExecuteStepComplete(ctx context.Context, step *WorkflowStep) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if err := plugin.OnStepComplete(ctx, step); err != nil {
			slog.Error("[floxy] plugin error on step complete", "plugin", plugin.Name(), "error", err)
		}
	}

	return nil
}

func (pm *PluginManager) ExecuteStepFailed(ctx context.Context, step *WorkflowStep, err error) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if pluginErr := plugin.OnStepFailed(ctx, step, err); pluginErr != nil {
			slog.Error("[floxy] plugin error on step failed", "plugin", plugin.Name(), "error", err)
		}
	}

	return nil
}

package floxy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

type PluginPriority int

const (
	PriorityLow    PluginPriority = 0
	PriorityNormal PluginPriority = 50
	PriorityHigh   PluginPriority = 100
)

// Plugin represents a lifecycle hook system for workflows
type Plugin interface {
	// Name returns unique plugin identifier
	Name() string

	// Priority determines execution order (higher = earlier)
	Priority() PluginPriority

	// Lifecycle hooks
	OnWorkflowStart(ctx context.Context, instance *WorkflowInstance) error
	OnWorkflowComplete(ctx context.Context, instance *WorkflowInstance) error
	OnWorkflowFailed(ctx context.Context, instance *WorkflowInstance) error
	OnStepStart(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep) error
	OnStepComplete(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep) error
	OnStepFailed(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep, err error) error
}

// BasePlugin provides default no-op implementations
type BasePlugin struct {
	name     string
	priority PluginPriority
}

func NewBasePlugin(name string, priority PluginPriority) BasePlugin {
	return BasePlugin{name: name, priority: priority}
}

func (p BasePlugin) Name() string             { return p.name }
func (p BasePlugin) Priority() PluginPriority { return p.priority }
func (p BasePlugin) OnWorkflowStart(context.Context, *WorkflowInstance) error {
	return nil
}
func (p BasePlugin) OnWorkflowComplete(context.Context, *WorkflowInstance) error {
	return nil
}
func (p BasePlugin) OnWorkflowFailed(context.Context, *WorkflowInstance) error {
	return nil
}
func (p BasePlugin) OnStepStart(context.Context, *WorkflowInstance, *WorkflowStep) error { return nil }
func (p BasePlugin) OnStepComplete(context.Context, *WorkflowInstance, *WorkflowStep) error {
	return nil
}
func (p BasePlugin) OnStepFailed(context.Context, *WorkflowInstance, *WorkflowStep, error) error {
	return nil
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

func (pm *PluginManager) ExecuteStepStart(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if err := plugin.OnStepStart(ctx, instance, step); err != nil {
			return fmt.Errorf("plugin %s failed: %w", plugin.Name(), err)
		}
	}

	return nil
}

func (pm *PluginManager) ExecuteStepComplete(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if err := plugin.OnStepComplete(ctx, instance, step); err != nil {
			slog.Error("[floxy] plugin error on step complete", "plugin", plugin.Name(), "error", err)
		}
	}

	return nil
}

func (pm *PluginManager) ExecuteStepFailed(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep, err error) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, plugin := range pm.plugins {
		if pluginErr := plugin.OnStepFailed(ctx, instance, step, err); pluginErr != nil {
			slog.Error("[floxy] plugin error on step failed", "plugin", plugin.Name(), "error", err)
		}
	}

	return nil
}

package api

import (
	"context"

	"github.com/rom8726/floxy"
)

type APIService struct {
	store floxy.Store
}

func NewAPIService(store floxy.Store) *APIService {
	return &APIService{
		store: store,
	}
}

func (a *APIService) GetWorkflowDefinitions(ctx context.Context) ([]floxy.WorkflowDefinition, error) {
	definitions, err := a.store.GetWorkflowDefinitions(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]floxy.WorkflowDefinition, len(definitions))
	for _, def := range definitions {
		result = append(result, *def)
	}

	return result, nil
}

func (a *APIService) GetWorkflowDefinition(ctx context.Context, id string) (*floxy.WorkflowDefinition, error) {
	return a.store.GetWorkflowDefinition(ctx, id)
}

func (a *APIService) GetWorkflowInstances(ctx context.Context, workflowID string) ([]floxy.WorkflowInstance, error) {
	instances, err := a.store.GetWorkflowInstances(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	result := make([]floxy.WorkflowInstance, len(instances))
	for _, instance := range instances {
		result = append(result, *instance)
	}

	return result, nil
}

func (a *APIService) GetAllWorkflowInstances(ctx context.Context) ([]floxy.WorkflowInstance, error) {
	instances, err := a.store.GetAllWorkflowInstances(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]floxy.WorkflowInstance, len(instances))
	for _, instance := range instances {
		result = append(result, *instance)
	}

	return result, nil
}

func (a *APIService) GetWorkflowInstance(ctx context.Context, instanceID int64) (*floxy.WorkflowInstance, error) {
	return a.store.GetInstance(ctx, instanceID)
}

func (a *APIService) GetWorkflowSteps(ctx context.Context, instanceID int64) ([]floxy.WorkflowStep, error) {
	steps, err := a.store.GetWorkflowSteps(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	result := make([]floxy.WorkflowStep, len(steps))
	for _, step := range steps {
		result = append(result, *step)
	}

	return result, nil
}

func (a *APIService) GetWorkflowEvents(ctx context.Context, instanceID int64) ([]floxy.WorkflowEvent, error) {
	events, err := a.store.GetWorkflowEvents(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	result := make([]floxy.WorkflowEvent, len(events))
	for _, event := range events {
		result = append(result, *event)
	}

	return result, nil
}

func (a *APIService) GetSummaryStats(ctx context.Context) (*floxy.SummaryStats, error) {
	return a.store.GetSummaryStats(ctx)
}

func (a *APIService) GetActiveInstances(ctx context.Context) ([]floxy.ActiveWorkflowInstance, error) {
	instances, err := a.store.GetActiveInstances(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]floxy.ActiveWorkflowInstance, len(instances))
	for _, instance := range instances {
		result = append(result, *instance)
	}

	return result, nil
}

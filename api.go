package floxy

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type API struct {
	store Store
}

func NewAPI(pool *pgxpool.Pool) *API {
	return &API{store: NewStore(pool)}
}

func (a *API) GetWorkflowDefinitions(ctx context.Context) ([]WorkflowDefinition, error) {
	definitions, err := a.store.GetWorkflowDefinitions(ctx)
	if err != nil {
		return nil, err
	}

	var result []WorkflowDefinition
	for _, def := range definitions {
		result = append(result, *def)
	}

	return result, nil
}

func (a *API) GetWorkflowDefinition(ctx context.Context, id string) (*WorkflowDefinition, error) {
	return a.store.GetWorkflowDefinition(ctx, id)
}

func (a *API) GetWorkflowInstances(ctx context.Context, workflowID string) ([]WorkflowInstance, error) {
	instances, err := a.store.GetWorkflowInstances(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	var result []WorkflowInstance
	for _, instance := range instances {
		result = append(result, *instance)
	}

	return result, nil
}

func (a *API) GetAllWorkflowInstances(ctx context.Context) ([]WorkflowInstance, error) {
	instances, err := a.store.GetAllWorkflowInstances(ctx)
	if err != nil {
		return nil, err
	}

	var result []WorkflowInstance
	for _, instance := range instances {
		result = append(result, *instance)
	}

	return result, nil
}

func (a *API) GetWorkflowInstance(ctx context.Context, instanceID int64) (*WorkflowInstance, error) {
	return a.store.GetInstance(ctx, instanceID)
}

func (a *API) GetWorkflowSteps(ctx context.Context, instanceID int64) ([]WorkflowStep, error) {
	steps, err := a.store.GetWorkflowSteps(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	var result []WorkflowStep
	for _, step := range steps {
		result = append(result, *step)
	}

	return result, nil
}

func (a *API) GetWorkflowEvents(ctx context.Context, instanceID int64) ([]WorkflowEvent, error) {
	events, err := a.store.GetWorkflowEvents(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	var result []WorkflowEvent
	for _, event := range events {
		result = append(result, *event)
	}

	return result, nil
}

func (a *API) GetSummaryStats(ctx context.Context) (*SummaryStats, error) {
	return a.store.GetSummaryStats(ctx)
}

func (a *API) GetActiveInstances(ctx context.Context) ([]ActiveWorkflowInstance, error) {
	instances, err := a.store.GetActiveInstances(ctx)
	if err != nil {
		return nil, err
	}

	var result []ActiveWorkflowInstance
	for _, instance := range instances {
		result = append(result, *instance)
	}

	return result, nil
}

package dlq

import (
	"encoding/json"
)

type ListRequest struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

type ListResponse struct {
	Items    []DeadLetterResponse `json:"items"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Total    int64                `json:"total"`
}

type DeadLetterResponse struct {
	ID         int64           `json:"id"`
	InstanceID int64           `json:"instance_id"`
	WorkflowID string          `json:"workflow_id"`
	StepID     int64           `json:"step_id"`
	StepName   string          `json:"step_name"`
	StepType   string          `json:"step_type"`
	Input      json.RawMessage `json:"input"`
	Error      *string         `json:"error"`
	Reason     string          `json:"reason"`
	CreatedAt  string          `json:"created_at"`
}

type RequeueRequest struct {
	NewInput *json.RawMessage `json:"new_input"`
}

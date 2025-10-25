package human_decision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rom8726/floxy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleHumanDecision_Success_Confirm(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	stepID := int64(456)
	user := "test-user"
	comment := "test comment"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionConfirmed, &comment).
		Return(nil)

	requestBody := DecisionRequest{Message: comment}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleHumanDecision_Success_Reject(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	stepID := int64(456)
	user := "test-user"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionRejected, (*string)(nil)).
		Return(nil)

	// Создание HTTP запроса
	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/reject", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	// Act
	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionRejected)
	handler(w, req)

	// Assert
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleHumanDecision_InvalidInstanceID(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	req := httptest.NewRequest("POST", "/api/instances/invalid/make-decision/confirm", nil)
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()

	// Создание функции извлечения пользователя
	extractUserFn := func(r *http.Request) (string, error) {
		return "test-user", nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleHumanDecision_ExtractUser_NotFound(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return "", floxy.ErrEntityNotFound
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleHumanDecision_ExtractUser_InternalError(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return "", errors.New("internal error")
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleHumanDecision_GetStep_NotFound(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	user := "test-user"

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(nil, floxy.ErrEntityNotFound)

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleHumanDecision_GetStep_InternalError(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	user := "test-user"

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(nil, errors.New("database error"))

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleHumanDecision_StepNotWaitingForDecision(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	stepID := int64(456)
	user := "test-user"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusCompleted,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleHumanDecision_MakeHumanDecision_NotFound(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	stepID := int64(456)
	user := "test-user"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionConfirmed, (*string)(nil)).
		Return(floxy.ErrEntityNotFound)

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleHumanDecision_MakeHumanDecision_InternalError(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	stepID := int64(456)
	user := "test-user"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionConfirmed, (*string)(nil)).
		Return(errors.New("engine error"))

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleHumanDecision_WithComment(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	stepID := int64(456)
	user := "test-user"
	comment := "This is a test comment"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionConfirmed, &comment).
		Return(nil)

	requestBody := DecisionRequest{Message: comment}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleHumanDecision_EmptyComment(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(123)
	stepID := int64(456)
	user := "test-user"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionConfirmed, (*string)(nil)).
		Return(nil)

	req := httptest.NewRequest("POST", "/api/instances/123/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleHumanDecision_EdgeCase_MaxInt64(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(9223372036854775807) // max int64
	stepID := int64(456)
	user := "test-user"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionConfirmed, (*string)(nil)).
		Return(nil)

	req := httptest.NewRequest("POST", "/api/instances/9223372036854775807/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "9223372036854775807")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleHumanDecision_EdgeCase_ZeroInstanceID(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)
	mockStore := floxy.NewMockStore(t)

	instanceID := int64(0)
	stepID := int64(456)
	user := "test-user"

	step := &floxy.WorkflowStep{
		ID:     stepID,
		Status: floxy.StepStatusWaitingDecision,
	}

	mockStore.On("GetHumanDecisionStepByInstanceID", mock.Anything, instanceID).
		Return(step, nil)

	mockEngine.On("MakeHumanDecision", mock.Anything, stepID, user, floxy.HumanDecisionConfirmed, (*string)(nil)).
		Return(nil)

	req := httptest.NewRequest("POST", "/api/instances/0/make-decision/confirm", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "0")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleHumanDecision(mockEngine, mockStore, extractUserFn, floxy.HumanDecisionConfirmed)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

package abort

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

func TestHandleAbortWorkflow_Success(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	instanceID := int64(123)
	user := "test-user"
	reason := "Critical error detected"

	mockEngine.On("AbortWorkflow", mock.Anything, instanceID, user, reason).
		Return(nil)

	requestBody := AbortRequest{Reason: reason}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/123/abort", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleAbortWorkflow_InvalidInstanceID(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	req := httptest.NewRequest("POST", "/api/instances/invalid/abort", nil)
	req = req.WithContext(context.Background())

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return "test-user", nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAbortWorkflow_ExtractUser_NotFound(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	req := httptest.NewRequest("POST", "/api/instances/123/abort", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return "", floxy.ErrEntityNotFound
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAbortWorkflow_ExtractUser_InternalError(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	req := httptest.NewRequest("POST", "/api/instances/123/abort", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return "", errors.New("internal error")
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleAbortWorkflow_InvalidJSON(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	req := httptest.NewRequest("POST", "/api/instances/123/abort", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return "test-user", nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAbortWorkflow_MissingReason(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	requestBody := AbortRequest{Reason: ""}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/123/abort", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return "test-user", nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAbortWorkflow_WorkflowNotFound(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	instanceID := int64(123)
	user := "test-user"
	reason := "Test abort"

	mockEngine.On("AbortWorkflow", mock.Anything, instanceID, user, reason).
		Return(floxy.ErrEntityNotFound)

	requestBody := AbortRequest{Reason: reason}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/123/abort", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAbortWorkflow_WorkflowInTerminalState(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	instanceID := int64(123)
	user := "test-user"
	reason := "Test abort"

	mockEngine.On("AbortWorkflow", mock.Anything, instanceID, user, reason).
		Return(errors.New("workflow is already in terminal state"))

	requestBody := AbortRequest{Reason: reason}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/123/abort", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleAbortWorkflow_EngineError(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	instanceID := int64(123)
	user := "test-user"
	reason := "Test abort"

	mockEngine.On("AbortWorkflow", mock.Anything, instanceID, user, reason).
		Return(errors.New("engine error"))

	requestBody := AbortRequest{Reason: reason}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/123/abort", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "123")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleAbortWorkflow_EdgeCase_MaxInt64(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	instanceID := int64(9223372036854775807) // max int64
	user := "test-user"
	reason := "Test abort"

	mockEngine.On("AbortWorkflow", mock.Anything, instanceID, user, reason).
		Return(nil)

	requestBody := AbortRequest{Reason: reason}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/9223372036854775807/abort", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "9223372036854775807")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleAbortWorkflow_EdgeCase_ZeroInstanceID(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	instanceID := int64(0)
	user := "test-user"
	reason := "Test abort"

	mockEngine.On("AbortWorkflow", mock.Anything, instanceID, user, reason).
		Return(nil)

	requestBody := AbortRequest{Reason: reason}
	jsonBody, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/api/instances/0/abort", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("instance_id", "0")

	w := httptest.NewRecorder()

	extractUserFn := func(r *http.Request) (string, error) {
		return user, nil
	}

	handler := HandleAbortWorkflow(mockEngine, extractUserFn)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

package dlq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rom8726/floxy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleList_Success_DefaultPagination(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	recTime := time.Now().Add(-time.Hour).UTC()
	records := []floxy.DeadLetterRecord{
		{
			ID:         1,
			InstanceID: 10,
			WorkflowID: "wf-1",
			StepID:     100,
			StepName:   "step_a",
			StepType:   "task",
			Input:      json.RawMessage(`{"a":1}`),
			Error:      ptr("boom"),
			Reason:     "test",
			CreatedAt:  recTime,
		},
	}

	mockStore.
		On("ListDeadLetters", mock.Anything, 0, 20).
		Return(records, int64(1), nil)

	req := httptest.NewRequest("GET", "/api/dlq", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()

	handler := HandleList(mockStore)
	handler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, int64(1), resp.Total)
	assert.Equal(t, 1, len(resp.Items))
	item := resp.Items[0]
	assert.Equal(t, records[0].ID, item.ID)
	assert.Equal(t, records[0].InstanceID, item.InstanceID)
	assert.Equal(t, records[0].WorkflowID, item.WorkflowID)
	assert.Equal(t, records[0].StepID, item.StepID)
	assert.Equal(t, records[0].StepName, item.StepName)
	assert.Equal(t, records[0].StepType, item.StepType)
	assert.Equal(t, records[0].Reason, item.Reason)
	assert.Equal(t, records[0].CreatedAt.UTC().Format(time.RFC3339Nano), item.CreatedAt)
}

func TestHandleList_CustomPagination(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	// page=2, page_size=10 -> offset 10, limit 10
	mockStore.
		On("ListDeadLetters", mock.Anything, 10, 10).
		Return([]floxy.DeadLetterRecord{}, int64(0), nil)

	req := httptest.NewRequest("GET", "/api/dlq?page=2&page_size=10", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()

	handler := HandleList(mockStore)
	handler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleList_Error(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	mockStore.
		On("ListDeadLetters", mock.Anything, 0, 20).
		Return(nil, int64(0), errors.New("db error"))

	req := httptest.NewRequest("GET", "/api/dlq", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()

	handler := HandleList(mockStore)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGet_Success(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	rec := &floxy.DeadLetterRecord{
		ID:         42,
		InstanceID: 333,
		WorkflowID: "wf-42",
		StepID:     777,
		StepName:   "step_x",
		StepType:   "task",
		Input:      json.RawMessage(`{"foo":"bar"}`),
		Error:      ptr("oops"),
		Reason:     "failed",
		CreatedAt:  time.Now().UTC(),
	}
	mockStore.
		On("GetDeadLetterByID", mock.Anything, int64(42)).
		Return(rec, nil)

	req := httptest.NewRequest("GET", "/api/dlq/42", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "42")
	w := httptest.NewRecorder()

	handler := HandleGet(mockStore)
	handler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeadLetterResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, rec.ID, resp.ID)
	assert.Equal(t, rec.InstanceID, resp.InstanceID)
	assert.Equal(t, rec.WorkflowID, resp.WorkflowID)
	assert.Equal(t, rec.StepID, resp.StepID)
	assert.Equal(t, rec.StepName, resp.StepName)
	assert.Equal(t, rec.StepType, resp.StepType)
	assert.Equal(t, rec.CreatedAt.UTC().Format(time.RFC3339Nano), resp.CreatedAt)
}

func TestHandleGet_InvalidID(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	req := httptest.NewRequest("GET", "/api/dlq/abc", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()

	handler := HandleGet(mockStore)
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGet_NotFound(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	mockStore.
		On("GetDeadLetterByID", mock.Anything, int64(404)).
		Return((*floxy.DeadLetterRecord)(nil), floxy.ErrEntityNotFound)

	req := httptest.NewRequest("GET", "/api/dlq/404", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "404")
	w := httptest.NewRecorder()

	handler := HandleGet(mockStore)
	handler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleGet_InternalError(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	mockStore.
		On("GetDeadLetterByID", mock.Anything, int64(500)).
		Return((*floxy.DeadLetterRecord)(nil), errors.New("boom"))

	req := httptest.NewRequest("GET", "/api/dlq/500", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "500")
	w := httptest.NewRecorder()

	handler := HandleGet(mockStore)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleRequeue_Success_NoBody(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	mockEngine.
		On("RequeueFromDLQ", mock.Anything, int64(77), (*json.RawMessage)(nil)).
		Return(nil)

	req := httptest.NewRequest("POST", "/api/dlq/77/requeue", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "77")
	w := httptest.NewRecorder()

	handler := HandleRequeue(mockEngine)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleRequeue_Success_WithNewInput(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	newInput := json.RawMessage(`{"restart":true}`)
	mockEngine.
		On("RequeueFromDLQ", mock.Anything, int64(88), mock.MatchedBy(func(in *json.RawMessage) bool {
			return in != nil && bytes.Equal([]byte(*in), []byte(newInput))
		})).
		Return(nil)

	body := RequeueRequest{NewInput: &newInput}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/dlq/88/requeue", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "88")
	w := httptest.NewRecorder()

	handler := HandleRequeue(mockEngine)
	handler(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleRequeue_InvalidID(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	req := httptest.NewRequest("POST", "/api/dlq/NaN/requeue", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()

	handler := HandleRequeue(mockEngine)
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRequeue_InvalidJSON(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	req := httptest.NewRequest("POST", "/api/dlq/11/requeue", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "11")
	w := httptest.NewRecorder()

	handler := HandleRequeue(mockEngine)
	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRequeue_NotFound(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	mockEngine.
		On("RequeueFromDLQ", mock.Anything, int64(404), (*json.RawMessage)(nil)).
		Return(floxy.ErrEntityNotFound)

	req := httptest.NewRequest("POST", "/api/dlq/404/requeue", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "404")
	w := httptest.NewRecorder()

	handler := HandleRequeue(mockEngine)
	handler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleRequeue_InternalError(t *testing.T) {
	mockEngine := floxy.NewMockIEngine(t)

	mockEngine.
		On("RequeueFromDLQ", mock.Anything, int64(500), (*json.RawMessage)(nil)).
		Return(errors.New("boom"))

	req := httptest.NewRequest("POST", "/api/dlq/500/requeue", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("id", "500")
	w := httptest.NewRecorder()

	handler := HandleRequeue(mockEngine)
	handler(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func ptr[T any](v T) *T { return &v }

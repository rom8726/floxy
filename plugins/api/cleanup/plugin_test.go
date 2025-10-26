package cleanup

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/rom8726/floxy"
)

func TestPlugin_Name(t *testing.T) {
	plugin := New(nil)
	if plugin.Name() != "cleanup" {
		t.Errorf("Expected name 'cleanup', got '%s'", plugin.Name())
	}
}

func TestPlugin_Description(t *testing.T) {
	plugin := New(nil)
	expected := "Clean up old completed workflow instances"
	if plugin.Description() != expected {
		t.Errorf("Expected description '%s', got '%s'", expected, plugin.Description())
	}
}

func TestHandleCleanupWorkflows_Success(t *testing.T) {
	mockStore := floxy.NewMockStore(t)
	mockStore.EXPECT().CleanupOldWorkflows(mock.Anything, mock.Anything).Return(int64(42), nil)

	// Create request
	reqBody := CleanupRequest{DaysToKeep: 30}
	jsonBody, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/cleanup", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler
	handler := HandleCleanupWorkflows(mockStore)
	handler(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check response body
	var response CleanupResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.DeletedCount != 42 {
		t.Errorf("Expected deleted count 42, got %d", response.DeletedCount)
	}

	if response.DaysToKeep != 30 {
		t.Errorf("Expected days to keep 30, got %d", response.DaysToKeep)
	}
}

func TestHandleCleanupWorkflows_InvalidDaysToKeep(t *testing.T) {
	mockStore := floxy.NewMockStore(t)

	// Create request with invalid days_to_keep
	reqBody := CleanupRequest{DaysToKeep: 0}
	jsonBody, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/cleanup", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler
	handler := HandleCleanupWorkflows(mockStore)
	handler(w, req)

	// Check response
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleCleanupWorkflows_StoreError(t *testing.T) {
	// Mock store with error
	mockStore := floxy.NewMockStore(t)
	mockStore.EXPECT().CleanupOldWorkflows(mock.Anything, mock.Anything).Return(int64(0), errors.New("Store error"))

	// Create request
	reqBody := CleanupRequest{DaysToKeep: 30}
	jsonBody, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/cleanup", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	w := httptest.NewRecorder()

	// Call handler
	handler := HandleCleanupWorkflows(mockStore)
	handler(w, req)

	// Check response
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

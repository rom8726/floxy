package floxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	api     *API
	monitor *Monitor
	cleanup *CleanupService
}

func NewServer(pool *pgxpool.Pool) *Server {
	return &Server{
		api:     NewAPI(pool),
		monitor: NewMonitor(pool),
		cleanup: NewCleanupService(pool),
	}
}

func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()

	// Workflow definitions
	mux.HandleFunc("GET /api/workflows", s.handleGetWorkflowDefinitions)
	mux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflowDefinition)
	mux.HandleFunc("GET /api/workflows/{id}/instances", s.handleGetWorkflowInstances)

	// Workflow instances
	mux.HandleFunc("GET /api/instances", s.handleGetAllInstances)
	mux.HandleFunc("GET /api/instances/{id}", s.handleGetWorkflowInstance)
	mux.HandleFunc("GET /api/instances/{id}/steps", s.handleGetWorkflowSteps)
	mux.HandleFunc("GET /api/instances/{id}/events", s.handleGetWorkflowEvents)

	// Statistics
	mux.HandleFunc("GET /api/stats", s.handleGetStats)

	return mux
}

func (s *Server) handleGetWorkflowDefinitions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	definitions, err := s.api.GetWorkflowDefinitions(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow definitions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(definitions)
}

func (s *Server) handleGetWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	definition, err := s.api.GetWorkflowDefinition(ctx, id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow definition: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(definition)
}

func (s *Server) handleGetWorkflowInstances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflowID := r.PathValue("id")

	instances, err := s.api.GetWorkflowInstances(ctx, workflowID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow instances: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instances)
}

func (s *Server) handleGetAllInstances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	instances, err := s.api.GetAllWorkflowInstances(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch instances: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instances)
}

func (s *Server) handleGetWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	instanceID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	instance, err := s.api.GetWorkflowInstance(ctx, instanceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow instance: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instance)
}

func (s *Server) handleGetWorkflowSteps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	instanceID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	steps, err := s.api.GetWorkflowSteps(ctx, instanceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow steps: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(steps)
}

func (s *Server) handleGetWorkflowEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	instanceID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	events, err := s.api.GetWorkflowEvents(ctx, instanceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow events: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := s.monitor.GetWorkflowStats(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow stats: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

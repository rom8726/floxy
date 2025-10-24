package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/rom8726/floxy"
)

type Server struct {
	api     *APIService
	monitor floxy.Monitor
}

func NewServer(store floxy.Store, monitor floxy.Monitor) *Server {
	return &Server{
		api:     NewAPIService(store),
		monitor: monitor,
	}
}

func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()

	// Workflow definitions
	mux.HandleFunc("GET /api/workflows", s.HandleGetWorkflowDefinitions)
	mux.HandleFunc("GET /api/workflows/{id}", s.HandleGetWorkflowDefinition)
	mux.HandleFunc("GET /api/workflows/{id}/instances", s.HandleGetWorkflowInstances)

	// Workflow instances
	mux.HandleFunc("GET /api/instances", s.HandleGetAllInstances)
	mux.HandleFunc("GET /api/instances/{id}", s.HandleGetWorkflowInstance)
	mux.HandleFunc("GET /api/instances/{id}/steps", s.HandleGetWorkflowSteps)
	mux.HandleFunc("GET /api/instances/{id}/events", s.HandleGetWorkflowEvents)

	// Statistics
	mux.HandleFunc("GET /api/stats", s.HandleGetStats)
	mux.HandleFunc("GET /api/stats/summary", s.HandleGetSummaryStats)

	// Active instances
	mux.HandleFunc("GET /api/instances/active", s.HandleGetActiveInstances)

	return mux
}

func (s *Server) HandleGetWorkflowDefinitions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	definitions, err := s.api.GetWorkflowDefinitions(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow definitions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(definitions)
}

func (s *Server) HandleGetWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	definition, err := s.api.GetWorkflowDefinition(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Workflow definition not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to fetch workflow definition: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(definition)
}

func (s *Server) HandleGetWorkflowInstances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflowID := r.PathValue("id")

	// First check if workflow exists
	_, err := s.api.GetWorkflowDefinition(ctx, workflowID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Workflow not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to fetch workflow: %v", err), http.StatusInternalServerError)
		return
	}

	instances, err := s.api.GetWorkflowInstances(ctx, workflowID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow instances: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instances)
}

func (s *Server) HandleGetAllInstances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	instances, err := s.api.GetAllWorkflowInstances(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch instances: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instances)
}

func (s *Server) HandleGetWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	instanceID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	instance, err := s.api.GetWorkflowInstance(ctx, instanceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Workflow instance not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to fetch workflow instance: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instance)
}

func (s *Server) HandleGetWorkflowSteps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	instanceID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	// First check if instance exists
	_, err = s.api.GetWorkflowInstance(ctx, instanceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Workflow instance not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to fetch workflow instance: %v", err), http.StatusInternalServerError)
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

func (s *Server) HandleGetWorkflowEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	instanceID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	// First check if instance exists
	_, err = s.api.GetWorkflowInstance(ctx, instanceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Workflow instance not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to fetch workflow instance: %v", err), http.StatusInternalServerError)
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

func (s *Server) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := s.monitor.GetWorkflowStats(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch workflow stats: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *Server) HandleGetSummaryStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := s.api.GetSummaryStats(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch summary stats: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *Server) HandleGetActiveInstances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	instances, err := s.api.GetActiveInstances(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch active instances: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(instances)
}

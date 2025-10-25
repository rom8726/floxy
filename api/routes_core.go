package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/rom8726/floxy"
)

func RegisterCoreRoutes(mux *http.ServeMux, store floxy.Store) {
	// Workflow definitions
	mux.HandleFunc("GET /api/workflows", func(w http.ResponseWriter, req *http.Request) {
		HandleGetWorkflowDefinitions(store)(w, req)
	})

	mux.HandleFunc("GET /api/workflows/{id}", func(w http.ResponseWriter, req *http.Request) {
		HandleGetWorkflowDefinition(store)(w, req)
	})

	mux.HandleFunc("GET /api/workflows/{id}/instances", func(w http.ResponseWriter, req *http.Request) {
		HandleGetWorkflowInstances(store)(w, req)
	})

	// Workflow instances
	mux.HandleFunc("GET /api/instances", func(w http.ResponseWriter, req *http.Request) {
		HandleGetAllInstances(store)(w, req)
	})

	mux.HandleFunc("GET /api/instances/{id}", func(w http.ResponseWriter, req *http.Request) {
		HandleGetWorkflowInstance(store)(w, req)
	})

	mux.HandleFunc("GET /api/instances/{id}/steps", func(w http.ResponseWriter, req *http.Request) {
		HandleGetWorkflowSteps(store)(w, req)
	})

	mux.HandleFunc("GET /api/instances/{id}/events", func(w http.ResponseWriter, req *http.Request) {
		HandleGetWorkflowEvents(store)(w, req)
	})

	// Statistics
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, req *http.Request) {
		HandleGetStats(store)(w, req)
	})

	mux.HandleFunc("GET /api/stats/summary", func(w http.ResponseWriter, req *http.Request) {
		HandleGetSummaryStats(store)(w, req)
	})

	// Active instances
	mux.HandleFunc("GET /api/instances/active", func(w http.ResponseWriter, req *http.Request) {
		HandleGetActiveInstances(store)(w, req)
	})
}

func HandleGetWorkflowDefinitions(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		definitions, err := store.GetWorkflowDefinitions(ctx)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow definitions: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(definitions)
	}
}

func HandleGetWorkflowDefinition(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.PathValue("id")

		definition, err := store.GetWorkflowDefinition(ctx, id)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				WriteErrorResponse(w, errors.New("workflow definition not found"), http.StatusNotFound)

				return
			}

			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow definition: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(definition)
	}
}

func HandleGetWorkflowInstances(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		workflowID := r.PathValue("id")

		// First check if workflow exists
		_, err := store.GetWorkflowDefinition(ctx, workflowID)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				WriteErrorResponse(w, errors.New("workflow not found"), http.StatusNotFound)

				return
			}

			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow: %w", err), http.StatusInternalServerError)

			return
		}

		instances, err := store.GetWorkflowInstances(ctx, workflowID)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow instances: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(instances)
	}
}

func HandleGetAllInstances(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		instances, err := store.GetAllWorkflowInstances(ctx)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch instances: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(instances)
	}
}

func HandleGetWorkflowInstance(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		idStr := r.PathValue("id")

		instanceID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			WriteErrorResponse(w, errors.New("invalid instance ID"), http.StatusBadRequest)

			return
		}

		instance, err := store.GetInstance(ctx, instanceID)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				WriteErrorResponse(w, errors.New("workflow instance not found"), http.StatusNotFound)

				return
			}

			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow instance: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(instance)
	}
}

func HandleGetWorkflowSteps(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		idStr := r.PathValue("id")

		instanceID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			WriteErrorResponse(w, errors.New("invalid instance ID"), http.StatusBadRequest)

			return
		}

		// First check if instance exists
		_, err = store.GetInstance(ctx, instanceID)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				WriteErrorResponse(w, errors.New("workflow instance not found"), http.StatusNotFound)

				return
			}

			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow instance: %w", err), http.StatusInternalServerError)

			return
		}

		steps, err := store.GetWorkflowSteps(ctx, instanceID)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow steps: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(steps)
	}
}

func HandleGetWorkflowEvents(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		idStr := r.PathValue("id")

		instanceID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			WriteErrorResponse(w, errors.New("invalid instance ID"), http.StatusBadRequest)

			return
		}

		// First check if instance exists
		_, err = store.GetInstance(ctx, instanceID)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				WriteErrorResponse(w, errors.New("workflow instance not found"), http.StatusNotFound)

				return
			}

			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow instance: %w", err), http.StatusInternalServerError)

			return
		}

		events, err := store.GetWorkflowEvents(ctx, instanceID)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow events: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(events)
	}
}

func HandleGetStats(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		stats, err := store.GetWorkflowStats(ctx)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch workflow stats: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

func HandleGetSummaryStats(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		stats, err := store.GetSummaryStats(ctx)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch summary stats: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

func HandleGetActiveInstances(store floxy.Store) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		instances, err := store.GetActiveInstances(ctx)
		if err != nil {
			WriteErrorResponse(w, fmt.Errorf("failed to fetch active instances: %w", err), http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(instances)
	}
}

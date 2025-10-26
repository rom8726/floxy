package cleanup

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rom8726/floxy"
	"github.com/rom8726/floxy/api"
)

var _ api.Plugin = (*Plugin)(nil)

type Plugin struct {
	store floxy.Store
}

func New(store floxy.Store) *Plugin {
	return &Plugin{
		store: store,
	}
}

func (p *Plugin) Name() string { return "cleanup" }

func (p *Plugin) Description() string { return "Clean up old completed workflow instances" }

func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/cleanup", HandleCleanupWorkflows(p.store))
}

func HandleCleanupWorkflows(
	store floxy.Store,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var cleanupReq CleanupRequest
		if err := json.NewDecoder(r.Body).Decode(&cleanupReq); err != nil {
			api.WriteErrorResponse(w, err, http.StatusBadRequest)

			return
		}

		// Validate days_to_keep parameter
		if cleanupReq.DaysToKeep <= 0 {
			err := errors.New("days_to_keep must be greater than 0")
			api.WriteErrorResponse(w, err, http.StatusBadRequest)

			return
		}

		// Call the cleanup function
		deletedCount, err := store.CleanupOldWorkflows(ctx, cleanupReq.DaysToKeep)
		if err != nil {
			api.WriteErrorResponse(w, err, http.StatusInternalServerError)

			return
		}

		// Return the result
		response := CleanupResponse{
			DeletedCount: deletedCount,
			DaysToKeep:   cleanupReq.DaysToKeep,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	}
}

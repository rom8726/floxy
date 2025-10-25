package abort

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rom8726/floxy"
	"github.com/rom8726/floxy/api"
)

var _ api.Plugin = (*Plugin)(nil)

type Plugin struct {
	engine        floxy.IEngine
	extractUserFn ExtractUserFn
}

func New(engine floxy.IEngine, extractUserFn ExtractUserFn) *Plugin {
	return &Plugin{
		engine:        engine,
		extractUserFn: extractUserFn,
	}
}

func (p *Plugin) Name() string { return "abort" }

func (p *Plugin) Description() string { return "Abort a workflow instance" }

func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(
		"POST /api/instances/{instance_id}/abort",
		HandleAbortWorkflow(p.engine, p.extractUserFn),
	)
}

func HandleAbortWorkflow(
	engine floxy.IEngine,
	extractUserFn ExtractUserFn,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		instanceIDStr := r.PathValue("instance_id")
		instanceID, err := strconv.ParseInt(instanceIDStr, 10, 64)
		if err != nil {
			api.WriteErrorResponse(w, err, http.StatusBadRequest)
			return
		}

		user, err := extractUserFn(r)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				api.WriteErrorResponse(w, err, http.StatusNotFound)

				return
			}

			api.WriteErrorResponse(w, err, http.StatusInternalServerError)

			return
		}

		var abortReq AbortRequest
		if err := json.NewDecoder(r.Body).Decode(&abortReq); err != nil {
			api.WriteErrorResponse(w, err, http.StatusBadRequest)

			return
		}

		// Validate required fields
		if abortReq.Reason == "" {
			err = errors.New("reason is required")
			api.WriteErrorResponse(w, err, http.StatusBadRequest)

			return
		}

		err = engine.AbortWorkflow(ctx, instanceID, user, abortReq.Reason)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				api.WriteErrorResponse(w, err, http.StatusNotFound)

				return
			}

			// Check if workflow is in terminal state
			if strings.Contains(err.Error(), "already in terminal state") {
				api.WriteErrorResponse(w, err, http.StatusConflict)

				return
			}

			api.WriteErrorResponse(w, err, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

package human_decision

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/rom8726/floxy"
	"github.com/rom8726/floxy/api"
)

var _ api.Plugin = (*Plugin)(nil)

type Plugin struct {
	engine        floxy.IEngine
	store         floxy.Store
	extractUserFn ExtractUserFn
}

func New(engine floxy.IEngine, store floxy.Store, extractUserFn ExtractUserFn) *Plugin {
	return &Plugin{
		engine:        engine,
		store:         store,
		extractUserFn: extractUserFn,
	}
}

func (p *Plugin) Name() string { return "human-decision" }

func (p *Plugin) Description() string { return "Make a human decision" }

func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(
		"POST /api/instances/{instance_id}/make-decision/confirm",
		HandleHumanDecision(p.engine, p.store, p.extractUserFn, floxy.HumanDecisionConfirmed),
	)

	mux.HandleFunc(
		"POST /api/instances/{instance_id}/make-decision/reject",
		HandleHumanDecision(p.engine, p.store, p.extractUserFn, floxy.HumanDecisionRejected),
	)
}

func HandleHumanDecision(
	engine floxy.IEngine,
	store floxy.Store,
	extractUserFn ExtractUserFn,
	decision floxy.HumanDecision,
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

		step, err := store.GetHumanDecisionStepByInstanceID(ctx, instanceID)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				api.WriteErrorResponse(w, err, http.StatusNotFound)

				return
			}

			api.WriteErrorResponse(w, err, http.StatusInternalServerError)

			return
		}

		if step.Status != floxy.StepStatusWaitingDecision {
			err = errors.New("step is not waiting for human decision")
			api.WriteErrorResponse(w, err, http.StatusConflict)

			return
		}

		var commentRef *string
		var decisionReq DecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&decisionReq); err == nil && decisionReq.Message != "" {
			commentRef = &decisionReq.Message
		}

		err = engine.MakeHumanDecision(ctx, step.ID, user, decision, commentRef)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				api.WriteErrorResponse(w, err, http.StatusNotFound)

				return
			}

			api.WriteErrorResponse(w, err, http.StatusInternalServerError)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

package dlq

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/rom8726/floxy"
	"github.com/rom8726/floxy/api"
)

var _ api.Plugin = (*Plugin)(nil)

type Plugin struct {
	engine floxy.IEngine
	store  floxy.Store
}

func New(engine floxy.IEngine, store floxy.Store) *Plugin {
	return &Plugin{engine: engine, store: store}
}

func (p *Plugin) Name() string        { return "dlq" }
func (p *Plugin) Description() string { return "Dead Letter Queue operations" }

func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dlq", HandleList(p.store))
	mux.HandleFunc("GET /api/dlq/{id}", HandleGet(p.store))
	mux.HandleFunc("POST /api/dlq/{id}/requeue", HandleRequeue(p.engine))
}

func HandleList(store floxy.Store) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// pagination: default page=1, page_size=20
		page := 1
		pageSize := 20
		if v := r.URL.Query().Get("page"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				page = n
			}
		}
		if v := r.URL.Query().Get("page_size"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				pageSize = n
			}
		}

		offset := (page - 1) * pageSize
		items, total, err := store.ListDeadLetters(ctx, offset, pageSize)
		if err != nil {
			api.WriteErrorResponse(w, err, http.StatusInternalServerError)
			return
		}

		resp := ListResponse{
			Items:    make([]DeadLetterResponse, 0, len(items)),
			Page:     page,
			PageSize: pageSize,
			Total:    total,
		}
		for _, it := range items {
			resp.Items = append(resp.Items, DeadLetterResponse{
				ID:         it.ID,
				InstanceID: it.InstanceID,
				WorkflowID: it.WorkflowID,
				StepID:     it.StepID,
				StepName:   it.StepName,
				StepType:   it.StepType,
				Input:      it.Input,
				Error:      it.Error,
				Reason:     it.Reason,
				CreatedAt:  it.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func HandleGet(store floxy.Store) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			api.WriteErrorResponse(w, err, http.StatusBadRequest)
			return
		}

		rec, err := store.GetDeadLetterByID(ctx, id)
		if err != nil {
			if errors.Is(err, floxy.ErrEntityNotFound) {
				api.WriteErrorResponse(w, err, http.StatusNotFound)
				return
			}
			api.WriteErrorResponse(w, err, http.StatusInternalServerError)
			return
		}

		resp := DeadLetterResponse{
			ID:         rec.ID,
			InstanceID: rec.InstanceID,
			WorkflowID: rec.WorkflowID,
			StepID:     rec.StepID,
			StepName:   rec.StepName,
			StepType:   rec.StepType,
			Input:      rec.Input,
			Error:      rec.Error,
			Reason:     rec.Reason,
			CreatedAt:  rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func HandleRequeue(engine floxy.IEngine) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			api.WriteErrorResponse(w, err, http.StatusBadRequest)
			return
		}

		var req RequeueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			api.WriteErrorResponse(w, err, http.StatusBadRequest)
			return
		}

		if err := engine.RequeueFromDLQ(ctx, id, req.NewInput); err != nil {
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

package human_decision

import (
	"net/http"
)

type ExtractUserFn func(req *http.Request) (string, error)

type DecisionRequest struct {
	Message string `json:"message"`
}

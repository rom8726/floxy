package abort

import (
	"net/http"
)

type ExtractUserFn func(req *http.Request) (string, error)

type AbortRequest struct {
	Reason string `json:"reason"`
}

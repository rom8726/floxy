package cancel

import (
	"net/http"
)

type ExtractUserFn func(req *http.Request) (string, error)

type CancelRequest struct {
	Reason string `json:"reason"`
}

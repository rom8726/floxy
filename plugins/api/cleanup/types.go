package cleanup

import (
	"net/http"
)

type ExtractUserFn func(req *http.Request) (string, error)

type CleanupRequest struct {
	DaysToKeep int `json:"days_to_keep"`
}

type CleanupResponse struct {
	DeletedCount int64 `json:"deleted_count"`
	DaysToKeep   int   `json:"days_to_keep"`
}

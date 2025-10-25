package api

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	Message string `json:"message"`
}

func WriteErrorResponse(writer http.ResponseWriter, err error, statusCode int) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)

	resp := ErrorResponse{Message: err.Error()}
	_ = json.NewEncoder(writer).Encode(resp)
}

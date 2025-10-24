package api

import (
	"net/http"
)

type Plugin interface {
	Name() string
	Description() string
	RegisterRoutes(mux *http.ServeMux)
}

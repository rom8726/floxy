package api

import (
	"fmt"
	"net/http"

	"github.com/rom8726/floxy"
)

type Server struct {
	engine  *floxy.Engine
	store   floxy.Store
	mux     *http.ServeMux
	plugins []Plugin
}

type Option func(*Server)

func WithPlugins(plugins ...Plugin) Option {
	return func(s *Server) {
		for _, p := range plugins {
			s.RegisterPlugin(p)
		}
	}
}

func New(engine *floxy.Engine, store floxy.Store, opts ...Option) *Server {
	srv := &Server{
		engine:  engine,
		store:   store,
		mux:     http.NewServeMux(),
		plugins: make([]Plugin, 0),
	}

	RegisterCoreRoutes(srv.mux, store)

	for _, opt := range opts {
		opt(srv)
	}

	return srv
}

func (s *Server) RegisterPlugin(plugin Plugin) {
	s.plugins = append(s.plugins, plugin)
	plugin.RegisterRoutes(s.mux)
	fmt.Printf("[floxy] plugin registered: %s\n", plugin.Name())
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

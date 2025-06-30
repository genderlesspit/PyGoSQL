package server

import (
	"fmt"
	"gosql/config"
	"gosql/httpsetup"
	"net/http"
	"time"
)

type Server struct {
	config    config.Config
	endpoints []httpsetup.Endpoint
	mux       *http.ServeMux
	server    *http.Server
}

func NewServer(cfg config.Config, endpoints []httpsetup.Endpoint) *Server {
	s := &Server{
		config:    cfg,
		endpoints: endpoints,
		mux:       http.NewServeMux(),
	}

	s.setupRoutes()
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      s.mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) setupRoutes() {
	// API endpoints
	for _, ep := range s.endpoints {
		s.mux.HandleFunc(ep.Path, ep.Handler)
	}

	// System endpoints
	s.mux.HandleFunc("/health", s.healthHandler)
	s.mux.HandleFunc("/", s.rootHandler)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"healthy","endpoints":%d,"port":%d,"timestamp":"%s"}`,
		len(s.endpoints), s.config.Port, time.Now().Format(time.RFC3339))
}

func (s *Server) rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"message":"GoSQL HTTP API Server","endpoints":[`))

	for i, ep := range s.endpoints {
		if i > 0 {
			w.Write([]byte(","))
		}
		fmt.Fprintf(w, `{"method":"%s","path":"%s","universal":%t}`,
			ep.HTTPMethod, ep.Path, ep.IsUniversal)
	}

	w.Write([]byte(`]}`))
}

func (s *Server) Start() error {
	fmt.Printf("\nServer running on http://localhost:%d\n", s.config.Port)
	fmt.Printf("API base: http://localhost:%d%s\n", s.config.Port, s.config.BaseURL)
	fmt.Printf("Health: http://localhost:%d/health\n", s.config.Port)
	fmt.Printf("Endpoints: %d\n", len(s.endpoints))

	return s.server.ListenAndServe()
}

func (s *Server) Shutdown() error {
	fmt.Println("\n🛑 Shutting down server...")
	return s.server.Close()
}
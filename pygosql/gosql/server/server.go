// server.go
package server

import (
    "context"
    "encoding/json"
    "fmt"
    "gosql/setup"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"
)

// Server manages the HTTP server with configured endpoints and middleware
type Server struct {
    config    setup.Config   // Server configuration
    endpoints []Endpoint     // List of configured endpoints
    mux       *http.ServeMux // HTTP request multiplexer
    server    *http.Server   // Underlying HTTP server
}

// NewServer creates a new Server instance with the given configuration and endpoints
func NewServer(cfg setup.Config, endpoints []Endpoint) *Server {
    s := &Server{
        config:    cfg,
        endpoints: endpoints,
        mux:       http.NewServeMux(),
    }

    // Setup routes immediately
    s.SetupRoutes()

    // Create HTTP server with timeouts
    s.server = &http.Server{
        Addr:         fmt.Sprintf(":%d", cfg.Port),
        Handler:      s.mux,
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    return s
}

// SetupRoutes registers all endpoint handlers with the HTTP multiplexer
func (s *Server) SetupRoutes() {
    // Register system endpoints
    s.mux.HandleFunc("/health", s.HealthHandler)
    s.mux.HandleFunc("/", s.RootHandler)

    // Register API endpoints
    for _, endpoint := range s.endpoints {
        log.Printf("Registering endpoint: %s %s -> %s", endpoint.Method, endpoint.Path, endpoint.SQLPath)
        s.mux.HandleFunc(endpoint.Path, s.wrapHandler(endpoint))
    }

    log.Printf("Registered %d API endpoints", len(s.endpoints))
}

// wrapHandler wraps endpoint handlers with middleware (CORS, method validation, etc.)
func (s *Server) wrapHandler(endpoint Endpoint) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Enable CORS if configured
        if s.config.EnableCORS {
            s.EnableCORS(w, r)
        }

        // Handle preflight requests
        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }

        // Validate HTTP method
        if r.Method != endpoint.Method {
            s.WriteJSONResponse(w, http.StatusMethodNotAllowed, map[string]interface{}{
                "success": false,
                "error":   fmt.Sprintf("Method %s not allowed. Expected %s", r.Method, endpoint.Method),
            })
            return
        }

        // Add debug information if enabled
        if s.config.DebugMode {
            log.Printf("%s %s - Executing %s", r.Method, r.URL.Path, endpoint.SQLPath)
        }

        // Call the actual endpoint handler
        endpoint.Handler(w, r)
    }
}

// HealthHandler responds to health check requests with server status information
func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
    if s.config.EnableCORS {
        s.EnableCORS(w, r)
    }

    if r.Method == "OPTIONS" {
        w.WriteHeader(http.StatusOK)
        return
    }

    if r.Method != "GET" {
        s.WriteJSONResponse(w, http.StatusMethodNotAllowed, map[string]interface{}{
            "success": false,
            "error":   "Only GET method allowed for health check",
        })
        return
    }

    healthData := map[string]interface{}{
        "status":      "healthy",
        "timestamp":   time.Now().Format(time.RFC3339),
        "endpoints":   len(s.endpoints),
        "port":        s.config.Port,
        "base_url":    s.config.BaseURL,
        "cors_enabled": s.config.EnableCORS,
        "debug_mode":  s.config.DebugMode,
    }

    s.WriteJSONResponse(w, http.StatusOK, healthData)
}

// RootHandler serves the root endpoint with API documentation and available endpoints
func (s *Server) RootHandler(w http.ResponseWriter, r *http.Request) {
    if s.config.EnableCORS {
        s.EnableCORS(w, r)
    }

    if r.Method == "OPTIONS" {
        w.WriteHeader(http.StatusOK)
        return
    }

    // Only serve root documentation on exact root path
    if r.URL.Path != "/" {
        http.NotFound(w, r)
        return
    }

    if r.Method != "GET" {
        s.WriteJSONResponse(w, http.StatusMethodNotAllowed, map[string]interface{}{
            "success": false,
            "error":   "Only GET method allowed for root endpoint",
        })
        return
    }

    // Build endpoint documentation
    endpointDocs := make([]map[string]interface{}, 0, len(s.endpoints))
    for _, endpoint := range s.endpoints {
        endpointDocs = append(endpointDocs, map[string]interface{}{
            "path":         endpoint.Path,
            "method":       endpoint.Method,
            "table_name":   endpoint.TableName,
            "is_universal": endpoint.IsUniversal,
            "sql_path":     endpoint.SQLPath,
        })
    }

    rootData := map[string]interface{}{
        "message":     "GoSQL HTTP API Server",
        "version":     "1.0.0",
        "base_url":    s.config.BaseURL,
        "endpoints":   endpointDocs,
        "system_endpoints": []map[string]interface{}{
            {"path": "/", "method": "GET", "description": "API documentation"},
            {"path": "/health", "method": "GET", "description": "Health check"},
        },
        "total_endpoints": len(s.endpoints),
        "timestamp":       time.Now().Format(time.RFC3339),
    }

    s.WriteJSONResponse(w, http.StatusOK, rootData)
}

// Start begins listening for HTTP requests on the configured port
func (s *Server) Start() error {
    // Setup graceful shutdown
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

    // Start server in goroutine
    go func() {
        log.Printf("🚀 Server starting on port %d", s.config.Port)
        log.Printf("📡 API base URL: http://localhost:%d%s", s.config.Port, s.config.BaseURL)
        log.Printf("❤️  Health check: http://localhost:%d/health", s.config.Port)
        log.Printf("📚 Documentation: http://localhost:%d/", s.config.Port)
        log.Printf("🔧 Debug mode: %v", s.config.DebugMode)
        log.Printf("🌐 CORS enabled: %v", s.config.EnableCORS)

        if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("Server failed to start: %v", err)
        }
    }()

    // Wait for interrupt signal
    <-stop
    log.Println("🛑 Shutting down server...")

    return s.Shutdown()
}

// Shutdown gracefully stops the HTTP server
func (s *Server) Shutdown() error {
    // Create shutdown context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Attempt graceful shutdown
    if err := s.server.Shutdown(ctx); err != nil {
        log.Printf("❌ Server forced to shutdown: %v", err)
        return err
    }

    log.Println("✅ Server gracefully stopped")
    return nil
}

// EnableCORS adds CORS headers to HTTP responses if enabled in config
func (s *Server) EnableCORS(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
    w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
}

// WriteJSONResponse writes a JSON response with the given status code and data
func (s *Server) WriteJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)

    if err := json.NewEncoder(w).Encode(data); err != nil {
        log.Printf("Failed to encode JSON response: %v", err)
        // Try to write a basic error response
        w.WriteHeader(http.StatusInternalServerError)
        fmt.Fprintf(w, `{"success":false,"error":"JSON encoding failed"}`)
    }
}

// ExtractRequestParams extracts parameters from URL query string and request body
func (s *Server) ExtractRequestParams(r *http.Request) (map[string]interface{}, error) {
    params := make(map[string]interface{})

    // Extract from query parameters
    for key, values := range r.URL.Query() {
        if len(values) > 0 {
            params[key] = values[0] // Take first value if multiple
        }
    }

    // Extract from body for POST/PUT requests
    if r.Method == "POST" || r.Method == "PUT" {
        var bodyParams map[string]interface{}
        if err := json.NewDecoder(r.Body).Decode(&bodyParams); err != nil {
            // Don't return error for empty body, just skip
            if err.Error() != "EOF" {
                return params, fmt.Errorf("failed to decode JSON body: %w", err)
            }
        } else {
            // Merge body parameters
            for key, value := range bodyParams {
                params[key] = value
            }
        }
    }

    return params, nil
}
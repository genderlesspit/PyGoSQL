package httpsetup

import (
	"encoding/json"
	"fmt"
	"gosql/config"
	"gosql/database"
	"gosql/models"
	"net/http"
	"strings"
)

type Endpoint struct {
	Path        string
	HTTPMethod  string
	Handler     http.HandlerFunc
	Description string
	SQLPath     string
	IsUniversal bool
	TableName   string
}

type HTTPSetup struct {
	config    config.Config
	database  *database.Database
	endpoints []Endpoint
}

func NewHTTPSetup(cfg config.Config) (*HTTPSetup, error) {
	// Load schema if available
	schemaContent := ""
	if cfg.SchemaPath != "" {
		if content, err := models.LoadSQL(cfg.SchemaPath); err == nil {
			schemaContent = content.Content
		}
	}

	db, err := database.NewDatabase(database.Config{
		Path:              cfg.DatabasePath,
		CreateIfNotExists: true,
		Schema:            schemaContent,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return &HTTPSetup{
		config:   cfg,
		database: db,
	}, nil
}

func (h *HTTPSetup) Setup() ([]Endpoint, error) {
	methods, err := models.Setup(h.config.SQLRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to setup SQL methods: %w", err)
	}

	endpoints := make([]Endpoint, 0, len(methods))
	for _, method := range methods {
		endpoint := h.createEndpoint(method)
		endpoints = append(endpoints, endpoint)
	}

	h.endpoints = endpoints
	return endpoints, nil
}

func (h *HTTPSetup) createEndpoint(method models.Method) Endpoint {
	path := h.config.BaseURL + "/"
	tableName := ""
	isUniversal := method.IsUniversal()

	// Since we only load table methods now, this should always be table-scoped
	if tm, ok := method.(models.TableMethods); ok {
		path += tm.Table + "/" + tm.Name
		tableName = tm.Table
		isUniversal = false
	} else {
		// Fallback for any universal methods (shouldn't happen with new structure)
		path += method.GetName()
	}

	return Endpoint{
		Path:        path,
		HTTPMethod:  h.mapHTTPMethod(method.GetName()),
		Handler:     h.createHandler(method),
		Description: fmt.Sprintf("%s operation on %s: %s", method.GetMethod(), tableName, method.GetName()),
		SQLPath:     method.GetSQL().Path,
		IsUniversal: isUniversal,
		TableName:   tableName,
	}
}

// Enhanced HTTP method mapping with exact matches and fallbacks
func (h *HTTPSetup) mapHTTPMethod(name string) string {
	// Exact matches first
	switch name {
	case "select", "find", "get", "read", "list":
		return "GET"
	case "insert", "create", "add", "new":
		return "POST"
	case "update", "upsert", "modify", "edit", "put":
		return "PUT"
	case "delete", "remove", "drop", "destroy":
		return "DELETE"
	default:
		// Fallback to contains matching
		lowerName := strings.ToLower(name)
		switch {
		case strings.Contains(lowerName, "select") || strings.Contains(lowerName, "find") ||
			 strings.Contains(lowerName, "get") || strings.Contains(lowerName, "read") ||
			 strings.Contains(lowerName, "list"):
			return "GET"
		case strings.Contains(lowerName, "insert") || strings.Contains(lowerName, "create") ||
			 strings.Contains(lowerName, "add") || strings.Contains(lowerName, "new"):
			return "POST"
		case strings.Contains(lowerName, "update") || strings.Contains(lowerName, "upsert") ||
			 strings.Contains(lowerName, "modify") || strings.Contains(lowerName, "edit") ||
			 strings.Contains(lowerName, "put"):
			return "PUT"
		case strings.Contains(lowerName, "delete") || strings.Contains(lowerName, "remove") ||
			 strings.Contains(lowerName, "drop") || strings.Contains(lowerName, "destroy"):
			return "DELETE"
		default:
			return "GET" // Default fallback
		}
	}
}

func (h *HTTPSetup) createHandler(method models.Method) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.handleCORS(w, r)
		if r.Method == "OPTIONS" {
			return
		}

		// Validate HTTP method matches expected method
		expectedMethod := h.mapHTTPMethod(method.GetName())
		if r.Method != expectedMethod {
			h.writeResponse(w, http.StatusMethodNotAllowed, map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Method %s not allowed. Expected %s", r.Method, expectedMethod),
			})
			return
		}

		params := h.extractParams(r)
		sql := h.processSQL(method, params)

		result, err := h.database.ExecSQL(sql, params...)
		if err != nil {
			h.writeResponse(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		response := map[string]interface{}{
			"success": true,
			"data":    result,
		}

		if h.config.DebugMode {
			response["debug"] = map[string]interface{}{
				"method":        method.GetMethod(),
				"sql_path":      method.GetSQL().Path,
				"is_universal":  method.IsUniversal(),
				"table_name":    "",
				"processed_sql": sql,
			}

			// Add table name for table methods
			if tm, ok := method.(models.TableMethods); ok {
				response["debug"].(map[string]interface{})["table_name"] = tm.Table
			}
		}

		h.writeResponse(w, http.StatusOK, response)
	}
}

func (h *HTTPSetup) handleCORS(w http.ResponseWriter, r *http.Request) {
	if h.config.EnableCORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	}
}

func (h *HTTPSetup) extractParams(r *http.Request) []interface{} {
	var params []interface{}

	// Extract from query parameters
	for _, values := range r.URL.Query() {
		if len(values) > 0 {
			params = append(params, values[0])
		}
	}

	// Extract from body for POST/PUT
	if r.Method == "POST" || r.Method == "PUT" {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			for _, v := range body {
				params = append(params, v)
			}
		}
	}

	return params
}

func (h *HTTPSetup) processSQL(method models.Method, params []interface{}) string {
	sql := method.GetSQL().Content

	// Replace table placeholder for table methods
	if tm, ok := method.(models.TableMethods); ok {
		sql = strings.ReplaceAll(sql, "{{table}}", tm.Table)
	}

	// Enhanced template replacements
	replacements := map[string]string{
		"{{columns}}":      "*", // Could be enhanced to use actual column names
		"{{placeholders}}": "?", // Could be enhanced to generate multiple placeholders
		"{{updates}}":      "column = ?", // Could be enhanced to generate actual update clauses
	}

	for k, v := range replacements {
		sql = strings.ReplaceAll(sql, k, v)
	}

	return sql
}

func (h *HTTPSetup) writeResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// GetDatabaseHealth returns the health status of the database
func (h *HTTPSetup) GetDatabaseHealth() bool {
	if h.database == nil {
		return false
	}
	return h.database.IsHealthy()
}

// GetEndpoints returns all configured endpoints
func (h *HTTPSetup) GetEndpoints() []Endpoint {
	return h.endpoints
}

// GetEndpointsByTable returns endpoints for a specific table
func (h *HTTPSetup) GetEndpointsByTable(tableName string) []Endpoint {
	var tableEndpoints []Endpoint
	for _, ep := range h.endpoints {
		if ep.TableName == tableName {
			tableEndpoints = append(tableEndpoints, ep)
		}
	}
	return tableEndpoints
}

// GetTableNames returns all table names that have endpoints
func (h *HTTPSetup) GetTableNames() []string {
	tableSet := make(map[string]bool)
	for _, ep := range h.endpoints {
		if ep.TableName != "" {
			tableSet[ep.TableName] = true
		}
	}

	var tables []string
	for table := range tableSet {
		tables = append(tables, table)
	}
	return tables
}

func (h *HTTPSetup) Close() error {
	if h.database != nil {
		return h.database.Close()
	}
	return nil
}

func SetupHTTP(cfg config.Config) ([]Endpoint, error) {
	setup, err := NewHTTPSetup(cfg)
	if err != nil {
		return nil, err
	}

	endpoints, err := setup.Setup()
	if err != nil {
		setup.Close()
		return nil, err
	}

	return endpoints, nil
}
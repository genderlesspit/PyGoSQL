// sql_to_http.go
package server

import (
    "encoding/json"
    "fmt"
    "gosql/database"
    "net/http"
    "path/filepath"
    "regexp"
    "strings"
    "os"
    "log"
)

// Endpoint represents an HTTP endpoint with its routing and SQL execution details
type Endpoint struct {
    Path        string            // HTTP route path (e.g., "/api/v1/users/select")
    Method      string            // HTTP method (GET, POST, PUT, DELETE)
    Handler     http.HandlerFunc  // HTTP handler function
    SQLPath     string            // Path to the SQL file
    TableName   string            // Table name (empty for universal endpoints)
    IsUniversal bool              // Whether this is a universal endpoint
}

// GlobSQLFiles recursively finds all .sql files in the given root directory
func GlobSQLFiles(rootPath string) ([]string, error) {
    var sqlFiles []string

    err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }

        if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".sql") {
            sqlFiles = append(sqlFiles, path)
        }

        return nil
    })

    if err != nil {
        return nil, fmt.Errorf("failed to walk directory %s: %w", rootPath, err)
    }

    return sqlFiles, nil
}

// RouteFromPath converts a SQL file path to an HTTP route path
// Example: "db/Tables/users/GET/select.sql" -> "/api/v1/users/select"
func RouteFromPath(sqlPath string, baseURL string) string {
    // Normalize path separators
    normalizedPath := filepath.ToSlash(sqlPath)

    // Split path into components
    parts := strings.Split(normalizedPath, "/")

    // Find Tables directory index
    tablesIndex := -1
    for i, part := range parts {
        if part == "Tables" {
            tablesIndex = i
            break
        }
    }

    if tablesIndex == -1 || tablesIndex+3 >= len(parts) {
        // Not a table-specific path, treat as universal
        filename := filepath.Base(sqlPath)
        name := strings.TrimSuffix(filename, ".sql")
        return fmt.Sprintf("%s/%s", baseURL, name)
    }

    // Extract table name and endpoint name
    tableName := parts[tablesIndex+1]
    filename := parts[len(parts)-1]
    endpointName := strings.TrimSuffix(filename, ".sql")

    return fmt.Sprintf("%s/%s/%s", baseURL, tableName, endpointName)
}

// MethodFromPath extracts the HTTP method from a SQL file path
// Example: "db/Tables/users/GET/select.sql" -> "GET"
func MethodFromPath(sqlPath string) string {
    // Normalize path separators
    normalizedPath := filepath.ToSlash(sqlPath)
    parts := strings.Split(normalizedPath, "/")

    // Look for HTTP method directory names
    httpMethods := []string{"GET", "POST", "PUT", "DELETE"}

    for _, part := range parts {
        upperPart := strings.ToUpper(part)
        for _, method := range httpMethods {
            if upperPart == method {
                return method
            }
        }
    }

    // Fallback: infer from filename
    filename := strings.ToLower(filepath.Base(sqlPath))
    filename = strings.TrimSuffix(filename, ".sql")

    switch {
    case strings.Contains(filename, "select") || strings.Contains(filename, "get") || strings.Contains(filename, "find"):
        return "GET"
    case strings.Contains(filename, "insert") || strings.Contains(filename, "create") || strings.Contains(filename, "add"):
        return "POST"
    case strings.Contains(filename, "update") || strings.Contains(filename, "put") || strings.Contains(filename, "modify"):
        return "PUT"
    case strings.Contains(filename, "delete") || strings.Contains(filename, "remove"):
        return "DELETE"
    default:
        return "GET" // Default fallback
    }
}

// ExecuteSQLFromPath loads and executes a SQL file with the provided parameters
func ExecuteSQLFromPath(db *database.Database, sqlPath string, params map[string]interface{}) (interface{}, error) {
    // Load SQL file
    sqlFile, err := database.LoadSQL(sqlPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load SQL file %s: %w", sqlPath, err)
    }

    if sqlFile.IsEmpty() {
        return nil, fmt.Errorf("SQL file is empty: %s", sqlPath)
    }

    // Extract table name and process template
    tableName := ExtractTableName(sqlPath)
    processedSQL := ProcessSQLTemplate(sqlFile.Content, tableName, params)

    // Convert params map to slice for sql.DB
    var args []interface{}
    for _, value := range params {
        args = append(args, value)
    }

    // Execute SQL
    return db.ExecSQL(processedSQL, args...)
}

// DefaultRoutesPerTable generates standard CRUD endpoints for a given table
// Returns endpoints for basic operations like select, insert, update, delete
func DefaultRoutesPerTable(tableName string, db *database.Database) []Endpoint {
    baseURL := "/api/v1"

    endpoints := []Endpoint{
        {
            Path:        fmt.Sprintf("%s/%s/select", baseURL, tableName),
            Method:      "GET",
            Handler:     CreateHandler(db, fmt.Sprintf("Tables/%s/GET/select.sql", tableName)),
            SQLPath:     fmt.Sprintf("Tables/%s/GET/select.sql", tableName),
            TableName:   tableName,
            IsUniversal: false,
        },
        {
            Path:        fmt.Sprintf("%s/%s/insert", baseURL, tableName),
            Method:      "POST",
            Handler:     CreateHandler(db, fmt.Sprintf("Tables/%s/POST/insert.sql", tableName)),
            SQLPath:     fmt.Sprintf("Tables/%s/POST/insert.sql", tableName),
            TableName:   tableName,
            IsUniversal: false,
        },
        {
            Path:        fmt.Sprintf("%s/%s/update", baseURL, tableName),
            Method:      "PUT",
            Handler:     CreateHandler(db, fmt.Sprintf("Tables/%s/PUT/update.sql", tableName)),
            SQLPath:     fmt.Sprintf("Tables/%s/PUT/update.sql", tableName),
            TableName:   tableName,
            IsUniversal: false,
        },
        {
            Path:        fmt.Sprintf("%s/%s/delete", baseURL, tableName),
            Method:      "DELETE",
            Handler:     CreateHandler(db, fmt.Sprintf("Tables/%s/DELETE/delete.sql", tableName)),
            SQLPath:     fmt.Sprintf("Tables/%s/DELETE/delete.sql", tableName),
            TableName:   tableName,
            IsUniversal: false,
        },
    }

    return endpoints
}

// AssembleEndpoint creates a complete Endpoint from a SQL file path and database connection
func AssembleEndpoint(sqlPath string, db *database.Database, baseURL string) Endpoint {
    return Endpoint{
        Path:        RouteFromPath(sqlPath, baseURL),
        Method:      MethodFromPath(sqlPath),
        Handler:     CreateHandler(db, sqlPath),
        SQLPath:     sqlPath,
        TableName:   ExtractTableName(sqlPath),
        IsUniversal: !strings.Contains(sqlPath, "Tables/"),
    }
}

// CreateHandler creates an HTTP handler function that executes the SQL file at the given path
func CreateHandler(db *database.Database, sqlPath string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Set CORS headers
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

        // Handle preflight requests
        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }

        // Extract parameters from request
        params, err := ExtractRequestParams(r)
        if err != nil {
            WriteErrorResponse(w, http.StatusBadRequest, fmt.Sprintf("Failed to extract parameters: %v", err))
            return
        }

        // Execute SQL
        result, err := ExecuteSQLFromPath(db, sqlPath, params)
        if err != nil {
            // Check if it's a constraint error (client error)
            if isConstraintError(err) {
                log.Printf("   - Constraint violation: %v", err)
                WriteErrorResponse(w, http.StatusBadRequest, fmt.Sprintf("Data validation failed: %v", err))
                return
            }

            // Otherwise it's a server error
            log.Printf("   - Server error: %v", err)
            WriteErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("SQL execution failed: %v", err))
            return
        }

        // Write success response
        WriteJSONResponse(w, http.StatusOK, map[string]interface{}{
            "success": true,
            "data":    result,
        })
    }
}

// Helper to identify constraint errors
func isConstraintError(err error) bool {
    errStr := strings.ToLower(err.Error())
    return strings.Contains(errStr, "constraint") ||
           strings.Contains(errStr, "unique") ||
           strings.Contains(errStr, "not null") ||
           strings.Contains(errStr, "foreign key") ||
           strings.Contains(errStr, "check constraint")
}

// ExtractTableName extracts the table name from a SQL file path
// Example: "db/Tables/users/GET/select.sql" -> "users"
func ExtractTableName(sqlPath string) string {
    // Normalize path separators
    normalizedPath := filepath.ToSlash(sqlPath)
    parts := strings.Split(normalizedPath, "/")

    // Find Tables directory index
    for i, part := range parts {
        if part == "Tables" && i+1 < len(parts) {
            return parts[i+1]
        }
    }

    return "" // Not a table-specific path
}

func ProcessSQLTemplate(sqlContent string, tableName string, params map[string]interface{}) string {
    log.Printf("   - ProcessSQLTemplate called:")
    log.Printf("   - Original SQL: %s", sqlContent)
    log.Printf("   - Table name: %s", tableName)
    log.Printf("   - Parameters: %+v", params)

    // Add table name to params for {{table}} replacement
    allParams := make(map[string]interface{})
    for k, v := range params {
        allParams[k] = v
    }
    if tableName != "" {
        allParams["table"] = tableName
        log.Printf("   - Added table to params: %s", tableName)
    }

    // SPECIAL HANDLING: Generate columns and values from actual JSON data
    if len(params) > 0 {
        var columns []string
        var values []string

        for key, value := range params {
            columns = append(columns, key)
            values = append(values, fmt.Sprintf("'%v'", value))
        }

        allParams["columns"] = strings.Join(columns, ", ")
        allParams["values"] = strings.Join(values, ", ")

        log.Printf("   - Generated columns: %s", allParams["columns"])
        log.Printf("   - Generated values: %s", allParams["values"])
    }

    log.Printf("   - All available params: %+v", allParams)

    // Regex to find all {{variable}} patterns
    re := regexp.MustCompile(`\{\{(\w+)\}\}`)

    // Find all matches first for logging
    matches := re.FindAllString(sqlContent, -1)
    log.Printf("   - Found template variables: %v", matches)

    result := re.ReplaceAllStringFunc(sqlContent, func(match string) string {
        // Extract variable name from {{variable}}
        varName := strings.Trim(match, "{}")

        // If variable exists in params, replace it
        if value, exists := allParams[varName]; exists {
            replacement := fmt.Sprintf("%v", value)
            log.Printf("   - Replacing %s with '%s'", match, replacement)
            return replacement
        }

        // Leave unmatched templates as-is
        log.Printf("   - No replacement found for %s - leaving as-is", match)
        return match
    })

    log.Printf("Final SQL: %s", result)
    log.Printf("ProcessSQLTemplate complete\n")

    return result
}

// Helper functions

// ExtractRequestParams extracts parameters from URL query string and request body
func ExtractRequestParams(r *http.Request) (map[string]interface{}, error) {
    params := make(map[string]interface{})

    // Extract from query parameters
    for key, values := range r.URL.Query() {
        if len(values) > 0 {
            params[key] = values[0]
        }
    }

    // Extract from body for POST/PUT requests
    if r.Method == "POST" || r.Method == "PUT" {
        var bodyParams map[string]interface{}
        if err := json.NewDecoder(r.Body).Decode(&bodyParams); err == nil {
            for key, value := range bodyParams {
                params[key] = value
            }
        }
    }

    return params, nil
}

// WriteJSONResponse writes a JSON response with the given status code and data
func WriteJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    json.NewEncoder(w).Encode(data)
}

// WriteErrorResponse writes a JSON error response
func WriteErrorResponse(w http.ResponseWriter, statusCode int, message string) {
    WriteJSONResponse(w, statusCode, map[string]interface{}{
        "success": false,
        "error":   message,
    })
}
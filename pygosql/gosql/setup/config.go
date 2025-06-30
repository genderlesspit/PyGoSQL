// config.go
package setup

const (
    DefaultDBPath     = "gosql_dir/app.db"
    DefaultSchemaPath = "gosql_dir/db/schema.sql"
    DefaultSQLRoot    = "gosql_dir/db"
    BaseURL           = "/api/v1"
    DefaultPort       = 2222
    ModuleName        = "gosql"
)

// Config holds all configuration settings for the GoSQL application
type Config struct {
    DatabasePath string // Path to the SQLite database file
    SQLRoot      string // Root directory containing SQL files
    SchemaPath   string // Path to schema.sql file
    BaseURL      string // Base URL prefix for API endpoints
    Port         int    // HTTP server port
    EnableCORS   bool   // Whether to enable CORS headers
    DebugMode    bool   // Whether to include debug information in responses
}

// DefaultConfig returns a Config struct with sensible default values
func DefaultConfig() Config {
    return Config{
        DatabasePath: DefaultDBPath,
        SQLRoot:      DefaultSQLRoot,
        SchemaPath:   DefaultSchemaPath,
        BaseURL:      BaseURL,
        Port:         DefaultPort,
        EnableCORS:   true,
        DebugMode:    true,
    }
}
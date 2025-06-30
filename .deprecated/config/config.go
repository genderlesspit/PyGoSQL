package config

const (
	DefaultDBPath     = "gosql_dir/app.db"
	DefaultSchemaPath = "gosql_dir/db/schema.sql"
	DefaultSQLRoot    = "gosql_dir/db"
	BaseURL           = "/api/v1"
	DefaultPort       = 8080
	ModuleName        = "gosql"
)

type Config struct {
	DatabasePath string
	SQLRoot      string
	SchemaPath   string
	BaseURL      string
	Port         int
	EnableCORS   bool
	DebugMode    bool
}

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
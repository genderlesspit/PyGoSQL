// main.go
package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "gosql/database"
    "gosql/server"
    "gosql/setup"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "time"
)

// main is the entry point that sets up configuration, discovers SQL files,
// creates endpoints, and starts the HTTP server
func main() {
    log.Printf("[MAIN] Starting PyGoSQL...")

    cfg := setup.DefaultConfig()
    log.Printf("[MAIN] Default configuration loaded:")
    log.Printf("[MAIN]   - Port: %d", cfg.Port)
    log.Printf("[MAIN]   - DatabasePath: %q", cfg.DatabasePath)
    log.Printf("[MAIN]   - SQLRoot: %q", cfg.SQLRoot)
    log.Printf("[MAIN]   - SchemaPath: %q", cfg.SchemaPath)  // This is key!
    log.Printf("[MAIN]   - BaseURL: %q", cfg.BaseURL)
    log.Printf("[MAIN]   - DebugMode: %v", cfg.DebugMode)
    log.Printf("[MAIN]   - EnableCORS: %v", cfg.EnableCORS)

    // Parse command line flags
    var (
        port     = flag.Int("port", cfg.Port, "HTTP server port")
        portShort = flag.Int("p", cfg.Port, "HTTP server port (shorthand)")
        dbPath   = flag.String("db", cfg.DatabasePath, "Database file path")
        sqlRoot  = flag.String("sql", cfg.SQLRoot, "SQL files root directory")
        baseURL  = flag.String("base", cfg.BaseURL, "API base URL")
        debug    = flag.Bool("debug", cfg.DebugMode, "Enable debug mode")
        cors     = flag.Bool("cors", cfg.EnableCORS, "Enable CORS")
        help     = flag.Bool("help", false, "Show help")
        test     = flag.Bool("test", false, "Run endpoint tests")
        runsetup   = flag.Bool("setup", false, "Run initial setup")
    )
    flag.Parse()

    log.Printf("[MAIN] Command line arguments parsed:")
    log.Printf("[MAIN]   - port flag: %d", *port)
    log.Printf("[MAIN]   - db flag: %q", *dbPath)
    log.Printf("[MAIN]   - sql flag: %q", *sqlRoot)
    log.Printf("[MAIN]   - base flag: %q", *baseURL)
    log.Printf("[MAIN]   - debug flag: %v", *debug)
    log.Printf("[MAIN]   - cors flag: %v", *cors)

    if *help {
        ShowHelp()
        return
    }

    // Update config with flags (prefer explicit port flag over shorthand)
    if flag.Lookup("port").Value.String() != fmt.Sprint(cfg.Port) {
        log.Printf("[MAIN] Updating port from flag: %d -> %d", cfg.Port, *port)
        cfg.Port = *port
    } else if flag.Lookup("p").Value.String() != fmt.Sprint(cfg.Port) {
        log.Printf("[MAIN] Updating port from shorthand flag: %d -> %d", cfg.Port, *portShort)
        cfg.Port = *portShort
    }

    if *dbPath != cfg.DatabasePath {
        log.Printf("[MAIN] Updating database path: %q -> %q", cfg.DatabasePath, *dbPath)
        cfg.DatabasePath = *dbPath
    }

    if *sqlRoot != cfg.SQLRoot {
        log.Printf("[MAIN] Updating SQL root: %q -> %q", cfg.SQLRoot, *sqlRoot)
        cfg.SQLRoot = *sqlRoot
        // IMPORTANT: SchemaPath should be updated when SQLRoot changes!
        cfg.SchemaPath = filepath.Join(*sqlRoot, "schema.sql")
        log.Printf("[MAIN] Updated schema path to: %q", cfg.SchemaPath)
    }

    if *baseURL != cfg.BaseURL {
        log.Printf("[MAIN] Updating base URL: %q -> %q", cfg.BaseURL, *baseURL)
        cfg.BaseURL = *baseURL
    }

    if *debug != cfg.DebugMode {
        log.Printf("[MAIN] Updating debug mode: %v -> %v", cfg.DebugMode, *debug)
        cfg.DebugMode = *debug
    }

    if *cors != cfg.EnableCORS {
        log.Printf("[MAIN] Updating CORS: %v -> %v", cfg.EnableCORS, *cors)
        cfg.EnableCORS = *cors
    }

    log.Printf("[MAIN] Final configuration:")
    log.Printf("[MAIN]   - Port: %d", cfg.Port)
    log.Printf("[MAIN]   - DatabasePath: %q", cfg.DatabasePath)
    log.Printf("[MAIN]   - SQLRoot: %q", cfg.SQLRoot)
    log.Printf("[MAIN]   - SchemaPath: %q", cfg.SchemaPath)  // This is the critical one!
    log.Printf("[MAIN]   - BaseURL: %q", cfg.BaseURL)
    log.Printf("[MAIN]   - DebugMode: %v", cfg.DebugMode)
    log.Printf("[MAIN]   - EnableCORS: %v", cfg.EnableCORS)

    // Validate configuration
    if cfg.Port < 1 || cfg.Port > 65535 {
        log.Fatalf("❌ Invalid port: %d (must be 1-65535)", cfg.Port)
    }

    if cfg.SQLRoot == "" {
        log.Fatalf("❌ SQL root directory cannot be empty")
    }

    // Check if SQL root directory exists
    if _, err := os.Stat(cfg.SQLRoot); os.IsNotExist(err) {
        log.Printf("[MAIN] WARNING: SQL root directory does not exist: %q", cfg.SQLRoot)
    } else {
        log.Printf("[MAIN] SQL root directory exists: %q", cfg.SQLRoot)
    }

    // Check if schema file exists at expected location
    if _, err := os.Stat(cfg.SchemaPath); os.IsNotExist(err) {
        log.Printf("[MAIN] WARNING: Schema file does not exist: %q", cfg.SchemaPath)
    } else {
        log.Printf("[MAIN] Schema file exists: %q", cfg.SchemaPath)
    }

    // Run setup if requested or if setup is incomplete
    if *runsetup || !IsSetupComplete(cfg) {
        log.Println("🔧 Running initial setup...")
        if err := RunSetup(cfg); err != nil {
            log.Fatalf("❌ Setup failed: %v", err)
        }
        log.Println("✅ Setup completed successfully")
    }

    // Initialize directory structure
    log.Println("📁 Initializing directory structure...")
    dir := setup.NewDir(cfg.SQLRoot)
    if err := dir.MakeDirs(); err != nil {
        log.Fatalf("❌ Failed to create directories: %v", err)
    }

    // Discover tables and create table directories
    tables, err := dir.DiscoverTables()
    if err != nil {
        log.Fatalf("❌ Failed to discover tables: %v", err)
    }

    if len(tables) > 0 {
        log.Printf("📊 Found %d tables: %v", len(tables), tables)
        if err := dir.CreateTableDirs(tables); err != nil {
            log.Fatalf("❌ Failed to create table directories: %v", err)
        }
    } else {
        log.Println("⚠️  No tables found in schema.sql")
    }

    // Initialize database
    log.Println("💾 Initializing database...")

    // Enhanced schema loading with detailed logging
    var schemaContent string
    log.Printf("[SCHEMA] Attempting to load schema from: %q", cfg.SchemaPath)

    if cfg.SchemaPath == "" {
        log.Printf("[SCHEMA] WARNING: cfg.SchemaPath is empty!")
    } else {
        // Check if file exists first
        if _, err := os.Stat(cfg.SchemaPath); os.IsNotExist(err) {
            log.Printf("[SCHEMA] ERROR: Schema file does not exist: %q", cfg.SchemaPath)
        } else {
            log.Printf("[SCHEMA] Schema file exists, attempting to load...")

            schemaFile, err := database.LoadSQL(cfg.SchemaPath)
            if err != nil {
                log.Printf("[SCHEMA] ERROR: Failed to load schema file: %v", err)
            } else {
                log.Printf("[SCHEMA] Successfully loaded schema file")
                log.Printf("[SCHEMA] Schema file path: %q", schemaFile.Path)
                log.Printf("[SCHEMA] Schema content length: %d", len(schemaFile.Content))
                log.Printf("[SCHEMA] Schema is empty: %v", schemaFile.IsEmpty())

                if !schemaFile.IsEmpty() {
                    schemaContent = schemaFile.Content
                    if len(schemaContent) > 200 {
                        log.Printf("[SCHEMA] Schema content (first 200 chars): %q", schemaContent[:200]+"...")
                    } else {
                        log.Printf("[SCHEMA] Schema content: %q", schemaContent)
                    }
                } else {
                    log.Printf("[SCHEMA] WARNING: Schema file is empty!")
                }
            }
        }
    }

    log.Printf("[SCHEMA] Final schemaContent length: %d", len(schemaContent))

    db, err := database.NewDatabase(database.Config{
        Path:              cfg.DatabasePath,
        CreateIfNotExists: true,
        Schema:            schemaContent,
    })
    if err != nil {
        log.Fatalf("❌ Failed to initialize database: %v", err)
    }
    defer db.Close()

    // Discover SQL files and create endpoints
    log.Println("🔍 Discovering SQL files...")
    sqlFiles, err := server.GlobSQLFiles(cfg.SQLRoot)
    if err != nil {
        log.Fatalf("❌ Failed to discover SQL files: %v", err)
    }

    var endpoints []server.Endpoint

    // Create endpoints from discovered SQL files
    for _, sqlFile := range sqlFiles {
        endpoint := server.AssembleEndpoint(sqlFile, db, cfg.BaseURL)
        endpoints = append(endpoints, endpoint)
    }

//     // Add default CRUD endpoints for each table
//     for _, table := range tables {
//         defaultEndpoints := server.DefaultRoutesPerTable(table, db)
//         endpoints = append(endpoints, defaultEndpoints...)
//     }

    if len(endpoints) == 0 {
        log.Println("⚠️  No endpoints found. Creating example endpoints...")
        // Create a minimal example if no endpoints exist
        endpoints = createExampleEndpoints(db, cfg.BaseURL)
    }

    log.Printf("🚀 Loaded %d endpoints", len(endpoints))

    // Run tests if requested
    if *test {
        log.Println("🧪 Running endpoint tests...")
        if err := RunEndpointTests(endpoints); err != nil {
            log.Fatalf("❌ Tests failed: %v", err)
        }
        log.Println("✅ All tests passed")
    }

    // Create and start server
    log.Println("🌐 Starting HTTP server...")
    srv := server.NewServer(cfg, endpoints)

    if err := srv.Start(); err != nil {
        log.Fatalf("❌ Server failed: %v", err)
    }
}

// IsSetupComplete checks if all required directories and files exist for the application to run
func IsSetupComplete(cfg setup.Config) bool {
    requiredPaths := []string{
        cfg.SQLRoot,
        filepath.Dir(cfg.DatabasePath),
    }

    for _, path := range requiredPaths {
        if _, err := os.Stat(path); os.IsNotExist(err) {
            return false
        }
    }

    return true
}

// RunSetup performs initial setup of directories and files
func RunSetup(cfg setup.Config) error {
    dir := setup.NewDir(cfg.SQLRoot)

    // Create directory structure
    if err := dir.MakeDirs(); err != nil {
        return fmt.Errorf("failed to create directories: %w", err)
    }

    // Create database directory
    if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0755); err != nil {
        return fmt.Errorf("failed to create database directory: %w", err)
    }

    return nil
}

// ShowHelp displays usage information and available command line options
func ShowHelp() {
    fmt.Println("GoSQL - HTTP API Server for SQL Files")
    fmt.Println()
    fmt.Println("USAGE:")
    fmt.Println("  gosql [flags]")
    fmt.Println()
    fmt.Println("FLAGS:")
    fmt.Println("  -port, -p <number>     HTTP server port (default: 8080)")
    fmt.Println("  -db <path>            Database file path (default: gosql_dir/app.db)")
    fmt.Println("  -sql <path>           SQL files root directory (default: gosql_dir/db)")
    fmt.Println("  -base <url>           API base URL (default: /api/v1)")
    fmt.Println("  -debug                Enable debug mode (default: true)")
    fmt.Println("  -cors                 Enable CORS (default: true)")
    fmt.Println("  -runsetup               Run initial setup")
    fmt.Println("  -test                 Run endpoint tests")
    fmt.Println("  -help                 Show this help")
    fmt.Println()
    fmt.Println("EXAMPLES:")
    fmt.Println("  gosql                          # Start with defaults")
    fmt.Println("  gosql -port 3000               # Start on port 3000")
    fmt.Println("  gosql -runsetup                  # Run setup then start")
    fmt.Println("  gosql -test -debug             # Run tests with debug output")
    fmt.Println("  gosql -db ./data/app.db        # Use custom database path")
    fmt.Println()
    fmt.Println("DIRECTORY STRUCTURE:")
    fmt.Println("  gosql_dir/")
    fmt.Println("  ├── app.db                     # SQLite database")
    fmt.Println("  └── db/")
    fmt.Println("      ├── schema.sql             # Database schema")
    fmt.Println("      ├── GET/                   # Universal GET endpoints")
    fmt.Println("      ├── POST/                  # Universal POST endpoints")
    fmt.Println("      ├── PUT/                   # Universal PUT endpoints")
    fmt.Println("      ├── DELETE/                # Universal DELETE endpoints")
    fmt.Println("      └── Tables/")
    fmt.Println("          └── users/             # Table-specific endpoints")
    fmt.Println("              ├── GET/")
    fmt.Println("              ├── POST/")
    fmt.Println("              ├── PUT/")
    fmt.Println("              └── DELETE/")
    fmt.Println()
    fmt.Println("API ENDPOINTS:")
    fmt.Println("  GET  /                         # API documentation")
    fmt.Println("  GET  /health                   # Health check")
    fmt.Println("  *    /api/v1/{table}/{action}  # Generated from SQL files")
}

// RunEndpointTests executes basic tests against all configured endpoints to verify functionality
func RunEndpointTests(endpoints []server.Endpoint) error {
    if len(endpoints) == 0 {
        return fmt.Errorf("no endpoints to test")
    }

    log.Printf("Testing %d endpoints...", len(endpoints))

    // For now, just verify endpoints have required fields
    for i, endpoint := range endpoints {
        if endpoint.Path == "" {
            return fmt.Errorf("endpoint %d: missing path", i)
        }
        if endpoint.Method == "" {
            return fmt.Errorf("endpoint %d: missing method", i)
        }
        if endpoint.Handler == nil {
            return fmt.Errorf("endpoint %d: missing handler", i)
        }
        if endpoint.SQLPath == "" {
            return fmt.Errorf("endpoint %d: missing SQL path", i)
        }

        log.Printf("✓ %s %s", endpoint.Method, endpoint.Path)
    }

    return nil
}

// createExampleEndpoints creates minimal example endpoints when none are found
func createExampleEndpoints(db *database.Database, baseURL string) []server.Endpoint {
    return []server.Endpoint{
        {
            Path:        baseURL + "/example",
            Method:      "GET",
            Handler:     createExampleHandler(),
            SQLPath:     "example.sql",
            TableName:   "",
            IsUniversal: true,
        },
    }
}

// createExampleHandler creates a simple example handler
func createExampleHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        response := map[string]interface{}{
            "success": true,
            "message": "GoSQL is running! Add SQL files to create real endpoints.",
            "timestamp": time.Now().Format(time.RFC3339),
        }
        json.NewEncoder(w).Encode(response)
    }
}
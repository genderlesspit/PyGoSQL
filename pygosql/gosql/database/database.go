// database.go
package database

import (
    "database/sql"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "sync"
    _ "modernc.org/sqlite"
)

// Database wraps a sql.DB connection with thread-safety and additional methods
type Database struct {
    DB     *sql.DB        // Underlying database connection
    Path   string         // Database file path
    mu     sync.RWMutex   // Read-write mutex for thread safety
    closed bool           // Whether the database is closed
}

// Config holds configuration options for database initialization
type Config struct {
    Path              string // Database file path
    CreateIfNotExists bool   // Whether to create database if it doesn't exist
    Schema            string // Optional schema SQL to execute on creation
}

// NewDatabase creates a new Database instance with the given configuration
func NewDatabase(cfg Config) (*Database, error) {
    if cfg.Path == "" {
        cfg.Path = "gosql_dir/gosql.db"
    }

    // Create directory if it doesn't exist
    if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
        return nil, fmt.Errorf("failed to create database directory: %w", err)
    }

    // Open database connection with SQLite pragmas for performance
    dsn := cfg.Path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)"
    conn, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }

    // Enable foreign key constraints
    if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
        log.Printf("Warning: failed to enable foreign keys: %v", err)
    }

    // Set connection pool settings
    conn.SetMaxOpenConns(10)
    conn.SetMaxIdleConns(5)

    // Test the connection
    if err := conn.Ping(); err != nil {
        conn.Close()
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    db := &Database{
        DB:   conn,
        Path: cfg.Path,
    }

    // Apply schema if provided
    log.Printf("[NewDatabase] Checking if schema should be applied...")
    if cfg.Schema != "" {
        log.Printf("[NewDatabase] Schema provided, calling ApplySchema...")
        if err := db.ApplySchema(cfg.Schema); err != nil {
            conn.Close()
            log.Printf("[NewDatabase] ERROR: Failed to apply schema: %v", err)
            return nil, fmt.Errorf("failed to apply schema: %w", err)
        }
        log.Printf("[NewDatabase] Schema applied successfully")
    } else {
        log.Printf("[NewDatabase] WARNING: No schema provided (Config.Schema is empty)")
    }

    return db, nil
}

// ApplySchema executes the provided schema SQL against the database
func (d *Database) ApplySchema(schema string) error {
    d.mu.Lock()
    defer d.mu.Unlock()

    if d.closed {
        return fmt.Errorf("database is closed")
    }

    if strings.TrimSpace(schema) == "" {
        log.Printf("[ApplySchema] WARNING: Schema is empty or whitespace only, returning early")
        return nil
    }

    log.Printf("[ApplySchema] Processing schema...")

    // Clean the schema by removing comments and empty lines
    cleanedSchema := cleanSQLSchema(schema)
    log.Printf("[ApplySchema] Schema after cleaning comments (length: %d)", len(cleanedSchema))
    if len(cleanedSchema) > 300 {
        log.Printf("[ApplySchema] Cleaned schema (first 300 chars): %q", cleanedSchema[:300]+"...")
    } else {
        log.Printf("[ApplySchema] Cleaned schema: %q", cleanedSchema)
    }


    // Ensure CREATE TABLE statements are idempotent
    fixedSchema := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+`).ReplaceAllString(cleanedSchema, "CREATE TABLE IF NOT EXISTS ")
    log.Printf("[ApplySchema] Schema after fixing CREATE TABLE statements (length: %d)", len(fixedSchema))

    // Split schema into individual statements
    statements := strings.Split(fixedSchema, ";")
    log.Printf("[ApplySchema] Split schema into %d statements", len(statements))

    for i, stmt := range statements {
        stmt = strings.TrimSpace(stmt)
        log.Printf("[ApplySchema] Processing statement %d (length: %d)", i+1, len(stmt))

        if stmt == "" {
            log.Printf("[ApplySchema] Statement %d is empty, skipping", i+1)
            continue
        }

        if len(stmt) > 100 {
            log.Printf("[ApplySchema] Executing schema statement %d: %s...", i+1, stmt[:100])
        } else {
            log.Printf("[ApplySchema] Executing schema statement %d: %s", i+1, stmt)
        }

        if _, err := d.DB.Exec(stmt); err != nil {
            log.Printf("[ApplySchema] ERROR executing statement %d: %v", i+1, err)
            return fmt.Errorf("failed to execute schema statement '%s': %w", stmt, err)
        }
        log.Printf("[ApplySchema] Successfully executed statement %d", i+1)
    }

    log.Printf("[ApplySchema] All schema statements executed successfully")
    return nil
}

// cleanSQLSchema removes comments and empty lines from SQL
func cleanSQLSchema(schema string) string {
    lines := strings.Split(schema, "\n")
    var cleanedLines []string

    for _, line := range lines {
        // Trim whitespace
        trimmed := strings.TrimSpace(line)

        // Skip empty lines
        if trimmed == "" {
            continue
        }

        // Skip comment lines (starting with --)
        if strings.HasPrefix(trimmed, "--") {
            continue
        }

        // Handle inline comments (remove everything after --)
        if commentIndex := strings.Index(trimmed, "--"); commentIndex != -1 {
            trimmed = strings.TrimSpace(trimmed[:commentIndex])
            if trimmed == "" {
                continue
            }
        }

        cleanedLines = append(cleanedLines, trimmed)
    }

    return strings.Join(cleanedLines, "\n")
}

// ExecSQL executes a SQL query and returns whatever the database outputs
func (d *Database) ExecSQL(query string, args ...interface{}) (interface{}, error) {
    log.Printf("Database.ExecSQL called:")
    log.Printf("   - Query: %s", query)
    log.Printf("   - Args: %+v", args)

    d.mu.Lock()
    defer d.mu.Unlock()

    if d.closed {
        return nil, fmt.Errorf("database is closed")
    }

    query = strings.TrimSpace(query)
    if query == "" {
        return nil, fmt.Errorf("empty query")
    }

    // Just execute it and let the database handle everything
    // For SELECT queries, use Query() to get rows
    // For everything else, use Exec() to get result metadata

    queryUpper := strings.ToUpper(strings.TrimSpace(query))
    if strings.HasPrefix(queryUpper, "SELECT") {
        // Return rows as JSON-like structure
        rows, err := d.DB.Query(query, args...)
        if err != nil {
            return nil, err
        }
        defer rows.Close()

        // Convert to simple [][]interface{} or similar
        columns, _ := rows.Columns()
        var results [][]interface{}

        // Add column headers as first row
        headers := make([]interface{}, len(columns))
        for i, col := range columns {
            headers[i] = col
        }
        results = append(results, headers)

        // Add data rows
        for rows.Next() {
            values := make([]interface{}, len(columns))
            valuePtrs := make([]interface{}, len(columns))
            for i := range values {
                valuePtrs[i] = &values[i]
            }

            rows.Scan(valuePtrs...)

            row := make([]interface{}, len(columns))
            for i, val := range values {
                if b, ok := val.([]byte); ok {
                    row[i] = string(b)
                } else {
                    row[i] = val
                }
            }
            results = append(results, row)
        }

        return results, nil
    } else {
        // Just return what Exec() gives us
        result, err := d.DB.Exec(query, args...)
        if err != nil {
            return nil, err
        }

        affected, _ := result.RowsAffected()
        lastId, _ := result.LastInsertId()

        return []interface{}{affected, lastId}, nil
    }
}

// Close closes the database connection and marks it as closed
func (d *Database) Close() error {
    d.mu.Lock()
    defer d.mu.Unlock()

    if d.closed {
        return nil
    }

    d.closed = true
    if d.DB != nil {
        return d.DB.Close()
    }
    return nil
}

// IsHealthy checks if the database connection is still functional
func (d *Database) IsHealthy() bool {
    d.mu.RLock()
    defer d.mu.RUnlock()

    if d.closed || d.DB == nil {
        return false
    }

    return d.DB.Ping() == nil
}

// GetConnection returns the underlying sql.DB connection for advanced usage
func (d *Database) GetConnection() *sql.DB {
    d.mu.RLock()
    defer d.mu.RUnlock()

    if d.closed {
        return nil
    }

    return d.DB
}

// GetPath returns the database file path
func (d *Database) GetPath() string {
    d.mu.RLock()
    defer d.mu.RUnlock()
    return d.Path
}
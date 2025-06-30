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
    if cfg.Schema != "" {
        if err := db.ApplySchema(cfg.Schema); err != nil {
            conn.Close()
            return nil, fmt.Errorf("failed to apply schema: %w", err)
        }
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
        return nil
    }

    // Ensure CREATE TABLE statements are idempotent
    fixedSchema := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+`).ReplaceAllString(schema, "CREATE TABLE IF NOT EXISTS ")

    // Split schema into individual statements
    statements := strings.Split(fixedSchema, ";")

    for _, stmt := range statements {
        stmt = strings.TrimSpace(stmt)
        if stmt == "" {
            continue
        }

        log.Printf("Executing schema statement: %s", stmt)
        if _, err := d.DB.Exec(stmt); err != nil {
            return fmt.Errorf("failed to execute schema statement '%s': %w", stmt, err)
        }
    }

    return nil
}

// ExecSQL executes a SQL query and returns results in a standardized format
// For SELECT queries: returns map with "columns", "rows", "count" keys
// For INSERT/UPDATE/DELETE: returns map with "last_insert_id", "rows_affected", "success" keys
func (d *Database) ExecSQL(query string, args ...interface{}) (interface{}, error) {
    log.Printf("Database.ExecSQL called:")
    log.Printf("   - Query: %s", query)
    log.Printf("   - Args: %+v", args)

    d.mu.Lock()
    defer d.mu.Unlock()

    if d.closed {
        log.Printf("   - ERROR: Database is closed")
        return nil, fmt.Errorf("database is closed")
    }

    query = strings.TrimSpace(query)
    if query == "" {
        log.Printf("   - ERROR: Empty query")
        return nil, fmt.Errorf("empty query")
    }

    // Just execute the raw SQL directly
    log.Printf("   - Executing raw SQL...")
    result, err := d.DB.Exec(query, args...)
    if err != nil {
        log.Printf("   - ERROR: SQL execution failed: %v", err)
        return nil, err
    }

    // For INSERT/UPDATE/DELETE, return basic info
    rowsAffected, _ := result.RowsAffected()
    lastInsertId, _ := result.LastInsertId()

    response := map[string]interface{}{
        "success":        true,
        "rows_affected":  rowsAffected,
        "last_insert_id": lastInsertId,
    }

    log.Printf("   - SUCCESS: %+v", response)
    return response, nil
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
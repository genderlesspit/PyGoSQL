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
    d.mu.Lock()
    defer d.mu.Unlock()

    if d.closed {
        return nil, fmt.Errorf("database is closed")
    }

    query = strings.TrimSpace(query)
    if query == "" {
        return nil, fmt.Errorf("empty query")
    }

    // Determine query type based on the first word
    upperQuery := strings.ToUpper(query)

    if strings.HasPrefix(upperQuery, "SELECT") ||
       strings.HasPrefix(upperQuery, "WITH") ||
       strings.HasPrefix(upperQuery, "EXPLAIN") ||
       strings.HasPrefix(upperQuery, "PRAGMA") {
        return d.executeQuery(query, args...)
    } else {
        return d.executeExec(query, args...)
    }
}

// executeQuery handles SELECT-type queries
func (d *Database) executeQuery(query string, args ...interface{}) (interface{}, error) {
    rows, err := d.DB.Query(query, args...)
    if err != nil {
        return nil, fmt.Errorf("query failed: %w", err)
    }
    defer rows.Close()

    // Get column names
    columns, err := rows.Columns()
    if err != nil {
        return nil, fmt.Errorf("failed to get columns: %w", err)
    }

    // Prepare scanners for each column
    columnCount := len(columns)
    scanArgs := make([]interface{}, columnCount)
    columnPointers := make([]interface{}, columnCount)
    for i := range columnPointers {
        columnPointers[i] = &scanArgs[i]
    }

    // Scan all rows
    var resultRows []map[string]interface{}
    for rows.Next() {
        if err := rows.Scan(columnPointers...); err != nil {
            return nil, fmt.Errorf("failed to scan row: %w", err)
        }

        row := make(map[string]interface{})
        for i, colName := range columns {
            val := scanArgs[i]

            // Handle SQLite types properly
            switch v := val.(type) {
            case []byte:
                row[colName] = string(v)
            case nil:
                row[colName] = nil
            default:
                row[colName] = v
            }
        }
        resultRows = append(resultRows, row)
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("row iteration error: %w", err)
    }

    return map[string]interface{}{
        "columns": columns,
        "rows":    resultRows,
        "count":   len(resultRows),
    }, nil
}

// executeExec handles INSERT, UPDATE, DELETE, CREATE, etc.
func (d *Database) executeExec(query string, args ...interface{}) (interface{}, error) {
    result, err := d.DB.Exec(query, args...)
    if err != nil {
        return nil, fmt.Errorf("exec failed: %w", err)
    }

    lastId, _ := result.LastInsertId()
    rowsAffected, _ := result.RowsAffected()

    return map[string]interface{}{
        "last_insert_id": lastId,
        "rows_affected":  rowsAffected,
        "success":        true,
    }, nil
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
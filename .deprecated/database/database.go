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

// Database holds the database connection and configuration
type Database struct {
	DB     *sql.DB
	Path   string
	mu     sync.RWMutex
	closed bool
}

// Config holds database configuration options
type Config struct {
	Path              string
	CreateIfNotExists bool
	Schema            string // optional schema SQL to run on creation
}

func NewDatabase(cfg Config) (*Database, error) {
  if cfg.Path == "" {
    cfg.Path = "gosql_dir/gosql.db"
  }

  if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
    return nil, fmt.Errorf("mkdir failed: %w", err)
  }

  conn, err := sql.Open(
    "sqlite",
    cfg.Path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)",
  )
  if err != nil {
    return nil, fmt.Errorf("open failed: %w", err)
  }
  if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
    log.Printf("⚠️ foreign_keys pragma failed: %v", err)
  }

  conn.SetMaxOpenConns(10)
  conn.SetMaxIdleConns(5)

  if err := conn.Ping(); err != nil {
    conn.Close()
    return nil, fmt.Errorf("ping failed: %w", err)
  }

  db := &Database{DB: conn, Path: cfg.Path}

  if cfg.Schema != "" {
    // ensure idempotent CREATEs
    fixed := regexp.
      MustCompile(`(?i)CREATE\s+TABLE\s+`).
      ReplaceAllString(cfg.Schema, "CREATE TABLE IF NOT EXISTS ")

    // split and run each stmt with logs
    for _, stmt := range strings.Split(fixed, ";") {
      stmt = strings.TrimSpace(stmt)
      if stmt == "" {
        continue
      }

      log.Printf("migrating schema: %s", stmt)
      if _, err := db.ExecSQL(stmt); err != nil {
        conn.Close()
        return nil, fmt.Errorf("schema migration failed for '%s': %w", stmt, err)
      }
      log.Printf("migrated: %s", stmt)
    }
  }

  return db, nil
}

// ApplySchema applies schema SQL, handling multiple statements
func (d *Database) ApplySchema(schema string) error {
	// Split schema into individual statements
	statements := strings.Split(schema, ";")

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		if _, err := d.ExecSQL(stmt); err != nil {
			return fmt.Errorf("failed to execute schema statement '%s': %w", stmt, err)
		}
	}

	return nil
}

// ExecSQL executes raw SQL and returns the result
// This is the single method for all database interactions
func (d *Database) ExecSQL(query string, args ...interface{}) (interface{}, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, fmt.Errorf("database is closed")
	}

	// Clean up the query
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}

	// Determine if this is a SELECT query or an exec query
	trimmed := strings.ToUpper(query)

	if strings.HasPrefix(trimmed, "SELECT") ||
		strings.HasPrefix(trimmed, "WITH") ||
		strings.HasPrefix(trimmed, "EXPLAIN") ||
		strings.HasPrefix(trimmed, "PRAGMA") {
		// Handle SELECT queries
		return d.executeQuery(query, args...)
	} else {
		// Handle INSERT, UPDATE, DELETE, CREATE, etc.
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

	result := map[string]interface{}{
		"columns": columns,
		"rows":    []map[string]interface{}{},
	}

	// Prepare scanners for each column
	columnCount := len(columns)
	scanArgs := make([]interface{}, columnCount)
	columnPointers := make([]interface{}, columnCount)
	for i := range columnPointers {
		columnPointers[i] = &scanArgs[i]
	}

	// Scan rows
	var resultRows []map[string]interface{}
	for rows.Next() {
		if err := rows.Scan(columnPointers...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, colName := range columns {
			val := scanArgs[i]

			// Handle different SQLite types
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

	result["rows"] = resultRows
	result["count"] = len(resultRows)
	return result, nil
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

// Close closes the database connection
func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}

	d.closed = true
	return d.DB.Close()
}

// IsHealthy checks if the database connection is still good
func (d *Database) IsHealthy() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return false
	}

	return d.DB.Ping() == nil
}

// GetConnection returns the underlying sql.DB connection for advanced use cases
// This should be used sparingly - prefer ExecSQL for consistency
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

// ExecuteFromFile is a convenience method to execute SQL from a file
// This integrates well with the SQLFile type from models
func (d *Database) ExecuteFromFile(path string, args ...interface{}) (interface{}, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read SQL file %s: %w", path, err)
	}

	return d.ExecSQL(string(content), args...)
}
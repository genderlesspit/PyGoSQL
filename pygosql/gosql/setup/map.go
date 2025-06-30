// map.go
package setup

import (
    "fmt"
    "gosql/database"
    "os"
    "path/filepath"
    "regexp"
    "strings"
)

// Dir represents the directory structure for SQL files and database
// <Root>/
// ├── <Database>/
// │   ├── <database>.db
// │   ├── GET/
// │   ├── POST/
// │   ├── DELETE/
// │   └── PUT/
// ├── schema.sql
// └── <Tables>/
//     └── <TableName>/                 # e.g., users, products, whatever
//         ├── GET/
//         │   └── <custom>.sql         # e.g., fetch_users_by_role.sql
//         ├── POST/
//         ├── DELETE/
//         └── PUT/
type Dir struct {
    Root     string // Root directory path
    Database string // Database directory path
    GET      string // GET method SQL files directory
    POST     string // POST method SQL files directory
    DELETE   string // DELETE method SQL files directory
    PUT      string // PUT method SQL files directory
    Schema   string // Schema file path
    Tables   string // Tables directory path
}

// NewDir creates a new Dir instance with calculated paths based on the root directory
func NewDir(root string) *Dir {
    return &Dir{
        Root:     root,
        Database: filepath.Join(root, "database"),
        GET:      filepath.Join(root, "GET"),
        POST:     filepath.Join(root, "POST"),
        DELETE:   filepath.Join(root, "DELETE"),
        PUT:      filepath.Join(root, "PUT"),
        Schema:   filepath.Join(root, "schema.sql"),
        Tables:   filepath.Join(root, "Tables"),
    }
}

// MakeDirs creates all necessary directories in the file system if they don't exist
func (d *Dir) MakeDirs() error {
    // List of directories to create
    dirs := []string{
        d.Root,
        d.Database,
        d.GET,
        d.POST,
        d.DELETE,
        d.PUT,
        d.Tables,
    }

    // Create each directory with 0755 permissions
    for _, dir := range dirs {
        if err := os.MkdirAll(dir, 0755); err != nil {
            return fmt.Errorf("failed to create directory %s: %w", dir, err)
        }
    }

    // Create schema.sql if it doesn't exist
    if _, err := os.Stat(d.Schema); os.IsNotExist(err) {
        schemaContent := `-- Schema definitions
-- Add your CREATE TABLE statements here

-- Example:
-- CREATE TABLE users (
--     id INTEGER PRIMARY KEY AUTOINCREMENT,
--     name TEXT NOT NULL,
--     email TEXT UNIQUE NOT NULL,
--     created_at DATETIME DEFAULT CURRENT_TIMESTAMP
-- );
`
        if err := os.WriteFile(d.Schema, []byte(schemaContent), 0644); err != nil {
            return fmt.Errorf("failed to create schema.sql: %w", err)
        }
    }

    return nil
}

// CreateTableDirs creates subdirectories for each table with HTTP method folders
func (d *Dir) CreateTableDirs(tables []string) error {
    httpMethods := []string{"GET", "POST", "PUT", "DELETE"}

    for _, table := range tables {
        // Create table directory
        tableDir := filepath.Join(d.Tables, table)
        if err := os.MkdirAll(tableDir, 0755); err != nil {
            return fmt.Errorf("failed to create table directory %s: %w", tableDir, err)
        }

        // Create HTTP method subdirectories for each table
        for _, method := range httpMethods {
            methodDir := filepath.Join(tableDir, method)
            if err := os.MkdirAll(methodDir, 0755); err != nil {
                return fmt.Errorf("failed to create method directory %s: %w", methodDir, err)
            }

            // Create default SQL files for each method
            if err := d.createDefaultSQLFile(methodDir, method, table); err != nil {
                return fmt.Errorf("failed to create default SQL file for %s/%s: %w", table, method, err)
            }
        }
    }

    return nil
}

// createDefaultSQLFile creates a default SQL template file for a given method and table
func (d *Dir) createDefaultSQLFile(methodDir, method, table string) error {
    var filename, content string

    switch method {
    case "GET":
        filename = "select.sql"
        content = fmt.Sprintf("SELECT * FROM %s;", table)
    case "POST":
        filename = "insert.sql"
        content = fmt.Sprintf("INSERT INTO %s ({{columns}}) VALUES ({{values}});", table)
    case "PUT":
        filename = "update.sql"
        content = fmt.Sprintf("UPDATE %s SET {{updates}} WHERE id = ?;", table)
    case "DELETE":
        filename = "delete.sql"
        content = fmt.Sprintf("DELETE FROM %s WHERE id = ?;", table)
    default:
        return fmt.Errorf("unsupported HTTP method: %s", method)
    }

    filePath := filepath.Join(methodDir, filename)

    // Only create if file doesn't exist (don't overwrite custom SQL)
    if _, err := os.Stat(filePath); os.IsNotExist(err) {
        if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
            return fmt.Errorf("failed to write file %s: %w", filePath, err)
        }
    }

    return nil
}

// DiscoverTables parses the schema.sql file and extracts table names from CREATE TABLE statements
func (d *Dir) DiscoverTables() ([]string, error) {
    // Load schema.sql using the database package
    sqlFile, err := database.LoadSQL(d.Schema)
    if err != nil {
        return nil, fmt.Errorf("failed to load schema file: %w", err)
    }

    if sqlFile.IsEmpty() {
        return []string{}, nil
    }

    // Regular expression to match CREATE TABLE statements
    // Matches: CREATE TABLE table_name, CREATE TABLE IF NOT EXISTS table_name, etc.
    tableRegex := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["']?(\w+)["']?\s*\(`)

    matches := tableRegex.FindAllStringSubmatch(sqlFile.Content, -1)

    var tables []string
    seenTables := make(map[string]bool) // To avoid duplicates

    for _, match := range matches {
        if len(match) > 1 {
            tableName := strings.ToLower(match[1])
            // Skip sqlite internal tables
            if !strings.HasPrefix(tableName, "sqlite_") && !seenTables[tableName] {
                tables = append(tables, tableName)
                seenTables[tableName] = true
            }
        }
    }

    return tables, nil
}
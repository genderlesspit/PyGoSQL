package models

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Dir holds base paths for different HTTP method folders
type Dir struct {
	Root        string
	Database    string
	GET         string
	POST        string
	DELETE      string
	PUT         string
	Schema      string
	Tables      string
}

// MkDirs creates method subfolders, a tables folder, and schema.sql if missing
func (d *Dir) MkDirs() error {
	if d.GET == "" {
		d.GET = filepath.Join(d.Root, "GET")
	}
	if d.POST == "" {
		d.POST = filepath.Join(d.Root, "POST")
	}
	if d.DELETE == "" {
		d.DELETE = filepath.Join(d.Root, "DELETE")
	}
	if d.PUT == "" {
		d.PUT = filepath.Join(d.Root, "PUT")
	}
	if d.Tables == "" {
		d.Tables = filepath.Join(d.Root, "Tables")
	}
	if d.Schema == "" {
		d.Schema = filepath.Join(d.Root, "schema.sql")
	}

	// create all dirs
	dirs := []string{d.Root, d.GET, d.POST, d.DELETE, d.PUT, d.Tables}
	for _, path := range dirs {
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}
	}

	// create empty schema.sql if it doesn't exist
	if _, err := os.Stat(d.Schema); os.IsNotExist(err) {
		if err := os.WriteFile(d.Schema, []byte("-- define your schema here\n"), 0644); err != nil {
			return fmt.Errorf("failed to create schema.sql: %w", err)
		}
	}

	return nil
}

// SQLFile represents a raw .sql file from disk
type SQLFile struct {
	Path    string // full path like db/POST/insert.sql
	Content string // the query string
}

// LoadSQL reads a file and returns an SQLFile
func LoadSQL(path string) (SQLFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SQLFile{}, err
	}
	return SQLFile{Path: path, Content: string(raw)}, nil
}

// CreateTableDirs creates directories for tables found in schema.sql
func (d *Dir) CreateTableDirs() error {
	// ensure schema.sql exists
	if _, err := os.Stat(d.Schema); os.IsNotExist(err) {
		err := os.WriteFile(d.Schema, []byte(""), 0644)
		if err != nil {
			return fmt.Errorf("failed to create empty schema.sql: %w", err)
		}
	}

	schema, err := LoadSQL(d.Schema)
	if err != nil {
		return fmt.Errorf("failed to load schema.sql: %w", err)
	}

	re := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+["']?([\w]+)["']?`)
	matches := re.FindAllStringSubmatch(schema.Content, -1)

	// no tables? inject a default test table into schema.sql
	if len(matches) == 0 {
		testTable := "CREATE TABLE test (id INTEGER PRIMARY KEY);\n"
		if err := os.WriteFile(d.Schema, []byte(testTable), 0644); err != nil {
			return fmt.Errorf("failed to write test schema: %w", err)
		}
		schema, _ = LoadSQL(d.Schema)
		matches = re.FindAllStringSubmatch(schema.Content, -1)
	}

	// Create table directories organized by method
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	for _, match := range matches {
		table := match[1]
		if table == "" {
			continue
		}

		// Create method subdirectories for each table
		for _, method := range methods {
			dir := filepath.Join(d.Tables, table, method)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create folder %s for table %s: %w", method, table, err)
			}
		}
	}

	return nil
}

// ProvisionDefaults creates default .sql files for each method if missing (templates only)
func (d *Dir) ProvisionDefaults() error {
	defaults := map[string]map[string]string{
		"GET": {
			"select.sql": `SELECT * FROM {{table}};`,
		},
		"POST": {
			"insert.sql": `INSERT INTO {{table}} ({{columns}}) VALUES ({{placeholders}});`,
		},
		"PUT": {
			"update.sql": `UPDATE {{table}} SET {{updates}} WHERE id = ?;`,
		},
		"DELETE": {
			"delete.sql": `DELETE FROM {{table}} WHERE id = ?;`,
		},
	}

	dirs := map[string]string{
		"GET":    d.GET,
		"POST":   d.POST,
		"PUT":    d.PUT,
		"DELETE": d.DELETE,
	}

	for method, files := range defaults {
		baseDir := dirs[method]
		for name, content := range files {
			path := filepath.Join(baseDir, name)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					return fmt.Errorf("failed to write %s: %w", path, err)
				}
			}
		}
	}

	return nil
}

// ProvisionTableDefaults creates universal methods for each table organized by method
func (d *Dir) ProvisionTableDefaults() error {
	// Universal method templates organized by HTTP method
	universalMethods := map[string]map[string]string{
		"GET": {
			"select.sql": `SELECT * FROM {{table}};`,
		},
		"POST": {
			"insert.sql": `INSERT INTO {{table}} ({{columns}}) VALUES ({{placeholders}});`,
		},
		"PUT": {
			"update.sql": `UPDATE {{table}} SET {{updates}} WHERE id = ?;`,
		},
		"DELETE": {
			"delete.sql": `DELETE FROM {{table}} WHERE id = ?;`,
		},
	}

	// Get all table directories
	tableDirs, err := os.ReadDir(d.Tables)
	if err != nil {
		return fmt.Errorf("failed to read tables directory: %w", err)
	}

	// For each table, create universal method files organized by HTTP method
	for _, td := range tableDirs {
		if !td.IsDir() {
			continue
		}

		tableName := td.Name()

		// Create universal SQL files for each HTTP method
		for httpMethod, files := range universalMethods {
			methodDir := filepath.Join(d.Tables, tableName, httpMethod)

			for filename, template := range files {
				sqlPath := filepath.Join(methodDir, filename)

				// Only create if it doesn't exist (allows custom overrides)
				if _, err := os.Stat(sqlPath); os.IsNotExist(err) {
					if err := os.WriteFile(sqlPath, []byte(template), 0644); err != nil {
						return fmt.Errorf("failed to write %s for table %s: %w", filename, tableName, err)
					}
				}
			}
		}
	}

	return nil
}

type Method interface {
	IsUniversal() bool
	GetMethod() string
	GetSQL() SQLFile
	GetName() string
}

type UniversalMethods struct {
	Method string   // e.g. "POST"
	SQL    SQLFile  // compiled file + query content
	Name   string   // name based on file path, like insert
}

func (u UniversalMethods) IsUniversal() bool    { return true }
func (u UniversalMethods) GetMethod() string    { return u.Method }
func (u UniversalMethods) GetSQL() SQLFile      { return u.SQL }
func (u UniversalMethods) GetName() string      { return u.Name }

type TableMethods struct {
	Table  string   // e.g. "users"
	Method string   // e.g. "POST"
	SQL    SQLFile  // content + path
	Name   string   // e.g. "insert"
}

func (t TableMethods) IsUniversal() bool    { return false }
func (t TableMethods) GetMethod() string    { return t.Method }
func (t TableMethods) GetSQL() SQLFile      { return t.SQL }
func (t TableMethods) GetName() string      { return t.Name }

// LoadUniversalMethods now returns empty slice - universal methods not directly callable
func (d *Dir) LoadUniversalMethods() ([]UniversalMethods, error) {
	// Return empty slice - universal methods are not directly callable
	return []UniversalMethods{}, nil
}

// LoadTableMethods loads methods from Tables/tablename/METHOD/ structure
func (d *Dir) LoadTableMethods() ([]TableMethods, error) {
	var out []TableMethods

	tableDirs, err := os.ReadDir(d.Tables)
	if err != nil {
		return nil, err
	}

	// Iterate through each table directory
	for _, td := range tableDirs {
		if !td.IsDir() {
			continue
		}

		tableName := td.Name()
		tablePath := filepath.Join(d.Tables, tableName)

		// Iterate through HTTP method directories (GET, POST, PUT, DELETE)
		methodDirs, err := os.ReadDir(tablePath)
		if err != nil {
			continue
		}

		for _, md := range methodDirs {
			if !md.IsDir() {
				continue
			}

			httpMethod := md.Name()
			methodPath := filepath.Join(tablePath, httpMethod)

			// Load all .sql files in this method directory
			sqlFiles, err := os.ReadDir(methodPath)
			if err != nil {
				continue
			}

			for _, sqlFile := range sqlFiles {
				if sqlFile.IsDir() || !strings.HasSuffix(sqlFile.Name(), ".sql") {
					continue
				}

				sqlPath := filepath.Join(methodPath, sqlFile.Name())
				sqlFileContent, err := LoadSQL(sqlPath)
				if err != nil {
					continue
				}

				methodName := strings.TrimSuffix(sqlFile.Name(), ".sql")

				out = append(out, TableMethods{
					Table:  tableName,
					Method: httpMethod,
					SQL:    sqlFileContent,
					Name:   methodName,
				})
			}
		}
	}

	return out, nil
}

// LoadMethods loads only table-scoped methods (no universal methods)
func (d *Dir) LoadMethods() ([]Method, error) {
	var methods []Method

	// Load per-table methods from Tables/tablename/METHOD/ structure
	tableMethods, err := d.LoadTableMethods()
	if err != nil {
		return nil, fmt.Errorf("table methods load fail: %w", err)
	}
	for _, t := range tableMethods {
		methods = append(methods, t)
	}

	return methods, nil
}

// Setup performs all the setup steps and returns the methods
func Setup(root string) ([]Method, error) {
	dir := &Dir{Root: root}

	// Step 1: create base structure
	if err := dir.MkDirs(); err != nil {
		return nil, fmt.Errorf("MkDirs failed: %w", err)
	}

	// Step 2: scaffold tables from schema.sql with method directories
	if err := dir.CreateTableDirs(); err != nil {
		return nil, fmt.Errorf("CreateTableDirs failed: %w", err)
	}

	// Step 3: provision default GET/POST/etc templates (for reference)
	if err := dir.ProvisionDefaults(); err != nil {
		return nil, fmt.Errorf("ProvisionDefaults failed: %w", err)
	}

	// Step 4: provision universal methods for each table
	if err := dir.ProvisionTableDefaults(); err != nil {
		return nil, fmt.Errorf("ProvisionTableDefaults failed: %w", err)
	}

	// Step 5: load all methods (now only table methods)
	methods, err := dir.LoadMethods()
	if err != nil {
		return nil, fmt.Errorf("LoadMethods failed: %w", err)
	}

	return methods, nil
}

func Run() {
	root := "gosql_dir/db"

	methods, err := Setup(root)
	if err != nil {
		fmt.Printf("Setup failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Dirs created or verified in %s\n", root)
	fmt.Printf("Table folders created under %s/Tables\n", root)
	fmt.Println("Universal methods provisioned for each table")
	fmt.Printf("Loaded %d table-scoped methods\n", len(methods))

	for _, m := range methods {
		if tm, ok := m.(TableMethods); ok {
			fmt.Printf("[Table] %s /%s/%s => %s\n", m.GetMethod(), tm.Table, m.GetName(), m.GetSQL().Path)
		}
	}
}
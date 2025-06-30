package setup

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"gosql/database"  // matches your module name
	"gosql/httpsetup" // new HTTP setup package
	"gosql/config"
)

const (
	defaultDBPath     = "gosql_dir/app.db"
	defaultSchemaPath = "gosql_dir/db/schema.sql"
	defaultSQLRoot    = "gosql_dir/db"
	moduleName        = "gosql"
	baseURL           = "/api/v1"
)

// setupResult holds the results of the setup process
type setupResult struct {
	Endpoints    []httpsetup.Endpoint
	DB           *database.Database
	HTTPSetup    *httpsetup.HTTPSetup
	DBPath       string
	EndpointCount int
	TableCount   int
}

func checkAndSetupDependencies() error {
	fmt.Println("Checking dependencies...")

	// Check if go.mod exists
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		fmt.Println("Initializing Go module...")
		cmd := exec.Command("go", "mod", "init", moduleName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to init go module: %w", err)
		}
		fmt.Println("✓ Go module initialized")
	} else {
		fmt.Println("✓ Go module exists")
	}

	// Check if pure Go SQLite driver is available
	cmd := exec.Command("go", "list", "-m", "modernc.org/sqlite")
	if err := cmd.Run(); err != nil {
		fmt.Println("Installing pure Go SQLite driver...")
		installCmd := exec.Command("go", "get", "modernc.org/sqlite")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("failed to install SQLite driver: %w", err)
		}
		fmt.Println("✓ Pure Go SQLite driver installed")
	} else {
		fmt.Println("✓ SQLite driver available")
	}

	return nil
}

func setupHTTPEndpoints() (*httpsetup.HTTPSetup, []httpsetup.Endpoint, error) {
	fmt.Println("Setting up HTTP endpoints and directory structure...")

	cfg := config.Config{
		DatabasePath: defaultDBPath,
		SQLRoot:      defaultSQLRoot,
		SchemaPath:   defaultSchemaPath,
		BaseURL:      baseURL,
		EnableCORS:   true,
		DebugMode:    true,
	}

	httpSetup, err := httpsetup.NewHTTPSetup(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create HTTP setup: %w", err)
	}

	endpoints, err := httpSetup.Setup()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup endpoints: %w", err)
	}

	fmt.Printf("✓ Successfully provisioned %d HTTP endpoints\n", len(endpoints))
	return httpSetup, endpoints, nil
}

func testDatabase(httpSetup *httpsetup.HTTPSetup) (int, error) {
	if !httpSetup.GetDatabaseHealth() {
		return 0, fmt.Errorf("database connection is not healthy")
	}

	// We'll access the database through a test endpoint or create a temporary connection
	db, err := database.NewDatabase(database.Config{
		Path:              defaultDBPath,
		CreateIfNotExists: true,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create test database connection: %w", err)
	}
	defer db.Close()

	// Query for tables
	result, err := db.ExecSQL("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%';")
	if err != nil {
		return 0, fmt.Errorf("failed to query tables: %w", err)
	}

	// Extract table count from result
	if resultMap, ok := result.(map[string]interface{}); ok {
		if count, ok := resultMap["count"].(int); ok {
			return count, nil
		}
	}

	return 0, nil
}

func displayEndpoints(endpoints []httpsetup.Endpoint) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("PROVISIONED HTTP ENDPOINTS")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("%-8s %-35s %-15s %-20s %s\n", "METHOD", "PATH", "TYPE", "TABLE", "SQL FILE")
	fmt.Println(strings.Repeat("-", 80))

	for _, endpoint := range endpoints {
		endpointType := "Universal"
		tableName := "-"

		if !endpoint.IsUniversal {
			endpointType = "Table-specific"
			tableName = endpoint.TableName
		}

		// Shorten the SQL path for display
		displayPath := endpoint.SQLPath
		if len(displayPath) > 30 {
			displayPath = "..." + displayPath[len(displayPath)-27:]
		}

		fmt.Printf("%-8s %-35s %-15s %-20s %s\n",
			endpoint.HTTPMethod,
			endpoint.Path,
			endpointType,
			tableName,
			displayPath)
	}
}

func displayDatabaseInfo(tableCount int) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("DATABASE INFORMATION")
	fmt.Println(strings.Repeat("=", 80))

	fmt.Printf("Database Path: %s\n", defaultDBPath)
	fmt.Printf("Schema Path: %s\n", defaultSchemaPath)
	fmt.Printf("SQL Root: %s\n", defaultSQLRoot)
	fmt.Printf("Tables Found: %d\n", tableCount)

	// Show tables if any exist
	if tableCount > 0 {
		db, err := database.NewDatabase(database.Config{
			Path:              defaultDBPath,
			CreateIfNotExists: false,
		})
		if err == nil {
			defer db.Close()
			result, err := db.ExecSQL("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%';")
			if err == nil {
				if resultMap, ok := result.(map[string]interface{}); ok {
					if rows, ok := resultMap["rows"].([]map[string]interface{}); ok {
						fmt.Println("Table Names:")
						for _, row := range rows {
							if tableName, ok := row["name"].(string); ok {
								fmt.Printf("  - %s\n", tableName)
							}
						}
					}
				}
			}
		}
	}
}

func displayEndpointsByType(endpoints []httpsetup.Endpoint) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("ENDPOINTS BY TYPE")
	fmt.Println(strings.Repeat("=", 80))

	// Group by type
	universal := []httpsetup.Endpoint{}
	tableSpecific := make(map[string][]httpsetup.Endpoint)

	for _, ep := range endpoints {
		if ep.IsUniversal {
			universal = append(universal, ep)
		} else {
			if tableSpecific[ep.TableName] == nil {
				tableSpecific[ep.TableName] = []httpsetup.Endpoint{}
			}
			tableSpecific[ep.TableName] = append(tableSpecific[ep.TableName], ep)
		}
	}

	// Display universal endpoints
	if len(universal) > 0 {
		fmt.Printf("\nUniversal Endpoints (%d):\n", len(universal))
		for _, ep := range universal {
			fmt.Printf("  %s %s\n", ep.HTTPMethod, ep.Path)
		}
	}

	// Display table-specific endpoints
	if len(tableSpecific) > 0 {
		for table, eps := range tableSpecific {
			fmt.Printf("\nTable '%s' Endpoints (%d):\n", table, len(eps))
			for _, ep := range eps {
				fmt.Printf("  %s %s\n", ep.HTTPMethod, ep.Path)
			}
		}
	}
}

func displayUsageExamples(endpoints []httpsetup.Endpoint) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("USAGE EXAMPLES")
	fmt.Println(strings.Repeat("=", 80))

	fmt.Println("\nTo use these endpoints in an HTTP server:")
	fmt.Println()
	fmt.Println("```go")
	fmt.Println("package main")
	fmt.Println()
	fmt.Println("import (")
	fmt.Println("    \"net/http\"")
	fmt.Println("    \"gosql/httpsetup\"")
	fmt.Println(")")
	fmt.Println()
	fmt.Println("func main() {")
	fmt.Println("    // Get the endpoints")
	fmt.Println("    endpoints, err := httpsetup.SetupHTTP(httpsetup.Config{")
	fmt.Printf("        DatabasePath: \"%s\",\n", defaultDBPath)
	fmt.Printf("        SQLRoot:      \"%s\",\n", defaultSQLRoot)
	fmt.Printf("        BaseURL:      \"%s\",\n", baseURL)
	fmt.Println("        EnableCORS:   true,")
	fmt.Println("        DebugMode:    true,")
	fmt.Println("    })")
	fmt.Println("    if err != nil {")
	fmt.Println("        panic(err)")
	fmt.Println("    }")
	fmt.Println()
	fmt.Println("    // Register with http.ServeMux")
	fmt.Println("    mux := http.NewServeMux()")
	fmt.Println("    for _, ep := range endpoints {")
	fmt.Println("        mux.HandleFunc(ep.Path, ep.Handler)")
	fmt.Println("    }")
	fmt.Println()
	fmt.Println("    // Start server")
	fmt.Println("    http.ListenAndServe(\":8080\", mux)")
	fmt.Println("}")
	fmt.Println("```")

	// Show a few example HTTP requests
	if len(endpoints) > 0 {
		fmt.Println("\nExample HTTP requests:")
		count := 0
		for _, ep := range endpoints {
			if count >= 3 { // Show max 3 examples
				break
			}
			fmt.Printf("  curl -X %s http://localhost:8080%s\n", ep.HTTPMethod, ep.Path)
			count++
		}
		if len(endpoints) > 3 {
			fmt.Printf("  ... and %d more endpoints\n", len(endpoints)-3)
		}
	}
}

func displaySummary(result setupResult) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("GOSQL HTTP SETUP COMPLETE")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("✓ HTTP endpoints provisioned: %d\n", result.EndpointCount)
	fmt.Printf("✓ Database created: %s\n", result.DBPath)
	fmt.Printf("✓ Tables available: %d\n", result.TableCount)
	fmt.Printf("✓ Base URL: %s\n", baseURL)
	fmt.Printf("✓ CORS enabled: Yes\n")
	fmt.Printf("✓ Debug mode: Yes\n")
	fmt.Println("\nYour GoSQL HTTP API system is ready!")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Customize your SQL files in " + defaultSQLRoot + "/")
	fmt.Println("  2. Add tables to schema.sql")
	fmt.Println("  3. Use the code example above to create your HTTP server")
	fmt.Println("  4. Test your API endpoints with the curl examples")
}

func cleanup(httpSetup *httpsetup.HTTPSetup) {
	if httpSetup != nil {
		if err := httpSetup.Close(); err != nil {
			log.Printf("Warning: Failed to close HTTP setup: %v", err)
		}
	}
}

func RunSetup() {
	fmt.Println("GoSQL HTTP Auto-Setup Starting...")
	fmt.Println(strings.Repeat("=", 50))

	var result setupResult
	var err error

	// Step 1: Check and setup dependencies
	if err := checkAndSetupDependencies(); err != nil {
		log.Printf("Dependency setup failed: %v", err)
		fmt.Println("\nPlease run manually:")
		fmt.Println("  go mod init " + moduleName)
		fmt.Println("  go get modernc.org/sqlite")
		os.Exit(1)
	}

	// Step 2: Setup HTTP endpoints (this handles everything: dirs, database, routes)
	result.HTTPSetup, result.Endpoints, err = setupHTTPEndpoints()
	if err != nil {
		log.Fatalf("HTTP endpoint setup failed: %v", err)
	}
	defer cleanup(result.HTTPSetup)

	result.EndpointCount = len(result.Endpoints)
	result.DBPath = defaultDBPath

	// Step 3: Test database and get table count
	result.TableCount, err = testDatabase(result.HTTPSetup)
	if err != nil {
		log.Printf("Database test failed: %v", err)
		// Continue anyway
		result.TableCount = 0
	}

	// Display results
	displayEndpoints(result.Endpoints)
	displayEndpointsByType(result.Endpoints)
	displayDatabaseInfo(result.TableCount)
	displayUsageExamples(result.Endpoints)
	displaySummary(result)
}
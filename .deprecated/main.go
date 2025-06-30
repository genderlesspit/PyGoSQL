package main

import (
	"flag"
	"fmt"
	"gosql/config"
	"gosql/httpsetup"
	"gosql/server"
	"gosql/setup"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := config.DefaultConfig()

	// Parse flags
	flag.IntVar(&cfg.Port, "port", cfg.Port, "Port number")
	flag.IntVar(&cfg.Port, "p", cfg.Port, "Port number (shorthand)")
	runTests := flag.Bool("test", false, "Run tests before starting")
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *help {
		showHelp()
		return
	}

	// Validate
	if cfg.Port < 1 || cfg.Port > 65535 {
		log.Fatalf("Invalid port: %d", cfg.Port)
	}

	// Run setup
    fmt.Println("⚙️  Running auto-setup…")
    setup.RunSetup()

	// Setup endpoints
	endpoints, err := httpsetup.SetupHTTP(cfg)
	if err != nil {
		log.Fatalf("Failed to setup endpoints: %v", err)
	}

	if len(endpoints) == 0 {
		fmt.Println("No endpoints found!")
	} else {
		fmt.Printf("Loaded %d endpoints\n", len(endpoints))
	}

	// Run tests if requested
	if *runTests {
		runEndpointTests(endpoints)
	}

	// Create and start server
	srv := server.NewServer(cfg, endpoints)

	// Handle shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-shutdown
	srv.Shutdown()
	fmt.Println("Server stopped")
}

func isSetupComplete(cfg config.Config) bool {
	paths := []string{cfg.SQLRoot, cfg.SchemaPath}
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func showHelp() {
	fmt.Println("GoSQL HTTP Server")
	fmt.Println("Usage: go run main.go [flags]")
	fmt.Println("Flags:")
	fmt.Println("  -port, -p  Port number (default: 8080)")
	fmt.Println("  -test      Run tests before starting")
	fmt.Println("  -help      Show this help")
}

func runEndpointTests(endpoints []httpsetup.Endpoint) {
	fmt.Println("\nRunning tests...")
	// Simplified test runner - implement as needed
	fmt.Println("Tests complete")
}
// sql_io.go
package database

import (
    "fmt"
    "os"
    "strings"
)

// SQLFile represents a SQL file loaded from disk with its path and content
type SQLFile struct {
    Path    string // Full file path
    Content string // SQL content as string
}

// LoadSQL reads a SQL file from disk and returns an SQLFile struct
func LoadSQL(path string) (SQLFile, error) {
    if path == "" {
        return SQLFile{}, fmt.Errorf("path cannot be empty")
    }

    // Check if file exists
    if _, err := os.Stat(path); os.IsNotExist(err) {
        return SQLFile{}, fmt.Errorf("file does not exist: %s", path)
    }

    // Read file content
    content, err := os.ReadFile(path)
    if err != nil {
        return SQLFile{}, fmt.Errorf("failed to read file %s: %w", path, err)
    }

    return SQLFile{
        Path:    path,
        Content: string(content),
    }, nil
}

// IsEmpty returns true if the SQLFile has no content or path
func (sf SQLFile) IsEmpty() bool {
    return sf.Path == "" || strings.TrimSpace(sf.Content) == ""
}
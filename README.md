# PyGoSQL

[![Python Version](https://img.shields.io/badge/python-3.8%2B-blue.svg)](https://python.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Code Style: Black](https://img.shields.io/badge/code%20style-black-000000.svg)](https://github.com/psf/black)

**A powerful Python wrapper for HTTPS-based SQL operations via Go backend services.**

### Basic Usage

```python
import asyncio
from pathlib import Path
from pygosql import PyGoSQL

async def main():
    # Initialize client with your Go SQL server
    client = PyGoSQL(
        sql_root=Path("./sql"),           # Directory containing SQL files
        verbose=True                      # Enable detailed logging
    )
    
    # Launch the server and discover endpoints
    await client.launch()
    
    # Use discovered table namespaces
    users = await client.users.select()
    new_user = await client.users.insert(name="Alice", email="alice@example.com")
    
    # Access server health and documentation
    health_status = await client.health()
    api_docs = await client.docs()
    
    # Clean shutdown
    await client.stop()

# Run the async function
asyncio.run(main())
```

### Advanced Configuration

```python
from pygosql import PyGoSQL
from pathlib import Path

client = PyGoSQL(
    sql_root=Path("./database/queries"),  # Custom SQL directory
    go_file=Path("./server/main.go"),     # Custom Go server location
    db_path=Path("./data/app.db"),        # Database file path
    port=8080,                            # Specific port (auto-assigned if None)
    base_url="/api/v2",                   # Custom API base path
    debug=True,                           # Enable debug mode
    cors=True,                            # Enable CORS headers
    verbose=True                          # Detailed logging
)
```

PyGoSQL follows a specific directory structure that maps SQL files to HTTP endpoints. Understanding this structure is crucial for effective customization.

### Required Directory Layout

```
<sql_root>/
├── <Database>/
│   ├── <database>.db              # SQLite database file
│   ├── GET/                       # Global GET operations
│   │   └── *.sql                  # SQL files for GET endpoints
│   ├── POST/                      # Global POST operations
│   │   └── *.sql                  # SQL files for POST endpoints
│   ├── DELETE/                    # Global DELETE operations
│   │   └── *.sql                  # SQL files for DELETE endpoints
│   └── PUT/                       # Global PUT operations
│       └── *.sql                  # SQL files for PUT endpoints
├── schema.sql                     # Database schema definition
└── <Tables>/
    └── <TableName>/               # Table-specific operations
        ├── GET/
        │   ├── select.sql         # Standard select operation
        │   └── fetch_by_role.sql  # Custom query example
        ├── POST/
        │   └── insert.sql         # Standard insert operation
        ├── DELETE/
        │   └── delete.sql         # Standard delete operation
        └── PUT/
            └── update.sql         # Standard update operation
```

### Example Project Structure

```
project/
├── main.py                        # Your Python application
├── gosql/
│   └── main.go                    # Go server implementation
└── sql/                           # SQL root directory
    ├── database/
    │   ├── app.db                 # SQLite database
    │   ├── GET/
    │   │   └── health_check.sql   # System-wide health check
    │   ├── POST/
    │   │   └── backup.sql         # Database backup operation
    │   ├── DELETE/
    │   └── PUT/
    ├── schema.sql                 # CREATE TABLE statements
    └── tables/
        ├── users/
        │   ├── GET/
        │   │   ├── select.sql     # SELECT * FROM users
        │   │   ├── by_email.sql   # SELECT * FROM users WHERE email = ?
        │   │   └── active.sql     # SELECT * FROM users WHERE active = 1
        │   ├── POST/
        │   │   └── insert.sql     # INSERT INTO users (...)
        │   ├── DELETE/
        │   │   └── delete.sql     # DELETE FROM users WHERE id = ?
        │   └── PUT/
        │       └── update.sql     # UPDATE users SET ... WHERE id = ?
        ├── orders/
        │   ├── GET/
        │   │   ├── select.sql
        │   │   └── by_user.sql    # SELECT * FROM orders WHERE user_id = ?
        │   ├── POST/
        │   │   └── insert.sql
        │   ├── DELETE/
        │   │   └── delete.sql
        │   └── PUT/
        │       └── update.sql
        └── products/
            ├── GET/
            │   ├── select.sql
            │   ├── in_stock.sql   # SELECT * FROM products WHERE stock > 0
            │   └── by_category.sql
            ├── POST/
            │   └── insert.sql
            ├── DELETE/
            │   └── delete.sql
            └── PUT/
                └── update.sql
```

### Endpoint Mapping

PyGoSQL automatically maps your directory structure to HTTP endpoints:

| File Path | HTTP Method | Generated Endpoint | Python Function |
|-----------|-------------|-------------------|-----------------|
| `tables/users/GET/select.sql` | GET | `/api/v1/users/select` | `client.users.select()` |
| `tables/users/GET/by_email.sql` | GET | `/api/v1/users/by_email` | `client.users.by_email()` |
| `tables/users/POST/insert.sql` | POST | `/api/v1/users/insert` | `client.users.insert()` |
| `tables/orders/GET/by_user.sql` | GET | `/api/v1/orders/by_user` | `client.orders.by_user()` |
| `database/GET/health_check.sql` | GET | `/api/v1/health_check` | `client.system.health_check()` |


### Supports Templating via {{<var>}}

```go
    // Regex to find all {{variable}} patterns
    re := regexp.MustCompile(`\{\{(\w+)\}\}`)

    // Find all matches first for logging
    matches := re.FindAllString(sqlContent, -1)
    log.Printf("   - Found template variables: %v", matches)

    result := re.ReplaceAllStringFunc(sqlContent, func(match string) string {
        // Extract variable name from {{variable}}
        varName := strings.Trim(match, "{}")

        // If variable exists in params, replace it
        if value, exists := allParams[varName]; exists {
            replacement := fmt.Sprintf("%v", value)
            log.Printf("   - Replacing %s with '%s'", match, replacement)
            return replacement
        }

        // Leave unmatched templates as-is
        log.Printf("   - No replacement found for %s - leaving as-is", match)
        return match
    })
```
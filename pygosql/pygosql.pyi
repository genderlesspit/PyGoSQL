import asyncio
import logging
import threading
from functools import cached_property
from pathlib import Path
from typing import Optional, Dict, List, Any, Union, Callable
from dataclasses import dataclass
import aiohttp
from pygops import GoServer
from toomanyports import PortManager
from loguru import logger as log

@dataclass
class Route:
    """Represents a discovered API route."""
    method: str
    path: str
    is_universal: bool
    table_name: str
    sql_path: Optional[str] = None
    description: Optional[str] = None

    @property
    def operation(self) -> str:
        """Extract operation name from path."""
        ...

    @property
    def namespace(self) -> str:
        """Get namespace (table_name or 'system' for system routes)."""
        ...

    @property
    def function_name(self) -> str:
        """Generate function name from operation."""
        ...

class APIRequester:
    """Dynamic HTTP client that builds namespaced functions from discovered routes."""

    def __init__(self, base_url: str, session: aiohttp.ClientSession,
                 routes: List[Route], verbose: bool = False) -> None:
        """Initialize requester with discovered routes.

        Args:
            base_url: Server base URL
            session: HTTP session for requests
            routes: List of discovered routes
            verbose: Enable verbose logging
        """
        self.base_url: str = base_url
        self.session: aiohttp.ClientSession = session
        self.routes: List[Route] = routes
        self.verbose: bool = verbose
        self._namespaces: Dict[str, NamespaceObject] = {}
        self._build_namespaces()
        ...

    def _create_request_function(self, route: Route) -> Callable:
        """Create an async function for a specific route.

        Args:
            route: Route information

        Returns:
            Async function that makes HTTP request to the route
        """
        ...

    def _build_namespaces(self) -> None:
        """Build namespaced functions from routes."""
        ...

    async def _make_request(self, route: Route, **kwargs) -> Dict[str, Any]:
        """Make HTTP request to a route.

        Args:
            route: Route to call
            **kwargs: Parameters/data for the request

        Returns:
            JSON response from server
        """
        ...

    def get_namespace(self, name: str) -> Any:
        """Get a namespace object with its functions.

        Args:
            name: Namespace name (table name or 'system')

        Returns:
            Namespace object with callable functions
        """
        ...

    @property
    def namespaces(self) -> List[str]:
        """Get all available namespace names."""
        ...

class PyGoSQL:
    """Python client for GoSQL server with automatic endpoint discovery."""

    instance: Optional['PyGoSQL'] = None

    def __init__(self,
                 go_file: Path,
                 sql_root: Path,
                 db_path: Path = None,
                 port: Optional[int] = None,
                 base_url: Optional[str] = "/api/v1",
                 debug: bool = False,
                 cors: bool = True,
                 verbose: bool = False) -> None:
        """Initialize PyGoSQL client.

        Args:
            go_file: Path to main.go file
            port: HTTP server port (random if None)
            db_path: Database file path (Go default if None)
            sql_root: SQL files root directory (Go default if None)
            base_url: API base URL
            debug: Enable debug mode in Go server
            cors: Enable CORS in Go server
            verbose: Enable verbose logging
        """
        # Server configuration
        self._port = port or PortManager.random_port()
        self._go_file = go_file
        self._db_path = db_path
        self._sql_root = sql_root
        self._base_url = base_url
        self._debug = debug
        self._cors = cors

        # // Parse command line flags
        # var (
        #     port     = flag.Int("port", cfg.Port, "HTTP server port")
        #     portShort = flag.Int("p", cfg.Port, "HTTP server port (shorthand)")
        #     dbPath   = flag.String("db", cfg.DatabasePath, "Database file path")
        #     sqlRoot  = flag.String("sql", cfg.SQLRoot, "SQL files root directory")
        #     baseURL  = flag.String("base", cfg.BaseURL, "API base URL")
        #     debug    = flag.Bool("debug", cfg.DebugMode, "Enable debug mode")
        #     cors     = flag.Bool("cors", cfg.EnableCORS, "Enable CORS")
        #     help     = flag.Bool("help", false, "Show help")
        #     test     = flag.Bool("test", false, "Run endpoint tests")
        #     runsetup   = flag.Bool("setup", false, "Run initial setup")
        # )
        # flag.Parse()

        # // <Root>/
        # // ├── <Database>/
        # // │   ├── <database>.db
        # // │   ├── GET/
        # // │   ├── POST/
        # // │   ├── DELETE/
        # // │   └── PUT/
        # // ├── schema.sql
        # // └── <Tables>/
        # //     └── <TableName>/                 # e.g., users, products, whatever
        # //         ├── GET/
        # //         │   └── <custom>.sql         # e.g., fetch_users_by_role.sql
        # //         ├── POST/
        # //         ├── DELETE/
        # //         └── PUT/

        # Only hardcoded parts - these are guaranteed to exist
        self._health: Optional[Callable] = None
        self._docs: Optional[Callable] = None

        # Build kwargs for GoServer, excluding None values
        server_kwargs = {
            'go_file': self._go_file,
            'port': self._port,
            'sql_root': self._sql_root,  # Always pass this
            'verbose': verbose
        }

        server_kwargs.update({
            k: v for k, v in {
                'db_path': self._db_path,
                'base_url': self._base_url,
                'debug': self._debug,
                'cors': self._cors
            }.items() if v is not None
        })

        self.server = GoServer(**server_kwargs)

    @cached_property
    def port(self) -> int:
        """Get the server port."""
        return self._port

    @cached_property
    def base_url(self) -> str:
        """Get the base URL for API requests."""
        return f"http://localhost:{self.port}"

    @property
    def server_info(self) -> Optional[Dict[str, Any]]:
        """Get discovered server information."""
        return self._server_info

    @cached_property
    def requester(self) -> Optional[APIRequester]:
        """Get the API requester instance."""
        return self._requester

    async def launch(self) -> None:
        """Launch the GoSQL server and discover endpoints."""
        ...
    async def stop(self) -> None:
        """Stop GoSQL server and clean up resources."""
        ...

    def __getattr__(self, name: str) -> Any:
        """Dynamic namespace access.

        Args:
            name: Namespace name (table name)

        Returns:
            Namespace object with callable functions

        Raises:
            AttributeError: If namespace not found
        """
        ...

    @property
    def tables(self) -> List[str]:
        """Get all table names (non-system namespaces)."""
        ...

    def __repr__(self) -> str:
        """String representation of PyGoSQL instance."""
        ...

# Example of what gets dynamically created:
# client.users.select()  # calls GET /api/v1/users/select
# client.users.insert()  # calls POST /api/v1/users/insert
# client.users.update()  # calls PUT /api/v1/users/update
# client.users.delete()  # calls DELETE /api/v1/users/delete
# client.health()        # calls GET /health (hardcoded)
# client.docs()          # calls GET / (hardcoded)
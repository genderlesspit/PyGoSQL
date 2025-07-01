import logging
from functools import cached_property
from pathlib import Path
from typing import Optional, Dict, List, Any, Callable

import aiohttp
from pygops import GoServer
from toomanyports import PortManager


def setup_logging(verbose: bool = False) -> logging.Logger:
    """Setup logging with toggleable verbosity."""
    ...


class Route:
    """Represents a discovered API route."""
    method: str
    path: str
    is_universal: bool
    table_name: str
    sql_path: Optional[str]
    description: Optional[str]

    def __init__(self, method: str, path: str, is_universal: bool,
                 table_name: str, sql_path: Optional[str] = None,
                 description: Optional[str] = None) -> None:
        """Initialize Route."""
        ...

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


class NamespaceObject:
    """Dynamic object that holds callable functions for a namespace."""

    def __init__(self, name: str):
        self.name = name
        self._functions = {}

    def add_function(self, func_name: str, func: Callable):
        """Add a function to this namespace."""

    @property
    def operations(self) -> List[str]:
        """Get list of available operations."""

    def __repr__(self) -> str:
        """
        :return:
        """

class PyGoSQLError(Exception):
    """Base exception for PyGoSQL errors."""
    pass

class ClientValidationError(PyGoSQLError):
    """Raised when the server returns a 400-level error (client validation)."""
    def __init__(self, message: str, status_code: int, response_data: Dict[str, Any]):
        super().__init__(message)
        self.status_code = status_code
        self.response_data = response_data

class ServerError(PyGoSQLError):
    """Raised when the server returns a 500-level error."""
    pass

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
        self.base_url = base_url
        self.session = session
        self.routes = routes
        self.verbose = verbose
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

    async def _make_request(self, route: Route, **kwargs) -> dict[str | str] | None | Any:
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
                 sql_root: Path = Path.cwd(),
                 go_file: Path = Path("./gosql/main.go"),
                 db_path: Path = None,
                 port: Optional[int] = None,
                 base_url: Optional[str] = "/api/v1",
                 debug: bool = False,
                 cors: bool = True,
                 verbose: bool = False) -> None:
        """Initialize PyGoSQL client."""
        # Server configuration
        self._port = port or PortManager.random_port()
        self._go_file = go_file
        self._db_path = db_path
        self._sql_root = sql_root
        self._base_url = base_url
        self._debug = debug
        self._cors = cors
        self._verbose = verbose

        # Initialize state
        self._session: Optional[aiohttp.ClientSession] = None
        self._requester: Optional[APIRequester] = None
        self._server_info: Optional[Dict[str, Any]] = None
        self._routes: List[Route] = []

        # Only hardcoded parts - these are guaranteed to exist
        self._health: Optional[Callable] = None
        self._docs: Optional[Callable] = None

        # Build kwargs for GoServer, excluding None values
        server_kwargs = {
            'go_file': self._go_file,
            'port': self._port,
            'sql_root': self._sql_root,
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

    @property
    def base_url(self) -> str:
        """Get the base URL for API requests."""
        return f"http://localhost:{self.port}"

    @property
    def server_info(self) -> Optional[Dict[str, Any]]:
        """Get discovered server information."""
        return self._server_info

    @property
    def requester(self) -> Optional[APIRequester]:
        """Get the API requester instance."""
        return self._requester

    async def launch(self) -> None:
        """Launch the GoSQL server and discover endpoints."""
        ...

    async def _start_server(self) -> None:
        """Start the GoSQL server process."""
        ...

    async def _discover_routes(self) -> List[Route]:
        """Discover all routes by fetching root endpoint.

        Returns:
            List of all discovered routes

        Raises:
            aiohttp.ClientError: If server discovery fails
            RuntimeError: If server response is invalid
        """
        ...

    def _setup_hardcoded_functions(self) -> None:
        """Set up the only hardcoded functions (health, docs)."""
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

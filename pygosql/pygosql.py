import asyncio
from dataclasses import dataclass
from functools import cached_property
from pathlib import Path
from typing import Optional, Dict, List, Any, Callable

import aiohttp
from loguru import logger as log
from pygops import GoServer
from toomanyports import PortManager


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
        # Extract last part of path as operation
        path_parts = self.path.rstrip('/').split('/')
        operation = path_parts[-1] if path_parts[-1] else 'root'
        return operation

    @property
    def namespace(self) -> str:
        """Get namespace (table_name or 'system' for system routes)."""
        return self.table_name if self.table_name else 'system'

    @property
    def function_name(self) -> str:
        """Generate function name from operation."""
        operation = self.operation.lower()
        # Handle special cases
        if operation == 'root':
            return 'docs'
        return operation


class NamespaceObject:
    """Dynamic object that holds callable functions for a namespace."""

    def __init__(self, name: str):
        self.name = name
        self._functions = {}

    def add_function(self, func_name: str, func: Callable):
        setattr(self, func_name, func)
        self._functions[func_name] = func

    @property
    def operations(self) -> List[str]:
        return list(self._functions.keys())

    def __repr__(self) -> str:
        ops = ', '.join(self.operations)
        return f"{self.name}({ops})"


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
        """Initialize requester with discovered routes."""
        self.base_url = base_url
        self.session = session
        self.routes = routes
        self.verbose = verbose
        self._namespaces: Dict[str, NamespaceObject] = {}
        self._build_namespaces()

    def _create_request_function(self, route: Route) -> Callable:
        """Create an async function for a specific route."""

        async def request_func(**kwargs) -> Dict[str, Any]:
            return await self._make_request(route, **kwargs)

        # Set function metadata
        request_func.__name__ = route.function_name
        request_func.__doc__ = f"Execute {route.method} {route.path}"

        # if self.verbose: log.debug(f"{self}: Initialized new endpoint:\n - {request_func.__name__}\n - {request_func.__doc__}")

        return request_func

    def _build_namespaces(self) -> None:
        """Build namespaced functions from routes."""
        for route in self.routes:
            namespace_name = route.namespace

            # Create namespace if it doesn't exist
            if namespace_name not in self._namespaces:
                self._namespaces[namespace_name] = NamespaceObject(namespace_name)

            # Create and add function to namespace
            func = self._create_request_function(route)
            self._namespaces[namespace_name].add_function(route.function_name, func)

            if self.verbose: log.debug(
                f"{self}: Added {route.method} {route.path} as {namespace_name}.{route.function_name}")

    async def _make_request(self, route: Route, **kwargs) -> Dict[str, Any]:
        """Make HTTP request to a route."""
        url = f"{self.base_url}{route.path}"

        # Determine request parameters based on method
        request_kwargs = {}
        if route.method.upper() in ['GET', 'DELETE']:
            request_kwargs['params'] = kwargs
        else:
            request_kwargs['json'] = kwargs

        if self.verbose:
            log.debug(f"Making {route.method} request to {url} with {request_kwargs}")

        async with self.session.request(route.method, url, **request_kwargs) as response:
            # Get response data first
            try:
                response_data = await response.json()
            except:
                response_data = {"error": "Invalid JSON response"}

            # Handle different error types
            if response.status >= 400:
                if 400 <= response.status < 500:
                    # Client errors - log warning and return error info
                    error_msg = response_data.get('error', f'Client error: {response.status}')
                    if self.verbose:
                        log.warning(f"Client error ({response.status}): {error_msg}")

                    # Return error info instead of raising (let user check success field)
                    return {
                        "success": False,
                        "error": error_msg,
                        "status_code": response.status,
                        "details": response_data
                    }
                else:
                    # Server errors - these are unexpected, so raise
                    error_msg = response_data.get('error', f'Server error: {response.status}')
                    if self.verbose:
                        log.error(f"Server error ({response.status}): {error_msg}")
                    response.raise_for_status()  # Raise for 500+ errors

            if self.verbose: log.debug(f"{self}: Returning response:\n{response_data}")
            return response_data

    def get_namespace(self, name: str) -> NamespaceObject:
        """Get a namespace object with its functions."""
        if name not in self._namespaces:
            available = ', '.join(self.namespaces)
            raise AttributeError(f"Namespace '{name}' not found. Available: {available}")
        return self._namespaces[name]

    @property
    def namespaces(self) -> List[str]:
        """Get all available namespace names."""
        return list(self._namespaces.keys())


# noinspection PyShadowingNames
class PyGoSQL:
    """Python client for GoSQL server with automatic endpoint discovery."""

    instance: Optional['PyGoSQL'] = None

    def __init__(self,
                 sql_root: Path = None,
                 #go_file: Path = Path("./gosql/main.go"),
                 db_path: Path = None,
                 port: Optional[int] = None,
                 base_url: Optional[str] = "/api/v1",
                 debug: bool = False,
                 cors: bool = True,
                 verbose: bool = False) -> None:
        """Initialize PyGoSQL client."""
        # Server configuration
        self._port = port or PortManager.random_port()
        self._go_file = Path("./gosql/main.go")
        self._sql_root = Path.cwd() / "sql" if sql_root is None else sql_root
        self._db_path = Path.cwd() / "sql" / "app.db" if db_path is None else db_path
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

        go_args = []
        for k, v in {
            'port': self._port,
            'sql': str(self._sql_root.resolve()),  # Changed from 'sql_root' to 'sql'
            'db': self._db_path,                   # Changed from 'db_path' to 'db'
            'base': self._base_url,                # Changed from 'base_url' to 'base'
            'debug': self._debug,
            'cors': self._cors
        }.items():
            if v is not None:
                go_args.extend([f"-{k}", str(v).lower() if isinstance(v, bool) else str(v)])

        # Remove verbose from go_args since it's not a Go flag
        self.server = GoServer(go_file=self._go_file, go_args=go_args, verbose=verbose)

        if self._verbose:
            props = "\n".join(f"{k}: {v}" for k, v in vars(self).items())
            log.success(f"{self} Successfully initialized!\n{props}")

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

    @property
    def requester(self) -> Optional[APIRequester]:
        """Get the API requester instance."""
        return self._requester

    async def launch(self) -> None:
        """Launch the GoSQL server and discover endpoints."""
        if self._verbose: log.debug(f"Starting GoSQL server on port {self.port}")

        # Start the Go server
        await self._start_server()

        # Create HTTP session
        self._session = aiohttp.ClientSession()

        # Discover routes
        await self._discover_routes()

        # Build requester
        self._requester = APIRequester(
            base_url=self.base_url,
            session=self._session,
            routes=self._routes,
            verbose=self._verbose
        )

        # Setup hardcoded functions
        self._setup_hardcoded_functions()

        if self._verbose: log.info(f"PyGoSQL launched successfully with {len(self._routes)} routes")

    async def _start_server(self) -> None:
        """Start the GoSQL server process."""
        await self.server.start()

        # Wait a moment for server to be ready
        await asyncio.sleep(1)

        # Verify server is running
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(f"{self.base_url}/health") as response:
                    if response.status != 200:
                        raise RuntimeError(f"Server health check failed: {response.status}")
        except Exception as e:
            raise RuntimeError(f"Failed to verify server startup: {e}")

    async def _discover_routes(self) -> None:
        """Discover all routes by fetching root endpoint."""
        try:
            async with self._session.get(f"{self.base_url}/") as response:
                response.raise_for_status()
                data = await response.json()

            self._server_info = data

            # Parse API endpoints
            api_routes = []
            for endpoint_data in data.get('endpoints', []):
                route = Route(
                    method=endpoint_data['method'],
                    path=endpoint_data['path'],
                    is_universal=endpoint_data['is_universal'],
                    table_name=endpoint_data.get('table_name', ''),
                    sql_path=endpoint_data.get('sql_path')
                )
                api_routes.append(route)

            # Parse system endpoints
            system_routes = []
            for endpoint_data in data.get('system_endpoints', []):
                route = Route(
                    method=endpoint_data['method'],
                    path=endpoint_data['path'],
                    is_universal=False,
                    table_name='',
                    description=endpoint_data.get('description')
                )
                system_routes.append(route)

            self._routes = api_routes + system_routes
            if self._verbose: log.info(
                f"Discovered {len(api_routes)} API routes and {len(system_routes)} system routes")

        except Exception as e:
            raise RuntimeError(f"Failed to discover routes: {e}")

    def _setup_hardcoded_functions(self) -> None:
        """Set up the only hardcoded functions (health, docs)."""
        # Find health and docs routes
        health_route = None
        docs_route = None

        for route in self._routes:
            if route.path == '/health':
                health_route = route
            elif route.path == '/':
                docs_route = route

        # Create hardcoded functions
        if health_route:
            self._health = self._requester._create_request_function(health_route)

        if docs_route:
            self._docs = self._requester._create_request_function(docs_route)

    async def health(self) -> Dict[str, Any]:
        """Check GoSQL server health via /health endpoint."""
        if not self._health:
            raise RuntimeError("Health endpoint not available")
        return await self._health()

    async def docs(self) -> Dict[str, Any]:
        """Get API documentation via / endpoint."""
        if not self._docs:
            raise RuntimeError("Documentation endpoint not available")
        return await self._docs()

    async def stop(self) -> None:
        """Stop GoSQL server and clean up resources."""
        if self._verbose: log.info("Stopping PyGoSQL...")

        if self._session:
            await self._session.close()
            self._session = None

        if self.server:
            await self.server.stop()

        self._requester = None
        self._server_info = None
        self._routes.clear()

        if self._verbose: log.info("PyGoSQL stopped")

    def __getattr__(self, name: str) -> NamespaceObject:
        """Dynamic namespace access."""
        log.debug(f"Attempting to retrieve attribute, {name}...")
        # Don't intercept private attributes or known instance attributes
        if name.startswith('_') or name in ('server', 'logger'):
            raise AttributeError(f"'{self.__class__.__name__}' object has no attribute '{name}'")

        if not self._requester:
            raise RuntimeError("PyGoSQL not launched. Call await client.launch() first.")

        try:
            return self._requester.get_namespace(name)
        except AttributeError:
            # Check if it's a table name specifically
            available_tables = self.tables
            available_ns = self._requester.namespaces
            raise AttributeError(
                f"Namespace '{name}' not found. "
                f"Available tables: {available_tables}. "
                f"Available namespaces: {available_ns}"
            )

    @property
    def tables(self) -> List[str]:
        """Get all table names (non-system namespaces)."""
        if not self._requester:
            return []

        return [ns for ns in self._requester.namespaces if ns != 'system']

    def __repr__(self) -> str:
        """String representation of PyGoSQL instance."""
        status = "running" if self._requester else "stopped"
        tables = ', '.join(self.tables) if self.tables else 'none'
        route_count = len(self._routes) if self._routes else 0

        return (f"PyGoSQL(status={status}, port={self.port}, "
                f"tables=[{tables}], routes={route_count}, "
                f"api={self.base_url})")


# Example usage:
"""
client = PyGoSQL(
    go_file=Path("./gosql/main.go"),
    sql_root=Path("./sql"),
    db_path=Path("./data/app.db"),
    port=8080,
    debug=True,
    verbose=True
)

await client.launch()

# Dynamic table access
users = await client.users.select()
await client.users.insert(name="John", email="john@test.com")
await client.users.update(id=1, name="Jane")
await client.users.delete(id=1)

# Hardcoded system functions
health = await client.health()
docs = await client.docs()

# Cleanup
await client.stop()
"""


async def debug():
    client = PyGoSQL(
        debug=True,
        verbose=True
    )

    try:
        await client.launch()

        # Your code here
        result = await client.users.insert(name="joe", email="unique@example.com")
        print(f"Result: {result}")

    finally:
        # ALWAYS clean up
        await client.stop()


if __name__ == "__main__":
    asyncio.run(debug())
    # Remove this line: time.sleep(100)

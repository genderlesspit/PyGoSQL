#Requires -Version 5.1

[CmdletBinding()]
param (
    [Parameter(Mandatory=$true, Position=0, HelpMessage="Path to the Go file to execute")]
    [ValidateScript({
        # Handle both absolute and relative paths
        $resolvedPath = if ([System.IO.Path]::IsPathRooted($_)) {
            # Absolute path - use as-is
            $_
        } else {
            # Relative path - resolve against current working directory
            Join-Path (Get-Location) $_
        }

        if (-not (Test-Path $resolvedPath)) {
            throw "Go file not found: $resolvedPath (original: $_)"
        }
        if (-not $resolvedPath.EndsWith(".go")) {
            throw "File must have .go extension: $resolvedPath"
        }
        return $true
    })]
    [string]$GoFile,

    [Parameter(HelpMessage="Go arguments as JSON array (e.g., '[`"--port`", `"8080`", `"--verbose`"]')")]
    [string]$GoArgs = "[]",

    [Parameter(HelpMessage="Go version to install (default: latest)")]
    [string]$GoVersion = "latest",

    [Parameter(HelpMessage="Force Go installation even if already present")]
    [switch]$ForceGoInstall,

    [Parameter(HelpMessage="Enable server mode with port management")]
    [switch]$ServerMode,

    [Parameter(HelpMessage="Port for server mode (auto-detected from GoArgs if not specified)")]
    [ValidateRange(1, 65535)]
    [int]$Port = 0,

    [Parameter(HelpMessage="Kill existing processes on the target port")]
    [switch]$StopExisting,

    [Parameter(HelpMessage="Dry run - show what would be executed without running")]
    [switch]$DryRun
)

# Global script variables
$script:VerboseEnabled = $VerbosePreference -ne 'SilentlyContinue'

function Write-Log {
    param(
        [string]$Message,
        [ValidateSet("INFO", "SUCCESS", "WARNING", "ERROR", "DEBUG")]
        [string]$Level = "INFO"
    )

    if (-not $script:VerboseEnabled -and $Level -eq "DEBUG") {
        return
    }

    $colors = @{
        "INFO"    = "White"
        "SUCCESS" = "Green"
        "WARNING" = "Yellow"
        "ERROR"   = "Red"
        "DEBUG"   = "Gray"
    }

    $timestamp = Get-Date -Format "HH:mm:ss"
    $prefix = "[$timestamp] [$Level]"
    Write-Host "$prefix $Message" -ForegroundColor $colors[$Level]
}

function Test-PortAvailable {
    param([int]$PortNumber)

    Write-Log "Testing port availability: $PortNumber" -Level "DEBUG"
    try {
        $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Any, $PortNumber)
        $listener.Start()
        $listener.Stop()
        $listener = $null
        Write-Log "Port $PortNumber is available" -Level "DEBUG"
        return $true
    }
    catch {
        Write-Log "Port $PortNumber is not available: $($_.Exception.Message)" -Level "DEBUG"
        if ($listener) { try { $listener.Stop() } catch { } ; $listener = $null }
        return $false
    }
}

function Find-AvailablePort {
    param([int]$StartPort = 3000)

    Write-Log "Searching for available port starting from: $StartPort" -Level "DEBUG"
    $maxPort = [Math]::Min(65535, $StartPort + 1000)

    for ($p = $StartPort; $p -le $maxPort; $p++) {
        Write-Log "Checking port: $p" -Level "DEBUG"
        if (Test-PortAvailable -PortNumber $p) {
            Write-Log "Found available port: $p" -Level "SUCCESS"
            return $p
        }
        else {
            Write-Log "Port $p busy. attempting to clear it…" -Level "DEBUG"
            Stop-PortProcess -PortNumber $p
            Start-Sleep -Seconds 1
            if (Test-PortAvailable -PortNumber $p) {
                Write-Log "Now free: $p" -Level "SUCCESS"
                return $p
            }
        }
    }

    $errorMsg = "No available ports found in range $StartPort-$maxPort"
    Write-Log $errorMsg -Level "ERROR"
    throw $errorMsg
}

function Stop-PortProcess {
    param([int]$PortNumber)

    Write-Log "Stopping processes on port $PortNumber" -Level "WARNING"

    # try native cmdlet first
    try {
        $conns = Get-NetTCPConnection -LocalPort $PortNumber -State Listen -ErrorAction Stop
    }
    catch {
        Write-Log "Get-NetTCPConnection unavailable. falling back to netstat" -Level "DEBUG"
        $conns = netstat -ano |
            Select-String ":$PortNumber\s+LISTENING" |
            ForEach-Object { [PSCustomObject]@{ OwningProcess = ($_ -split '\s+')[-1] } }
    }

    if (-not $conns) {
        Write-Log "no listeners found on $PortNumber" -Level "DEBUG"
        return
    }

    foreach ($c in $conns) {
        $pid = $c.OwningProcess
        Write-Log "killing pid $pid" -Level "DEBUG"
        try { Stop-Process -Id $pid -Force -ErrorAction SilentlyContinue } catch { }
    }

    # wait for OS TIME_WAIT to clear
    for ($i=0; $i -lt 5; $i++) {
        if (Test-PortAvailable -PortNumber $PortNumber) {
            Write-Log "port $PortNumber is now free" -Level "SUCCESS"
            return
        }
        Start-Sleep -Seconds 1
    }

    Write-Log "still busy: port $PortNumber didn’t free in time" -Level "ERROR"
}


function Get-ProcessedArgs {
    param(
        [array]$Arguments,
        [bool]$IsServerMode,
        [int]$ExplicitPort
    )

    Write-Log "Get-ProcessedArgs called with: Args=$($Arguments -join ','), ServerMode=$IsServerMode, Port=$ExplicitPort" -Level "DEBUG"

    if (-not $IsServerMode) {
        Write-Log "Not in server mode, returning original args" -Level "DEBUG"
        return $Arguments
    }

    # Handle empty args array properly
    if ($null -eq $Arguments -or $Arguments.Count -eq 0) {
        Write-Log "Arguments array is empty or null" -Level "DEBUG"
        $args = @()  # Initialize as empty array
    } else {
        $args = $Arguments.Clone()
    }

    $targetPort = $ExplicitPort
    Write-Log "Initial target port: $targetPort" -Level "DEBUG"

    # If no explicit port, try to find one in args
    if ($targetPort -eq 0 -and $args.Count -gt 0) {
        Write-Log "Searching for port in arguments..." -Level "DEBUG"
        for ($i = 0; $i -lt $args.Length - 1; $i++) {
            Write-Log "Checking arg[$i]: $($args[$i])" -Level "DEBUG"
            if ($args[$i] -in @("-port", "--port", "-p")) {
                try {
                    $targetPort = [int]$args[$i + 1]
                    Write-Log "Found port in args: $targetPort" -Level "DEBUG"
                    break
                }
                catch {
                    Write-Log "Invalid port value in arguments: $($args[$i + 1])" -Level "WARNING"
                }
            }
        }
    }

    if ($targetPort -eq 0) {
        Write-Log "No port specified for server mode, using explicit port if available" -Level "DEBUG"
        if ($ExplicitPort -gt 0) {
            $targetPort = $ExplicitPort
            Write-Log "Using explicit port: $targetPort" -Level "DEBUG"
            # Add port to args since it's not there
            $args += @("-port", $targetPort.ToString())
            Write-Log "Added port to args: $($args -join ', ')" -Level "DEBUG"
        } else {
            Write-Log "No port specified anywhere, returning original args" -Level "DEBUG"
            return $args
        }
    }

    Write-Log "Target port determined: $targetPort" -Level "DEBUG"

    try {
        $portAvailable = Test-PortAvailable -PortNumber $targetPort
        Write-Log "Port $targetPort available: $portAvailable" -Level "DEBUG"

        if (-not $portAvailable) {
            Write-Log "Port $targetPort is not available" -Level "DEBUG"
            if ($StopExisting) {
                Write-Log "Stopping existing processes on port $targetPort" -Level "DEBUG"
                Stop-PortProcess -PortNumber $targetPort
            }
            else {
                Write-Log "Finding alternative port..." -Level "DEBUG"
                $newPort = Find-AvailablePort -StartPort $targetPort
                Write-Log "Replacing port $targetPort with $newPort" -Level "WARNING"

                # Update port in args - handle empty args properly
                $portUpdated = $false
                if ($args.Count -gt 1) {
                    for ($i = 0; $i -lt $args.Length - 1; $i++) {
                        if ($args[$i] -in @("-port", "--port", "-p")) {
                            $args[$i + 1] = $newPort.ToString()
                            $portUpdated = $true
                            Write-Log "Updated existing port arg to: $newPort" -Level "DEBUG"
                            break
                        }
                    }
                }

                # If port wasn't found in args, add it
                if (-not $portUpdated) {
                    $args += @("-port", $newPort.ToString())
                    Write-Log "Added new port arg: $newPort" -Level "DEBUG"
                }
            }
        } else {
            Write-Log "Port $targetPort is available" -Level "DEBUG"
            # Ensure port is in args if it's not already there
            $portInArgs = $false
            if ($args.Count -gt 1) {
                for ($i = 0; $i -lt $args.Length - 1; $i++) {
                    if ($args[$i] -in @("-port", "--port", "-p")) {
                        $portInArgs = $true
                        break
                    }
                }
            }

            if (-not $portInArgs -and $targetPort -gt 0) {
                $args += @("-port", $targetPort.ToString())
                Write-Log "Added port to args since it wasn't present: $targetPort" -Level "DEBUG"
            }
        }
    }
    catch {
        Write-Log "Error in port processing: $($_.Exception.Message)" -Level "ERROR"
        Write-Log "Stack trace: $($_.ScriptStackTrace)" -Level "ERROR"
        throw "Port processing failed: $($_.Exception.Message)"
    }

    Write-Log "Final processed args: $($args -join ', ')" -Level "DEBUG"
    return $args
}

function Install-GoRuntime {
    param(
        [string]$Version,
        [bool]$Force
    )

    $goInstalled = $null -ne (Get-Command go -ErrorAction SilentlyContinue)

    if ($goInstalled -and -not $Force) {
        $currentVersion = (go version 2>$null) -replace "go version go", "" -replace " .*", ""
        Write-Log "Go already installed: $currentVersion" -Level "SUCCESS"
        return
    }

    if ($DryRun) {
        Write-Log "[DRY RUN] Would install Go version: $Version" -Level "INFO"
        return
    }

    Write-Log "Installing Go..." -Level "INFO"

    try {
        $installVersion = $Version

        if ($installVersion -eq "latest") {
            Write-Log "Resolving latest Go version..." -Level "DEBUG"
            $response = Invoke-RestMethod "https://api.github.com/repos/golang/go/tags" -UseBasicParsing
            $latestTag = $response | Where-Object { $_.name -match "^go\d+\.\d+\.\d+$" } | Select-Object -First 1

            if (-not $latestTag) {
                throw "Could not determine latest Go version"
            }

            $installVersion = $latestTag.name -replace "go", ""
        }

        $downloadUrl = "https://go.dev/dl/go$installVersion.windows-amd64.msi"
        $tempFile = Join-Path $env:TEMP "go$installVersion.msi"

        Write-Log "Downloading Go $installVersion..." -Level "INFO"
        Invoke-WebRequest $downloadUrl -OutFile $tempFile -UseBasicParsing

        Write-Log "Installing Go $installVersion..." -Level "INFO"
        $process = Start-Process msiexec -ArgumentList "/i", $tempFile, "/quiet" -Wait -PassThru

        if ($process.ExitCode -ne 0) {
            throw "Go installation failed with exit code: $($process.ExitCode)"
        }

        # Update PATH for current session
        $goPath = "C:\Program Files\Go\bin"
        if ($env:PATH -notlike "*$goPath*") {
            $env:PATH += ";$goPath"
        }

        Remove-Item $tempFile -ErrorAction SilentlyContinue
        Write-Log "Go $installVersion installed successfully" -Level "SUCCESS"
    }
    catch {
        throw "Go installation failed: $($_.Exception.Message)"
    }
}

# Main execution
$ErrorActionPreference = "Stop"

try {
    Write-Log "=== PyGoPS PowerShell Launcher Debug Information ===" -Level "INFO"
    Write-Log "Working Directory: $(Get-Location)" -Level "DEBUG"
    Write-Log "Script Directory: $($PSScriptRoot)" -Level "DEBUG"
    Write-Log "Original GoFile parameter: $GoFile" -Level "DEBUG"
    Write-Log "PowerShell Version: $($PSVersionTable.PSVersion)" -Level "DEBUG"

    # Resolve the Go file path properly for both absolute and relative paths
    $resolvedGoFile = if ([System.IO.Path]::IsPathRooted($GoFile)) {
        # Absolute path - use as-is but verify it exists
        Write-Log "GoFile is absolute path" -Level "DEBUG"
        $GoFile
    } else {
        # Relative path - resolve against current working directory
        Write-Log "GoFile is relative path" -Level "DEBUG"
        $resolved = Resolve-Path $GoFile -ErrorAction SilentlyContinue
        if ($resolved) {
            $resolved.Path
        } else {
            # Try resolving against script directory
            $scriptRelative = Join-Path $PSScriptRoot $GoFile
            Write-Log "Trying script-relative path: $scriptRelative" -Level "DEBUG"
            if (Test-Path $scriptRelative) {
                $scriptRelative
            } else {
                throw "Could not resolve Go file path: $GoFile"
            }
        }
    }

    Write-Log "Resolved GoFile path: $resolvedGoFile" -Level "DEBUG"
    Write-Log "GoFile exists: $(Test-Path $resolvedGoFile)" -Level "DEBUG"

    if (-not (Test-Path $resolvedGoFile)) {
        throw "Go file not found after path resolution: $resolvedGoFile"
    }

    # Parse JSON arguments
    Write-Log "Parsing GoArgs JSON: $GoArgs" -Level "DEBUG"
    try {
        $parsedArgs = ConvertFrom-Json $GoArgs
        if (-not ($parsedArgs -is [array])) {
            throw "GoArgs must be a JSON array"
        }
        Write-Log "Parsed GoArgs: $($parsedArgs -join ', ')" -Level "DEBUG"
    }
    catch {
        Write-Log "JSON parsing failed: $($_.Exception.Message)" -Level "ERROR"
        throw "Invalid JSON in GoArgs parameter: $($_.Exception.Message)"
    }

    Write-Log "ServerMode: $($ServerMode.IsPresent)" -Level "DEBUG"
    Write-Log "Port: $Port" -Level "DEBUG"
    Write-Log "DryRun: $($DryRun.IsPresent)" -Level "DEBUG"

    # Ensure Go is installed
    Write-Log "Checking Go installation..." -Level "DEBUG"
    Install-GoRuntime -Version $GoVersion -Force $ForceGoInstall.IsPresent

    # Process arguments for server mode
    Write-Log "Processing arguments for server mode..." -Level "DEBUG"
    $processedArgs = Get-ProcessedArgs -Arguments $parsedArgs -IsServerMode $ServerMode.IsPresent -ExplicitPort $Port
    Write-Log "Processed args: $($processedArgs -join ', ')" -Level "DEBUG"

    # Build command - use the resolved path
    $command = @("run", $resolvedGoFile) + $processedArgs
    $commandStr = "go " + ($command -join " ")

    Write-Log "Final command array: $($command -join ' | ')" -Level "DEBUG"
    Write-Log "Executing: $commandStr" -Level "INFO"

    if ($DryRun) {
        Write-Log "[DRY RUN] Command would be executed" -Level "INFO"
        Write-Log "[DRY RUN] Working directory would be: $(Get-Location)" -Level "INFO"
        exit 0
    }

    # Execute Go command with additional debugging
    Write-Log "Starting Go execution..." -Level "DEBUG"
    Write-Log "Go executable path: $(Get-Command go -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source)" -Level "DEBUG"

    try {
        & go @command
        $exitCode = $LASTEXITCODE
        Write-Log "Go command completed with exit code: $exitCode" -Level "DEBUG"

        if ($exitCode -eq 0) {
            Write-Log "Execution completed successfully" -Level "SUCCESS"
        }
        else {
            throw "Go execution failed with exit code: $exitCode"
        }
    }
    catch {
        Write-Log "Go execution threw exception: $($_.Exception.Message)" -Level "ERROR"
        throw "Go execution failed: $($_.Exception.Message)"
    }
}
catch {
    Write-Log "=== Error Details ===" -Level "ERROR"
    Write-Log "Error: $($_.Exception.Message)" -Level "ERROR"
    Write-Log "Error Type: $($_.Exception.GetType().Name)" -Level "ERROR"
    Write-Log "Stack Trace: $($_.ScriptStackTrace)" -Level "ERROR"
    exit 1
}
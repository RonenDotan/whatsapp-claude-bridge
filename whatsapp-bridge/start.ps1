$component = if ($args.Count -gt 0) { $args[0] } else { 'all' }

$BRIDGE_DIR = $PSScriptRoot
$MCP_DIR    = Join-Path $PSScriptRoot '..\whatsapp-mcp-server'

if ($component -eq 'help' -or $component -eq '--help') {
    Write-Host 'Usage: start.bat [component]'
    Write-Host ''
    Write-Host '  start.bat             Restart all components'
    Write-Host '  start.bat signal      Restart signal-cli only'
    Write-Host '  start.bat whatsapp    Restart whatsapp-mcp-server only'
    Write-Host '  start.bat bridge      Restart Go bridge only'
    Write-Host '  start.bat --help      Show this help'
    Write-Host ''
    Write-Host 'Components:'
    Write-Host '  signal    signal-cli daemon  (TCP 0.0.0.0:7583)'
    Write-Host '  whatsapp  whatsapp-mcp-server (python main.py)'
    Write-Host '  bridge    whatsapp-bridge.exe'
    exit 0
}

# Discover signal-cli - find the newest *-extracted folder in TEMP
$signalBase = Get-ChildItem $env:TEMP -Directory |
    Where-Object { $_.Name -like 'signal-cli-*-extracted' } |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1

if (-not $signalBase) {
    Write-Error ('Cannot find signal-cli-*-extracted in ' + $env:TEMP)
    exit 1
}

$signalCliBat = Get-ChildItem $signalBase.FullName -Recurse -Filter 'signal-cli.bat' |
    Select-Object -First 1 -ExpandProperty FullName

if (-not $signalCliBat) {
    Write-Error ('Cannot find signal-cli.bat under ' + $signalBase.FullName)
    exit 1
}

function Kill-ByCommandLine([string]$pattern) {
    Get-CimInstance Win32_Process |
        Where-Object { $_.CommandLine -like ('*' + $pattern + '*') } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}

function Restart-SignalCli {
    Write-Host 'Stopping signal-cli...'
    Kill-ByCommandLine 'signal-cli'
    Start-Sleep -Milliseconds 800
    $p = Start-Process -FilePath $signalCliBat `
        -ArgumentList 'daemon --tcp 0.0.0.0:7583' `
        -WindowStyle Hidden -PassThru
    Write-Host ('[OK] signal-cli started (PID ' + $p.Id + ')')
}

function Restart-WhatsAppMcp {
    Write-Host 'Stopping whatsapp-mcp-server...'
    Get-CimInstance Win32_Process |
        Where-Object { $_.Name -like 'python*' -and $_.CommandLine -like '*main.py*' } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Milliseconds 800
    $pythonExe = $MCP_DIR + '\.venv\Scripts\python.exe'
    $p = Start-Process -FilePath $pythonExe `
        -ArgumentList 'main.py' `
        -WorkingDirectory $MCP_DIR -WindowStyle Hidden -PassThru
    Write-Host ('[OK] whatsapp-mcp-server started (PID ' + $p.Id + ')')
}

function Restart-Bridge {
    Write-Host 'Stopping whatsapp-bridge...'
    # Kill-ByCommandLine catches both direct launches and cmd.exe log-wrapper processes
    Kill-ByCommandLine 'whatsapp-bridge.exe'
    Start-Sleep -Milliseconds 500

    # Check for whatsmeow updates before launching
    try {
        $latest  = (Invoke-RestMethod 'https://proxy.golang.org/go.mau.fi/whatsmeow/@latest').Version
        $current = (Select-String -Path "$BRIDGE_DIR\go.mod" -Pattern 'go.mau.fi/whatsmeow').Line.Split(' ')[1]
        if ($latest -ne $current) {
            Write-Host "[UPDATE] whatsmeow $current -> $latest, rebuilding..."
            Push-Location $BRIDGE_DIR
            & go get go.mau.fi/whatsmeow@latest
            & go build -o whatsapp-bridge.exe .
            Pop-Location
        } else {
            Write-Host "[OK] whatsmeow $current is current"
        }
    } catch {
        Write-Warning "[WARN] whatsmeow update check failed: $_. Continuing with existing binary."
    }
    # Start-Process -RedirectStandard* overwrites; use cmd.exe >> for append mode
    $logFile = $BRIDGE_DIR + '\bridge.log'
    $errFile = $BRIDGE_DIR + '\bridge.err'
    $p = Start-Process -FilePath cmd.exe `
        -ArgumentList ('/c ""' + $BRIDGE_DIR + '\whatsapp-bridge.exe" >> "' + $logFile + '" 2>> "' + $errFile + '""') `
        -WorkingDirectory $BRIDGE_DIR -WindowStyle Hidden -PassThru
    Write-Host ('[OK] bridge started (PID ' + $p.Id + ')')
}

switch ($component.ToLower()) {
    'signal'   { Restart-SignalCli }
    'whatsapp' { Restart-WhatsAppMcp }
    'bridge'   { Restart-Bridge }
    'all'      { Restart-SignalCli; Restart-WhatsAppMcp; Restart-Bridge }
    default {
        Write-Host ('Unknown component: ' + $component)
        Write-Host 'Run: start.bat --help'
        exit 1
    }
}

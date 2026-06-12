param(
    [string]$Channels = '',
    [string]$LLM      = '',
    [switch]$Help
)

$BRIDGE_DIR = $PSScriptRoot
$MCP_DIR    = Join-Path $PSScriptRoot '..\whatsapp-mcp-server'

# ─── Data directory ───────────────────────────────────────────────────────────
# User state (sessions, allowed chats, per-chat data) lives here — outside the repo.
# Default: bridge-data/ sibling to the bridge directory.
# Override: set WHATSAPP_BRIDGE_DATA_DIR before running install or start.
if (-not $env:WHATSAPP_BRIDGE_DATA_DIR) {
    $env:WHATSAPP_BRIDGE_DATA_DIR = Join-Path (Split-Path $BRIDGE_DIR -Parent) 'bridge-data'
    [System.Environment]::SetEnvironmentVariable('WHATSAPP_BRIDGE_DATA_DIR', $env:WHATSAPP_BRIDGE_DATA_DIR, 'User')
    Write-Host "[OK]  WHATSAPP_BRIDGE_DATA_DIR set to: $env:WHATSAPP_BRIDGE_DATA_DIR"
}
$DATA_DIR = $env:WHATSAPP_BRIDGE_DATA_DIR
if (-not (Test-Path $DATA_DIR)) { New-Item -ItemType Directory -Path $DATA_DIR -Force | Out-Null }

$STORE_DIR     = Join-Path $BRIDGE_DIR 'store'
$SETTINGS_FILE = Join-Path $DATA_DIR   'settings.json'

function Get-BridgeSettings {
    if (Test-Path $SETTINGS_FILE) {
        try { return (Get-Content $SETTINGS_FILE -Raw | ConvertFrom-Json) } catch {}
    }
    return [PSCustomObject]@{}
}

function Set-BridgeSetting([string]$Key, $Value) {
    $s = Get-BridgeSettings
    $s | Add-Member -NotePropertyName $Key -NotePropertyValue $Value -Force
    if (-not (Test-Path $DATA_DIR)) { New-Item -ItemType Directory -Path $DATA_DIR -Force | Out-Null }
    $s | ConvertTo-Json -Depth 5 | Set-Content $SETTINGS_FILE -Encoding UTF8
}

function Find-SignalCli {
    $base = Get-ChildItem $env:TEMP -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -like 'signal-cli-*-extracted' } |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if ($base) {
        $bat = Get-ChildItem $base.FullName -Recurse -Filter 'signal-cli.bat' -ErrorAction SilentlyContinue |
            Select-Object -First 1 -ExpandProperty FullName
        if ($bat) { return $bat }
    }
    try { return (Get-Command signal-cli -ErrorAction Stop).Source } catch {}
    return $null
}

function Get-SignalAccountsJsonPath {
    return Join-Path $env:APPDATA 'signal-cli\data\accounts.json'
}

function Test-SignalLinked {
    $path = Get-SignalAccountsJsonPath
    if (-not (Test-Path $path)) { return $false }
    try {
        $content = Get-Content $path -Raw | ConvertFrom-Json
        foreach ($acct in $content.accounts) {
            if ($acct.number -match '^\+') { return $true }
        }
    } catch {}
    return $false
}

function Show-Usage {
    Write-Host 'Usage: install.ps1 -Channels <whatsapp|signal|both> -LLM <claude|codex|both>'
    Write-Host ''
    Write-Host 'Parameters:'
    Write-Host '  -Channels   Channels to install: whatsapp, signal, or both'
    Write-Host '  -LLM        LLM backend to use:  claude, codex, or both'
    Write-Host '  -Help       Show this help'
    Write-Host ''
    Write-Host 'Examples:'
    Write-Host '  install.ps1 -Channels whatsapp -LLM claude'
    Write-Host '  install.ps1 -Channels signal   -LLM codex'
    Write-Host '  install.ps1 -Channels both     -LLM both'
}

if ($Help) {
    Show-Usage
    exit 0
}

$Channels = $Channels.ToLower()
$LLM      = $LLM.ToLower()

if (-not $Channels) {
    do {
        Write-Host 'Which channels do you want to configure?'
        Write-Host '  [1] WhatsApp only'
        Write-Host '  [2] Signal only'
        Write-Host '  [3] Both WhatsApp and Signal'
        $choice = Read-Host 'Enter choice (1/2/3)'
        switch ($choice) {
            '1' { $Channels = 'whatsapp' }
            '2' { $Channels = 'signal' }
            '3' { $Channels = 'both' }
            default { Write-Host '[WARN] Invalid choice. Enter 1, 2, or 3.' }
        }
    } while (-not $Channels)
    Write-Host ''
}

if (-not $LLM) {
    do {
        Write-Host 'Which LLM backend do you want to use?'
        Write-Host '  [1] Claude'
        Write-Host '  [2] Codex (OpenAI)'
        Write-Host '  [3] Both'
        $choice = Read-Host 'Enter choice (1/2/3)'
        switch ($choice) {
            '1' { $LLM = 'claude' }
            '2' { $LLM = 'codex' }
            '3' { $LLM = 'both' }
            default { Write-Host '[WARN] Invalid choice. Enter 1, 2, or 3.' }
        }
    } while (-not $LLM)
    Write-Host ''
}

if ($Channels -notin @('whatsapp', 'signal', 'both')) {
    Write-Host "Error: -Channels must be whatsapp, signal, or both (got: $Channels)"
    exit 1
}
if ($LLM -notin @('claude', 'codex', 'both')) {
    Write-Host "Error: -LLM must be claude, codex, or both (got: $LLM)"
    exit 1
}

Write-Host '================================================'
Write-Host '  WhatsApp/Signal AI Bridge - Installation Wizard'
Write-Host '================================================'
Write-Host ''
Write-Host "  Installing with: Channels=$Channels, LLM=$LLM"
Write-Host ''
Write-Host 'Step 1: Checking prerequisites...'
Write-Host ''

$missing = @()

function Test-VersionAtLeast([string]$actual, [string]$minimum) {
    try {
        return ([Version]$actual) -ge ([Version]$minimum)
    } catch {
        return $false
    }
}

# --- Go (always required) ---
try {
    $goOut = (& go version 2>&1) -join ' '
    if ($goOut -match 'go(\d+\.\d+\.?\d*)') {
        $goVer = $matches[1]
        if (Test-VersionAtLeast $goVer '1.21') {
            Write-Host "[OK]      Go $goVer"
        } else {
            Write-Host "[MISSING] Go 1.21+ required (found $goVer) -- download from https://go.dev/dl/"
            $missing += 'Go 1.21+'
        }
    } else {
        Write-Host '[MISSING] Go 1.21+ required -- download from https://go.dev/dl/'
        $missing += 'Go 1.21+'
    }
} catch {
    Write-Host '[MISSING] Go 1.21+ required -- download from https://go.dev/dl/'
    $missing += 'Go 1.21+'
}

# --- Java (required if signal) ---
if ($Channels -in @('signal', 'both')) {
    try {
        # java -version writes to stderr; capture by joining ErrorRecord strings
        $javaOut = (& java -version 2>&1) | ForEach-Object { "$_" }
        $javaStr = $javaOut -join ' '
        if ($javaStr -match '"(\d+)') {
            $javaMajor = [int]$matches[1]
            if ($javaMajor -ge 21) {
                Write-Host "[OK]      Java $javaMajor"
            } else {
                Write-Host "[MISSING] Java 21+ required (found $javaMajor) -- download from https://adoptium.net/"
                $missing += 'Java 21+'
            }
        } else {
            Write-Host '[MISSING] Java 21+ required -- download from https://adoptium.net/'
            $missing += 'Java 21+'
        }
    } catch {
        Write-Host '[MISSING] Java 21+ required -- download from https://adoptium.net/'
        $missing += 'Java 21+'
    }
}

# --- Python (required if whatsapp) ---
if ($Channels -in @('whatsapp', 'both')) {
    $pyFound = $false
    foreach ($pyCmd in @('python', 'python3')) {
        try {
            $pyOut = (& $pyCmd --version 2>&1) -join ' '
            if ($pyOut -match 'Python (\d+\.\d+\.?\d*)') {
                $pyVer = $matches[1]
                if (Test-VersionAtLeast $pyVer '3.8') {
                    Write-Host "[OK]      Python $pyVer ($pyCmd)"
                    $pyFound = $true
                    break
                }
            }
        } catch {}
    }
    if (-not $pyFound) {
        Write-Host '[MISSING] Python 3.8+ required -- download from https://www.python.org/downloads/'
        $missing += 'Python 3.8+'
    }
}

# --- Node.js (required if LLM includes claude) ---
if ($LLM -in @('claude', 'both')) {
    try {
        $nodeOut = (& node --version 2>&1) -join ' '
        if ($nodeOut -match 'v(\d+\.\d+\.?\d*)') {
            $nodeVer = $matches[1]
            Write-Host "[OK]      Node.js $nodeVer"
        } else {
            Write-Host '[MISSING] Node.js required -- download from https://nodejs.org/'
            $missing += 'Node.js'
        }
    } catch {
        Write-Host '[MISSING] Node.js required -- download from https://nodejs.org/'
        $missing += 'Node.js'
    }
}

# --- signal-cli (required if signal) ---
if ($Channels -in @('signal', 'both')) {
    $signalPath = Find-SignalCli
    if ($signalPath) {
        Write-Host "[OK]      signal-cli (found at $signalPath)"
    } else {
        Write-Host '[MISSING] signal-cli required:'
        Write-Host '          1. Download from https://github.com/AsamK/signal-cli/releases'
        Write-Host "          2. Extract to $env:TEMP\signal-cli-<version>-extracted\"
        $missing += 'signal-cli'
    }
}

# --- claude CLI (required if LLM=claude or both) ---
if ($LLM -in @('claude', 'both')) {
    try {
        $claudeOut = (& claude --version 2>&1) | ForEach-Object { "$_" }
        $claudeStr = ($claudeOut -join ' ').Trim()
        Write-Host "[OK]      claude CLI ($claudeStr)"
    } catch {
        Write-Host '[MISSING] claude CLI required -- install from https://claude.ai/download'
        $missing += 'claude CLI'
    }
}

# --- codex CLI (required if LLM=codex or both) ---
if ($LLM -in @('codex', 'both')) {
    try {
        $codexOut = (& codex --version 2>&1) | ForEach-Object { "$_" }
        $codexStr = ($codexOut -join ' ').Trim()
        Write-Host "[OK]      codex CLI ($codexStr)"
    } catch {
        Write-Host '[MISSING] codex CLI required -- install via: npm install -g @openai/codex'
        $missing += 'codex CLI'
    }
}

Write-Host ''

if ($missing.Count -gt 0) {
    Write-Host 'Prerequisites missing:'
    foreach ($item in $missing) {
        Write-Host "  - $item"
    }
    Write-Host ''
    Write-Host 'Install the items above and re-run install.ps1.'
    exit 1
}

Write-Host '[OK] All prerequisites satisfied.'
Write-Host ''

# ---------------------------------------------------------------------------
# Step 2: WhatsApp pairing
# ---------------------------------------------------------------------------

function Invoke-WhatsAppPairing {
    $dbPath = Join-Path $BRIDGE_DIR 'store\whatsapp.db'
    if ((Test-Path $dbPath) -and (Get-Item $dbPath).Length -gt 0) {
        Write-Host '[OK] WhatsApp already paired -- skipping'
        return 0
    }

    Write-Host '[STEP] Starting WhatsApp pairing...'

    $logFile = Join-Path $BRIDGE_DIR 'whatsapp-mcp.log'
    $errFile = Join-Path $BRIDGE_DIR 'whatsapp-mcp.err'

    # Clear stale logs so we only read fresh output
    if (Test-Path $logFile) { Remove-Item $logFile -Force }
    if (Test-Path $errFile) { Remove-Item $errFile -Force }

    $pyExe = Join-Path $MCP_DIR '.venv\Scripts\python.exe'
    if (-not (Test-Path $pyExe)) { $pyExe = 'python' }

    $proc = $null
    try {
        $proc = Start-Process -FilePath $pyExe `
            -ArgumentList 'main.py' `
            -WorkingDirectory $MCP_DIR `
            -RedirectStandardOutput $logFile `
            -RedirectStandardError  $errFile `
            -PassThru

        # Wait up to 15 s for QR code output
        $qrDetected = $false
        $deadline = (Get-Date).AddSeconds(15)
        while ((Get-Date) -lt $deadline) {
            Start-Sleep -Seconds 1
            if (Test-Path $logFile) {
                $content = Get-Content $logFile -Raw -ErrorAction SilentlyContinue
                if ($content -match '[█▀▄]' -or $content -match '(?i)(QR|scan)') {
                    $qrDetected = $true
                    break
                }
            }
        }

        if (-not $qrDetected) {
            Write-Host '[ERROR] QR code not detected within 15 seconds.'
            Write-Host "        Check $errFile for details."
            if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
            return 1
        }

        # Display the QR code
        Get-Content $logFile
        Write-Host ''
        Write-Host 'Scan the QR code above with your WhatsApp app:'
        Write-Host '   WhatsApp > Linked Devices > Link a Device'
        Write-Host ''

        # Wait up to 3 minutes for pairing to complete
        $deadline = (Get-Date).AddSeconds(180)
        while ((Get-Date) -lt $deadline) {
            Start-Sleep -Seconds 2

            # Check log for success indicators
            $content = Get-Content $logFile -Raw -ErrorAction SilentlyContinue
            if ($content -match '(?i)(Successfully authenticated|Connected|logged in)') {
                Write-Host '[OK] WhatsApp paired successfully!'
                return 0
            }

            # Check for DB file appearing as an alternative success signal
            if ((Test-Path $dbPath) -and (Get-Item $dbPath).Length -gt 0) {
                Write-Host '[OK] WhatsApp paired successfully!'
                return 0
            }
        }

        Write-Host '[ERROR] WhatsApp pairing timed out. Check whatsapp-mcp.log for details.'
        if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
        return 1
    } catch {
        Write-Host "[ERROR] Unexpected error during pairing: $_"
        if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
        return 1
    }
}

function Stop-SignalCli([System.Diagnostics.Process]$proc) {
    if ($proc -and -not $proc.HasExited) {
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    }
    Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like '*signal-cli*' } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}

function Invoke-SignalLinking {
    if (Test-SignalLinked) {
        Write-Host '[OK] Signal already linked -- skipping'
        return 0
    }

    $signalBat = Find-SignalCli
    if (-not $signalBat) {
        Write-Host '[ERROR] signal-cli not found. Check prerequisites.'
        return 1
    }

    Write-Host '[STEP] Starting Signal linking...'
    Write-Host ''
    Write-Host '  A new window will open showing a QR code.'
    Write-Host '  This may take up to a minute — the QR code will appear in the new window.'
    Write-Host '  On your phone: Signal > Settings > Linked Devices > Link New Device'
    Write-Host '  Scan the QR code in the new window.'
    Write-Host ''
    Write-Host '  Waiting for you to scan (up to 5 minutes)...'
    Write-Host ''

    # Use outer "" to avoid cmd.exe quote-stripping (same pattern as Restart-Bridge in start.ps1)
    $proc = Start-Process -FilePath 'cmd.exe' `
        -ArgumentList ('/c ""' + $signalBat + '" link --name ""AI-Bridge"""') `
        -WindowStyle Normal -PassThru

    if (-not $proc) {
        Write-Host '[ERROR] Failed to start signal-cli.'
        return 1
    }

    $deadline  = (Get-Date).AddSeconds(300)
    $startTime = Get-Date
    $linked    = $false
    while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 2
        if (Test-SignalLinked) { $linked = $true; break }
        if ($proc.HasExited -and $proc.ExitCode -ne 0) {
            Write-Host ('[ERROR] signal-cli exited with code ' + $proc.ExitCode)
            Stop-SignalCli $proc
            return 1
        }
        $elapsed = [int]((Get-Date) - $startTime).TotalSeconds
        if ($elapsed % 30 -eq 0 -and $elapsed -gt 0) {
            Write-Host ("  Still waiting... ($elapsed" + 's elapsed, up to 300s)')
        }
    }

    Stop-SignalCli $proc

    if ($linked) {
        Write-Host '[OK] Signal linked successfully!'
        return 0
    } else {
        Write-Host '[ERROR] Signal linking timed out after 5 minutes.'
        Write-Host '        Re-run install.ps1 to try again.'
        return 1
    }
}

function Invoke-VerifyAndSummary {
    Write-Host 'Step 4: Launch and verify...'
    Write-Host ''
    Write-Host '  Everything is configured.'
    Write-Host '  Press Enter to launch the bridge via start.bat and confirm it is running.'
    Write-Host ''
    Read-Host '  Press Enter to launch' | Out-Null

    $startBat = Join-Path $BRIDGE_DIR 'start.bat'
    if (-not (Test-Path $startBat)) {
        Write-Host '[WARN] start.bat not found -- launch the bridge manually.'
        return
    }

    Write-Host '[STEP] Running start.bat...'
    Start-Process -FilePath 'cmd.exe' `
        -ArgumentList ('/c ""' + $startBat + '""') `
        -WindowStyle Normal

    Write-Host '       Waiting for bridge on port 8080 (up to 60 s -- may rebuild first)...'
    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        Start-Sleep -Seconds 2
        try {
            $tcp = New-Object System.Net.Sockets.TcpClient
            $tcp.Connect('localhost', 8080)
            $tcp.Close()
            $ready = $true
            break
        } catch {}
    }

    Write-Host ''
    if ($ready) {
        Write-Host -ForegroundColor Green '[OK] Bridge is running on port 8080.'
        Write-Host ''
        Write-Host -ForegroundColor Green '================================================================'
        Write-Host -ForegroundColor Green '  Installation complete!'
        Write-Host ''
        Write-Host '  Start a Claude session : send  !meet-claude  in any chat'
        Write-Host '  Start a Codex session  : send  !meet-codex   in any chat'
        Write-Host '  End a session          : send  !clear-session'
        Write-Host -ForegroundColor Green '================================================================'
    } else {
        Write-Host -ForegroundColor Yellow '[WARN] Bridge did not respond within 60 seconds.'
        Write-Host -ForegroundColor Yellow '       It may still be rebuilding (whatsmeow auto-update can take a minute).'
        Write-Host -ForegroundColor Yellow '       Check bridge.log once it finishes -- installation files are correct.'
    }
}

function Invoke-LLMConfiguration {
    if ($LLM -in @('claude', 'both')) {
        Write-Host '[STEP] Configuring Claude...'
        try {
            $out = (& claude --version 2>&1) -join ' '
            Write-Host "[OK]  claude CLI: $out"
        } catch {
            Write-Host "[WARN] Cannot run claude CLI: $_"
            Write-Host '       Ensure claude is installed and on PATH.'
        }
        Set-BridgeSetting 'claude_enabled' $true
    }

    if ($LLM -in @('codex', 'both')) {
        Write-Host '[STEP] Configuring Codex...'
        $apiKey = $env:OPENAI_API_KEY
        if (-not $apiKey) {
            $saved = Get-BridgeSettings
            if ($saved.PSObject.Properties['openai_api_key']) { $apiKey = $saved.openai_api_key }
        }
        if ($apiKey) {
            Write-Host ("[OK]  OpenAI API key already set (" + $apiKey.Substring(0, [Math]::Min(7, $apiKey.Length)) + "...)")
        } else {
            do {
                $apiKey = Read-Host 'Enter your OpenAI API key (starts with sk-)'
                if (-not $apiKey -or -not $apiKey.StartsWith('sk-')) { Write-Host '[WARN] Key must start with "sk-". Try again.' }
            } while (-not $apiKey -or -not $apiKey.StartsWith('sk-'))
            SETX OPENAI_API_KEY $apiKey | Out-Null
            $env:OPENAI_API_KEY = $apiKey
            Write-Host '[OK]  OPENAI_API_KEY saved to user environment'
            Set-BridgeSetting 'openai_api_key' $apiKey
        }
        Set-BridgeSetting 'codex_enabled' $true
    }

    $default = if ($LLM -eq 'both') { 'claude' } else { $LLM }
    Set-BridgeSetting 'default_llm' $default
    Write-Host "[OK]  Default LLM set to: $default"
    return 0
}

# ---------------------------------------------------------------------------
# Main flow: run pairing steps based on selected channels
# ---------------------------------------------------------------------------

Write-Host 'Step 2: Pairing channels...'
Write-Host ''

$pairingOk = $true

if ($Channels -in @('whatsapp', 'both')) {
    $result = Invoke-WhatsAppPairing
    if ($result -ne 0) { $pairingOk = $false }
}

if ($Channels -in @('signal', 'both')) {
    $result = Invoke-SignalLinking
    if ($result -ne 0) { $pairingOk = $false }
}

Write-Host ''
Write-Host 'Step 3: Configuring LLM backend...'
Write-Host ''
$result = Invoke-LLMConfiguration
if ($result -ne 0) { $pairingOk = $false }
Write-Host ''

if (-not $pairingOk) {
    Write-Host '[ERROR] Installation did not complete. Fix the errors above and re-run install.ps1.'
    exit 1
}

Write-Host '[OK] LLM configuration complete.'
Write-Host ''
Invoke-VerifyAndSummary

$component = if ($args.Count -gt 0) { $args[0] } else { 'all' }

$BRIDGE_DIR = $PSScriptRoot
$MCP_DIR    = Join-Path $PSScriptRoot '..\whatsapp-mcp-server'

# ─── Signal-CLI update policy (issue #41) ────────────────────────────────────
# $false (default): locked to $SignalCliPinnedVersion. No GitHub check on start.
#   Fresh installs download the pinned version and auto-apply the NPE patch.
#   Change to $true only when you intentionally want to upgrade signal-cli.
# $true:  check GitHub for a newer release, download if found, then auto-patch.
#   After a successful upgrade, flip back to $false to lock the new version.
$SignalCliAutoUpdate    = $false
$SignalCliPinnedVersion = '0.14.4.1'
# ─────────────────────────────────────────────────────────────────────────────

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

function Rotate-LogFile([string]$path, [int]$keepCount = 5) {
    if (-not (Test-Path $path)) { return }
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $rotated = $path + '.' + $stamp
    Rename-Item -Path $path -NewName $rotated -Force
    # Keep only the most recent $keepCount rotated files for this base path
    $old = Get-ChildItem -Path (Split-Path $path) -Filter ((Split-Path $path -Leaf) + '.*') |
        Sort-Object LastWriteTime -Descending |
        Select-Object -Skip $keepCount
    foreach ($f in $old) { Remove-Item $f.FullName -Force }
}

function Kill-ByCommandLine([string]$pattern) {
    Get-CimInstance Win32_Process |
        Where-Object { $_.CommandLine -like ('*' + $pattern + '*') } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}

# Returns $true if the system Java major version is >= $minMajor
function Test-JavaVersion([int]$minMajor) {
    try {
        $out = (& java --version 2>&1)[0]   # "openjdk 21.0.11 ..."
        if ($out -match '(\d+)') {
            return ([int]$Matches[1] -ge $minMajor)
        }
    } catch {}
    return $false
}

# Download/verify signal-cli in %TEMP%. Behaviour controlled by $SignalCliAutoUpdate:
#   $false → always use $SignalCliPinnedVersion (no GitHub check)
#   $true  → check GitHub for latest, download if newer than pinned
# After any fresh download, Apply-SignalCliPatch is called by Restart-SignalCli.
# Requires Java 25+.
function Update-SignalCli {
    try {
        if ($SignalCliAutoUpdate) {
            $release   = Invoke-RestMethod 'https://api.github.com/repos/AsamK/signal-cli/releases/latest'
            $targetVer = $release.tag_name.TrimStart('v')
            Write-Host "[UPDATE] signal-cli auto-update enabled; latest is $targetVer"
        } else {
            $targetVer = $SignalCliPinnedVersion
            $release   = Invoke-RestMethod "https://api.github.com/repos/AsamK/signal-cli/releases/tags/v$targetVer"
        }

        $installDir = Join-Path $env:TEMP "signal-cli-$targetVer-extracted"

        if (Test-Path $installDir) {
            Write-Host "[OK] signal-cli $targetVer already installed"
            return
        }

        # signal-cli >= 0.14.0 requires Java 25+
        if (-not (Test-JavaVersion 25)) {
            $javaInfo = try { (& java --version 2>&1)[0] } catch { 'not found' }
            Write-Warning "[WARN] signal-cli $targetVer requires Java 25+; found: $javaInfo. Keeping current version."
            return
        }

        $assetName = "signal-cli-$targetVer.tar.gz"
        $asset = $release.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
        if (-not $asset) {
            Write-Warning "[WARN] Asset $assetName not found in release, skipping"
            return
        }

        Write-Host "[UPDATE] Downloading signal-cli $targetVer ($assetName)..."
        $tmpFile = Join-Path $env:TEMP $assetName
        Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $tmpFile

        New-Item -ItemType Directory -Path $installDir | Out-Null
        tar -xzf $tmpFile -C $installDir --strip-components=1
        Remove-Item $tmpFile -Force

        # Keep only the two most recent extracted versions
        Get-ChildItem $env:TEMP -Directory |
            Where-Object { $_.Name -like 'signal-cli-*-extracted' -and $_.FullName -ne $installDir } |
            Sort-Object LastWriteTime -Descending |
            Select-Object -Skip 1 |
            ForEach-Object {
                Write-Host "[CLEANUP] Removing old $($_.Name)"
                Remove-Item $_.FullName -Recurse -Force -ErrorAction SilentlyContinue
            }

        Write-Host "[OK] signal-cli $targetVer installed"
    } catch {
        Write-Warning "[WARN] signal-cli update check failed: $_. Continuing with existing version."
    }
}

# Apply the getServerGuid NPE bytecode patch (issue #41) to the active signal-cli install.
# Runs _patch_signal_jar.py; script is a no-op if already patched or bug fixed upstream.
function Apply-SignalCliPatch {
    $patchScript = Join-Path $BRIDGE_DIR '_patch_signal_jar.py'
    if (-not (Test-Path $patchScript)) {
        Write-Warning '[WARN] _patch_signal_jar.py not found — skipping signal-cli patch'
        return
    }
    $installDir = Get-ChildItem $env:TEMP -Directory |
        Where-Object { $_.Name -like 'signal-cli-*-extracted' } |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if (-not $installDir) {
        Write-Warning '[WARN] No signal-cli install found — cannot apply patch'
        return
    }
    $jar = Get-ChildItem (Join-Path $installDir.FullName 'lib') -Filter 'signal-service-java-*.jar' |
        Select-Object -First 1
    if (-not $jar) {
        Write-Warning '[WARN] signal-service-java jar not found — cannot apply patch'
        return
    }
    try {
        $result = & python $patchScript $jar.FullName 2>&1
        Write-Host $result
    } catch {
        Write-Warning "[WARN] signal-cli patch script failed: $_"
    }
}

# Discover signal-cli - find the newest *-extracted folder in TEMP
function Get-SignalCliBat {
    $base = Get-ChildItem $env:TEMP -Directory |
        Where-Object { $_.Name -like 'signal-cli-*-extracted' } |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if (-not $base) { return $null }
    return Get-ChildItem $base.FullName -Recurse -Filter 'signal-cli.bat' |
        Select-Object -First 1 -ExpandProperty FullName
}

function Restart-SignalCli {
    Update-SignalCli
    Apply-SignalCliPatch

    $signalCliBat = Get-SignalCliBat
    if (-not $signalCliBat) {
        Write-Error 'Cannot find signal-cli.bat. Install signal-cli or ensure Java 25+ is available.'
        exit 1
    }

    # Ensure signal-cli uses Java 25+ (JAVA_HOME may still point to an older JDK)
    $java25Home = 'C:\Program Files\Eclipse Adoptium\jdk-25.0.3.9-hotspot'
    if (Test-Path $java25Home) {
        $env:JAVA_HOME = $java25Home
    }

    Write-Host 'Stopping signal-cli...'
    Kill-ByCommandLine 'signal-cli'
    Rotate-LogFile ($BRIDGE_DIR + '\signal-cli.log')
    Start-Sleep -Milliseconds 800
    $sigLog = $BRIDGE_DIR + '\signal-cli.log'
    $p = Start-Process -FilePath $signalCliBat `
        -ArgumentList 'daemon --tcp 0.0.0.0:7583' `
        -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput $sigLog `
        -RedirectStandardError  ($BRIDGE_DIR + '\signal-cli.err')
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
    Rotate-LogFile ($BRIDGE_DIR + '\bridge.log')
    Rotate-LogFile ($BRIDGE_DIR + '\bridge.err')

    # Check for whatsmeow updates before launching
    try {
        $latest  = (Invoke-RestMethod 'https://proxy.golang.org/go.mau.fi/whatsmeow/@latest').Version
        $current = (Select-String -Path "$BRIDGE_DIR\go.mod" -Pattern 'go.mau.fi/whatsmeow').Line.Split(' ')[1]
        if ($latest -ne $current) {
            Write-Host "[UPDATE] whatsmeow $current -> $latest, rebuilding..."
            Push-Location $BRIDGE_DIR
            & go get go.mau.fi/whatsmeow@latest
            $ver = (Get-Content "$BRIDGE_DIR\VERSION" -Raw).Trim()
            & go build -ldflags "-X main.Version=$ver" -o whatsapp-bridge.exe .
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

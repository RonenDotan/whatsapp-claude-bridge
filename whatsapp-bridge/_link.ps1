$env:JAVA_HOME = 'C:\Program Files\Eclipse Adoptium\jdk-25.0.3.9-hotspot'
$env:PATH = "$env:JAVA_HOME\bin;" + $env:PATH

# Kill signal-cli daemon
Write-Output "Killing signal-cli..."
Get-Process java -ErrorAction SilentlyContinue | ForEach-Object {
    try { $_.Kill(); Write-Output "Killed PID $($_.Id)" } catch {}
}
Start-Sleep -Seconds 3
Write-Output "Killed."

# Delete old (SPQR-invalidated) account data so link can create fresh data
$dataDir = "$env:USERPROFILE\.local\share\signal-cli\data"
Write-Output "Clearing old account data in: $dataDir"
if (Test-Path $dataDir) {
    Remove-Item "$dataDir\*" -Recurse -Force -ErrorAction SilentlyContinue
    Write-Output "Cleared."
} else {
    Write-Output "Data dir not found, skipping."
}

# Find signal-cli.bat (newest extracted dir)
$extracted = Get-ChildItem 'C:\Users\ronen\AppData\Local\Temp' -Filter 'signal-cli-*-extracted' |
    Sort-Object LastWriteTime -Descending | Select-Object -First 1
if (-not $extracted) { Write-Output "ERROR: no signal-cli-*-extracted dir found"; exit 1 }
$bat = Join-Path $extracted.FullName 'bin\signal-cli.bat'
Write-Output "Using: $bat"

# Clear previous link output files
$outFile = 'C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_link_out.txt'
$errFile = 'C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_link_err.txt'
Set-Content $outFile ''
Set-Content $errFile ''

# Start signal-cli link in background — it prints the sgnl:// URL and waits for QR scan
$p = Start-Process -FilePath $bat -ArgumentList 'link --name bridge' `
    -RedirectStandardOutput $outFile -RedirectStandardError $errFile `
    -PassThru -WindowStyle Hidden
Write-Output "Started link process PID: $($p.Id)"

# Poll stdout + stderr for sgnl:// URL (up to 30 seconds)
$url = $null
for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    foreach ($f in $outFile, $errFile) {
        $lines = Get-Content $f -ErrorAction SilentlyContinue
        foreach ($line in $lines) {
            if ($line -match 'sgnl://') { $url = $line.Trim(); break }
        }
        if ($url) { break }
    }
    if ($url) { break }
}

if ($url) {
    Write-Output "URL_FOUND"
    Write-Output $url
    Write-Output "PID: $($p.Id)"
} else {
    Write-Output "ERROR: URL not found within 30s"
    Write-Output "=== stdout ==="
    Get-Content $outFile -ErrorAction SilentlyContinue
    Write-Output "=== stderr ==="
    Get-Content $errFile -ErrorAction SilentlyContinue | Select-Object -Last 15
    if (-not $p.HasExited) { $p.Kill() }
}

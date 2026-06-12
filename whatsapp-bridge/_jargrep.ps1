Add-Type -AssemblyName System.IO.Compression.FileSystem

$extracted = Get-ChildItem $env:TEMP 'signal-cli-*-extracted' | Sort-Object LastWriteTime -Descending | Select-Object -First 1
$libDir = Join-Path $extracted.FullName 'lib'

# Target jars most likely to contain getServerGuid
$targetJars = @(
    'signal-service-java-2.15.3_unofficial_147.jar',
    'signal-cli-0.14.4.1.jar',
    'signal-network-2.15.3_unofficial_147.jar',
    'core-network-2.15.3_unofficial_147.jar'
)

foreach ($jarName in $targetJars) {
    $jarPath = Join-Path $libDir $jarName
    if (-not (Test-Path $jarPath)) { Write-Output "NOT FOUND: $jarName"; continue }
    Write-Output "Searching $jarName ..."

    $zip = [System.IO.Compression.ZipFile]::OpenRead($jarPath)
    foreach ($entry in $zip.Entries) {
        if (-not $entry.Name.EndsWith('.class')) { continue }
        $stream = $entry.Open()
        $ms = [System.IO.MemoryStream]::new()
        $stream.CopyTo($ms)
        $stream.Dispose()
        $bytes = $ms.ToArray()
        $ms.Dispose()
        $str = [System.Text.Encoding]::Latin1.GetString($bytes)
        if ($str -match 'getServerGuid') {
            Write-Output "  HIT: $($entry.FullName)"
        }
    }
    $zip.Dispose()
}
Write-Output "Done."

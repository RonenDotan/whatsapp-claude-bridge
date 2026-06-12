$url = 'https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.3%2B9/OpenJDK25U-jdk_x64_windows_hotspot_25.0.3_9.msi'
$out = Join-Path $env:TEMP 'temurin25.msi'

Write-Host '[DOWNLOAD] Fetching Eclipse Temurin 25 (~200 MB)...'
Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing

Write-Host '[INSTALL] Running MSI installer as Administrator (UAC prompt will appear — please click Yes)...'
$proc = Start-Process msiexec.exe -ArgumentList "/i `"$out`" /norestart ADDLOCAL=FeatureMain,FeatureEnvironment,FeatureJarFileRunWith,FeatureJavaHome" -Verb RunAs -Wait -PassThru
Write-Host "[INSTALL] Exit code: $($proc.ExitCode)"

Remove-Item $out -Force -ErrorAction SilentlyContinue

Write-Host '[CHECK] Java version after install:'
& java --version 2>&1

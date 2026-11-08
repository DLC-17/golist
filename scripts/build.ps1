# GoList Windows Build Script
param (
    [string]$Version = "dev",
    [string]$Output = "golist.exe"
)

$ErrorActionPreference = "Stop"

try {
    $commit = (git rev-parse --short HEAD 2>$null)
    if (-not $commit) { $commit = "none" }
} catch {
    $commit = "none"
}

$date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$ldflags = "-s -w -X main.Version=$Version -X main.Commit=$commit -X main.Date=$date"

Write-Host "Building GoList ($Version, commit: $commit)..." -ForegroundColor Cyan
$env:CGO_ENABLED = "0"
go build -ldflags $ldflags -o $Output .

if ($LASTEXITCODE -eq 0) {
    Write-Host "Build succeeded: $Output" -ForegroundColor Green
    & ".\$Output" version
} else {
    Write-Error "Build failed"
}

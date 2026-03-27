#!/usr/bin/env pwsh
# prepare-web-vendor.ps1 — Downloads frontend vendor assets required before building.
# Run once after cloning: .\scripts\prepare-web-vendor.ps1
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$vendorRoot = "internal/web/static/vendor"

function Fetch($url, $out) {
    $dir = Split-Path $out -Parent
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    Write-Host "  $out"
    Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing -MaximumRetryCount 3 -RetryIntervalSec 2
}

Write-Host "Downloading vendor assets..."

Fetch "https://cdn.jsdelivr.net/npm/bootstrap@5.3.0-alpha3/dist/css/bootstrap.min.css" `
      "$vendorRoot/bootstrap-5.3.0-alpha3-dist/css/bootstrap.min.css"
Fetch "https://cdn.jsdelivr.net/npm/bootstrap@5.3.0-alpha3/dist/js/bootstrap.bundle.min.js" `
      "$vendorRoot/bootstrap-5.3.0-alpha3-dist/js/bootstrap.bundle.min.js"

Fetch "https://cdn.jsdelivr.net/npm/bootstrap-icons@1.10.5/font/bootstrap-icons.min.css" `
      "$vendorRoot/bootstrap-icons-1.10.5/font/bootstrap-icons.min.css"
Fetch "https://cdn.jsdelivr.net/npm/bootstrap-icons@1.10.5/font/fonts/bootstrap-icons.woff2" `
      "$vendorRoot/bootstrap-icons-1.10.5/font/fonts/bootstrap-icons.woff2"
Fetch "https://cdn.jsdelivr.net/npm/bootstrap-icons@1.10.5/font/fonts/bootstrap-icons.woff" `
      "$vendorRoot/bootstrap-icons-1.10.5/font/fonts/bootstrap-icons.woff"

Fetch "https://cdnjs.cloudflare.com/ajax/libs/cash/8.1.5/cash.min.js" `
      "$vendorRoot/cash.min.js"
Fetch "https://cdn.jsdelivr.net/npm/jmuxer@2.0.7/dist/jmuxer.min.js" `
      "$vendorRoot/jmuxer.min.js"
Fetch "https://cdn.jsdelivr.net/npm/chart.js@4.4.1/dist/chart.umd.min.js" `
      "$vendorRoot/chart.umd.min.js"

Write-Host "Done."

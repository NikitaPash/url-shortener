<#
.SYNOPSIS
  Per-container resource sampler for Windows Docker Desktop, where cAdvisor's VM
  path mounts are unreliable. Polls `docker stats` and appends a tidy CSV you can
  chart alongside the k6 run to NAME THE BOTTLENECK at saturation (CPU %, mem, net,
  block IO per container).

.NOTES
  Run this in a second terminal for the duration of a load test, then Ctrl+C.
  On the Linux droplet, prefer cAdvisor (docker-compose.loadtest.yml) instead.

.EXAMPLE
  pwsh loadtest/orchestrate/stats-poll.ps1 -IntervalSec 2
#>
[CmdletBinding()]
param(
  [int]$IntervalSec = 2,
  [string[]]$Containers = @(
    'shortener-go-api', 'shortener-redis', 'shortener-kafka',
    'shortener-clickhouse', 'shortener-clickhouse-consumer', 'shortener-postgres', 'shortener-nginx'
  )
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../..").Path
$resultsDir = Join-Path $repo 'loadtest/results'
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null
$stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmmss')
$csv = Join-Path $resultsDir "stats-$stamp.csv"

'timestamp,container,cpu_pct,mem_used,mem_pct,net_io,block_io' | Set-Content -Encoding utf8 $csv
Write-Host "Sampling docker stats every ${IntervalSec}s -> $csv  (Ctrl+C to stop)" -ForegroundColor Cyan

$fmt = '{{.Name}};{{.CPUPerc}};{{.MemUsage}};{{.MemPerc}};{{.NetIO}};{{.BlockIO}}'
while ($true) {
  $now = (Get-Date).ToUniversalTime().ToString('o')
  # One snapshot of all containers; filter to ours and normalise to CSV.
  $lines = & docker stats --no-stream --format $fmt
  foreach ($line in $lines) {
    $p = $line -split ';'
    if ($p.Count -lt 6) { continue }
    if ($Containers -notcontains $p[0]) { continue }
    # Strip commas inside fields (MemUsage/NetIO use "x / y") so the CSV stays clean.
    $row = @($now, $p[0], $p[1], ($p[2] -replace ',', ''), $p[3], ($p[4] -replace ',', ''), ($p[5] -replace ',', '')) -join ','
    Add-Content -Encoding utf8 -Path $csv -Value $row
  }
  Start-Sleep -Seconds $IntervalSec
}
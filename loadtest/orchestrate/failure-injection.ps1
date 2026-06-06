<#
.SYNOPSIS
  Exp 3 — failure injection under steady load. Drives a constant redirect stream,
  then kills a dependency mid-run and brings it back, recording a timeline you use
  to annotate Grafana. Demonstrates the availability properties:

    kafka      -> redirects keep succeeding, shortener_clicks_dropped_total climbs,
                  latency stays flat; publishing resumes on recovery.
    clickhouse -> consumer stops committing offsets (at-least-once); redirects
                  unaffected; on restart the buffered Kafka backlog REPLAYS into CH.
    redis      -> redirect falls back to Postgres (fail-open): serves with higher
                  latency rather than failing.

.NOTES
  Local rig: k6 hits go-api:8080 directly over the compose network. Run from the
  repo root. Stack must be up (docker compose up -d).

.EXAMPLE
  pwsh loadtest/orchestrate/failure-injection.ps1 -Target kafka
  pwsh loadtest/orchestrate/failure-injection.ps1 -Target clickhouse -Rate 400
#>
[CmdletBinding()]
param(
  [ValidateSet('kafka', 'clickhouse', 'redis')]
  [string]$Target = 'kafka',
  [int]$Rate = 300,          # steady redirects/s held for the whole run
  [int]$BaselineSec = 60,    # clean baseline before the outage
  [int]$OutageSec = 90,      # how long the dependency stays down
  [int]$RecoverySec = 90     # observe recovery after it returns
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot/../..").Path
$container = "shortener-$Target"
$resultsDir = Join-Path $repo 'loadtest/results'
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null

# clickhouse is stopped/started (to exercise offset-not-committed + replay); the
# others are paused/unpaused (faster, cleaner SIGSTOP of the process).
$useStop = ($Target -eq 'clickhouse')
$down = if ($useStop) { 'stop' } else { 'pause' }
$up = if ($useStop) { 'start' } else { 'unpause' }

# Total k6 window (+ buffer for seeding/teardown). redirect_capacity.js with a
# single flat STAGES step gives steady load for the whole window.
$total = $BaselineSec + $OutageSec + $RecoverySec + 30
$compose = @('compose', '-f', 'docker-compose.yml', '-f', 'docker-compose.loadtest.yml')

Write-Host "Failure-injection: target=$container  rate=$Rate/s  window=${total}s" -ForegroundColor Cyan
Write-Host "  baseline ${BaselineSec}s -> $down $container for ${OutageSec}s -> $up -> recovery ${RecoverySec}s`n"

# Launch k6 (steady redirect load) in the background.
$k6Job = Start-Job -ScriptBlock {
  param($repo, $compose, $rate, $total)
  Set-Location $repo
  $env:LOADTEST_STAGES = "${rate}:${total}s"
  & docker @compose run --rm k6 run /scripts/scenarios/redirect_capacity.js 2>&1
} -ArgumentList $repo, $compose, $Rate, $total

$events = [System.Collections.Generic.List[object]]::new()
function Record($label) {
  $ts = (Get-Date).ToUniversalTime().ToString('o')
  $events.Add([pscustomobject]@{ time = $ts; event = $label })
  Write-Host ("  [{0}] {1}" -f $ts, $label) -ForegroundColor Yellow
}

Record 'load_start (k6 seeding + ramp)'
Start-Sleep -Seconds $BaselineSec
Record "baseline_end -> $down $container"
& docker $down $container | Out-Null

Start-Sleep -Seconds $OutageSec
Record "$up $container -> recovery"
& docker $up $container | Out-Null

Start-Sleep -Seconds $RecoverySec
Record 'recovery_end (waiting for k6 to finish)'

Wait-Job $k6Job | Out-Null
$k6Out = Receive-Job $k6Job
Remove-Job $k6Job
Record 'load_end'

# Persist the timeline for Grafana annotations + the thesis appendix.
$stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmmss')
$outFile = Join-Path $resultsDir "failure-$Target-$stamp.txt"
$header = "Failure-injection run`ntarget=$container rate=$Rate/s baseline=${BaselineSec}s outage=${OutageSec}s recovery=${RecoverySec}s`n`nTIMELINE (UTC):"
$timeline = $events | ForEach-Object { "{0}  {1}" -f $_.time, $_.event }
@($header) + $timeline + @('', '--- k6 output ---') + $k6Out | Set-Content -Encoding utf8 $outFile

Write-Host "`nSaved timeline -> $outFile" -ForegroundColor Green
Write-Host "Use the UTC timestamps above to add annotations in Grafana, then export the panel as a thesis figure." -ForegroundColor Green
Write-Host "Check: shortener_redirects_total kept climbing through the outage; shortener_clicks_dropped_total rose during a kafka outage; http_req_failed stayed ~0." -ForegroundColor Green

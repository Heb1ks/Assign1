# ab_load_test.ps1
# ============================================================
# Load testing через Apache Benchmark (ab.exe)
# Locust запускается отдельно через: locust -f locustfile.py ...
#
# Требования:
#   ab.exe — есть в составе Apache HTTPD for Windows:
#   https://www.apachelounge.com/download/  (распаковать, добавить bin\ в PATH)
#   ИЛИ установить через chocolatey: choco install apache-httpd
#
# Запуск:
#   .\load-testing\ab_load_test.ps1
#   .\load-testing\ab_load_test.ps1 -Host "http://192.168.1.10:8080"
# ============================================================

param(
    [string]$TargetHost = "http://localhost:8080"
)

$ResultsDir = Join-Path $PSScriptRoot "ab-results"
if (-not (Test-Path $ResultsDir)) { New-Item -ItemType Directory -Path $ResultsDir | Out-Null }
$Timestamp = Get-Date -Format "yyyyMMdd_HHmmss"

# ── colour helpers ────────────────────────────────────────────
function Write-Section ([string]$Title) {
    Write-Host ""
    Write-Host ("=" * 50) -ForegroundColor Yellow
    Write-Host "  $Title" -ForegroundColor Yellow
    Write-Host ("=" * 50) -ForegroundColor Yellow
}

function Write-Info ([string]$Msg) {
    Write-Host "[INFO] $Msg" -ForegroundColor Cyan
}

# ── check ab ─────────────────────────────────────────────────
if (-not (Get-Command "ab" -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: 'ab.exe' not found in PATH." -ForegroundColor Red
    Write-Host "Install via: choco install apache-httpd" -ForegroundColor Yellow
    Write-Host "Or download from: https://www.apachelounge.com/download/" -ForegroundColor Yellow
    exit 1
}

# ── wait for gateway ─────────────────────────────────────────
Write-Info "Waiting for gateway at $TargetHost/health ..."
$ready = $false
for ($i = 1; $i -le 10; $i++) {
    try {
        $r = Invoke-WebRequest -Uri "$TargetHost/health" -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
        if ($r.StatusCode -eq 200) { $ready = $true; break }
    } catch {}
    Start-Sleep -Seconds 2
}
if (-not $ready) {
    Write-Host "ERROR: Gateway did not respond. Is it running?" -ForegroundColor Red
    exit 1
}
Write-Info "Gateway is ready."

# ── request body file ─────────────────────────────────────────
$BodyFile = Join-Path $env:TEMP "ab_notify_body.json"
@'
{"idempotency_key":"ab-test-fixed-key","channel":"email","recipient":"loadtest@clinic.example","message":"Appointment confirmed tomorrow at 10:00"}
'@ | Set-Content -Path $BodyFile -Encoding UTF8

# ── run ab ───────────────────────────────────────────────────
function Invoke-AbTest {
    param(
        [string]$Label,
        [string]$Url,
        [int]$N,
        [int]$C,
        [string]$Method = "GET"
    )

    Write-Info "Running: $Label  (n=$N c=$C method=$Method)"
    $OutFile = Join-Path $ResultsDir "${Timestamp}_${Label}.txt"

    if ($Method -eq "POST") {
        ab -n $N -c $C -p $BodyFile -T "application/json" -q $Url 2>&1 | Out-File -FilePath $OutFile -Encoding UTF8
    } else {
        ab -n $N -c $C -q $Url 2>&1 | Out-File -FilePath $OutFile -Encoding UTF8
    }

    # Parse metrics
    $content = Get-Content $OutFile -Raw
    $rps     = if ($content -match 'Requests per second:\s+([\d.]+)') { $Matches[1] } else { "N/A" }
    $p50     = if ($content -match '^\s+50%\s+(\d+)'m) { $Matches[1] } else { "N/A" }
    $p95     = if ($content -match '^\s+95%\s+(\d+)'m) { $Matches[1] } else { "N/A" }
    $p99     = if ($content -match '^\s+99%\s+(\d+)'m) { $Matches[1] } else { "N/A" }
    $failed  = if ($content -match 'Failed requests:\s+(\d+)') { $Matches[1] } else { "0" }

    Write-Host "  ✓ $Label" -ForegroundColor Green
    Write-Host "    RPS: $rps   p50: ${p50}ms   p95: ${p95}ms   p99: ${p99}ms   failed: $failed"

    return [PSCustomObject]@{
        Label  = $Label
        RPS    = $rps
        P50    = $p50
        P95    = $p95
        P99    = $p99
        Failed = $failed
    }
}

# ── test suite ────────────────────────────────────────────────
$results = @()

Write-Section "1. Smoke Test — GET /health"
$results += Invoke-AbTest "smoke_health"      "$TargetHost/health"  50    5  "GET"

Write-Section "2. Baseline — POST /notify (moderate)"
$results += Invoke-AbTest "baseline_notify"   "$TargetHost/notify"  500   10 "POST"

Write-Section "3. Sustained — POST /notify (high concurrency)"
$results += Invoke-AbTest "sustained_notify"  "$TargetHost/notify"  2000  50 "POST"

Write-Section "4. Spike — POST /notify (burst)"
$results += Invoke-AbTest "spike_notify"      "$TargetHost/notify"  1000 100 "POST"

Write-Section "5. Health under load — GET /health"
$results += Invoke-AbTest "health_under_load" "$TargetHost/health"  1000  25 "GET"

# ── summary ───────────────────────────────────────────────────
Write-Section "Summary"
$results | Format-Table -AutoSize

Write-Info "Full ab output saved to: $ResultsDir\"

# cleanup
Remove-Item $BodyFile -ErrorAction SilentlyContinue

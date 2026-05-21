# autoscale.ps1
# ============================================================
# Auto-scaler для Medical Scheduling Platform
# Запуск: .\autoscaling\autoscale.ps1
# С параметрами: .\autoscaling\autoscale.ps1 -MaxReplicas 4 -CooldownSeconds 20
# ============================================================

param(
    [int]$PollInterval        = 10,
    [int]$ScaleUpThreshold    = 70,
    [int]$ScaleDownThreshold  = 20,
    [int]$ScaleUpConsecutive  = 2,
    [int]$ScaleDownConsecutive = 5,
    [int]$CooldownSeconds     = 30,
    [int]$MinReplicas         = 1,
    [int]$MaxReplicas         = 5,
    [string]$ComposeFile      = "docker-compose.scaling.yml"
)

$WatchedServices = @("doctor-service", "appointment-service", "notification-service", "mock-gateway")

# ── state ────────────────────────────────────────────────────
$upCount    = @{}
$downCount  = @{}
$replicas   = @{}
$lastScale  = @{}

foreach ($svc in $WatchedServices) {
    $upCount[$svc]   = 0
    $downCount[$svc] = 0
    $replicas[$svc]  = 1
    $lastScale[$svc] = [datetime]::MinValue
}

# ── helpers ──────────────────────────────────────────────────
function Write-Log {
    param([string]$Level, [string]$Msg)
    $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $color = switch ($Level) {
        "SCALE_UP"   { "Green"  }
        "SCALE_DOWN" { "Cyan"   }
        "WARN"       { "Yellow" }
        default      { "Gray"   }
    }
    Write-Host "[$ts] [$Level] $Msg" -ForegroundColor $color
}

function Get-CpuPercent {
    param([string]$ServiceName)

    # docker stats --no-stream возвращает строки: NAME  CPU%  MEM  ...
    $lines = docker stats --no-stream --format "{{.Name}} {{.CPUPerc}}" 2>$null |
             Where-Object { $_ -imatch $ServiceName }

    if (-not $lines) { return 0.0 }

    $total = 0.0
    $count = 0
    foreach ($line in $lines) {
        $parts = $line -split '\s+'
        if ($parts.Count -ge 2) {
            $cpu = $parts[1] -replace '%', ''
            if ([double]::TryParse($cpu, [ref]$null)) {
                $total += [double]$cpu
                $count++
            }
        }
    }

    if ($count -eq 0) { return 0.0 }
    return [math]::Round($total / $count, 1)
}

function Test-InCooldown {
    param([string]$ServiceName)
    $elapsed = (Get-Date) - $lastScale[$ServiceName]
    return $elapsed.TotalSeconds -lt $CooldownSeconds
}

function Invoke-Scale {
    param([string]$ServiceName, [int]$N)
    Write-Log "INFO" "Scaling '$ServiceName' to $N replicas..."
    docker compose -f $ComposeFile up -d --scale "${ServiceName}=${N}" --no-recreate 2>&1 |
        Select-Object -Last 3 | ForEach-Object { Write-Log "INFO" "  $_" }
    $replicas[$ServiceName] = $N
    $lastScale[$ServiceName] = Get-Date
}

# ── main loop ────────────────────────────────────────────────
Write-Log "INFO" "Auto-scaler started"
Write-Log "INFO" "Services: $($WatchedServices -join ', ')"
Write-Log "INFO" "Scale-up: CPU > $ScaleUpThreshold% for $ScaleUpConsecutive polls"
Write-Log "INFO" "Scale-down: CPU < $ScaleDownThreshold% for $ScaleDownConsecutive polls"
Write-Log "INFO" "Replicas: min=$MinReplicas max=$MaxReplicas | cooldown=${CooldownSeconds}s | poll=${PollInterval}s"
Write-Host ""

try {
    while ($true) {
        foreach ($svc in $WatchedServices) {
            $cpu = Get-CpuPercent -ServiceName $svc
            $cur = $replicas[$svc]

            Write-Log "INFO" "  ${svc}: CPU=${cpu}%  replicas=$cur  up_streak=$($upCount[$svc])  down_streak=$($downCount[$svc])"

            if (Test-InCooldown -ServiceName $svc) {
                Write-Log "INFO" "  ${svc}: in cooldown — skip"
                continue
            }

            if ($cpu -gt $ScaleUpThreshold) {
                $upCount[$svc]++
                $downCount[$svc] = 0

                if ($upCount[$svc] -ge $ScaleUpConsecutive) {
                    $new = $cur + 1
                    if ($new -le $MaxReplicas) {
                        Write-Log "SCALE_UP" "${svc}: CPU=${cpu}% > ${ScaleUpThreshold}% for $ScaleUpConsecutive polls → scaling $cur→$new"
                        Invoke-Scale -ServiceName $svc -N $new
                        $upCount[$svc] = 0
                    } else {
                        Write-Log "WARN" "${svc}: already at MAX_REPLICAS ($MaxReplicas)"
                        $upCount[$svc] = 0
                    }
                }
            }
            elseif ($cpu -lt $ScaleDownThreshold) {
                $downCount[$svc]++
                $upCount[$svc] = 0

                if ($downCount[$svc] -ge $ScaleDownConsecutive) {
                    $new = $cur - 1
                    if ($new -ge $MinReplicas) {
                        Write-Log "SCALE_DOWN" "${svc}: CPU=${cpu}% < ${ScaleDownThreshold}% for $ScaleDownConsecutive polls → scaling $cur→$new"
                        Invoke-Scale -ServiceName $svc -N $new
                        $downCount[$svc] = 0
                    } else {
                        $downCount[$svc] = 0
                    }
                }
            }
            else {
                $upCount[$svc]   = 0
                $downCount[$svc] = 0
            }
        }

        Start-Sleep -Seconds $PollInterval
    }
}
catch [System.Management.Automation.PipelineStoppedException] {
    Write-Log "INFO" "Auto-scaler stopped (Ctrl+C)"
}
finally {
    Write-Log "INFO" "Auto-scaler exited"
}

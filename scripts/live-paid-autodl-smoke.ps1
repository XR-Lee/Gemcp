[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$confirmation = "I_ACCEPT_AUTODL_CHARGES"
$resultPath = Join-Path ([System.IO.Path]::GetTempPath()) "gemcp-autodl-smoke-last.json"
$errorPath = Join-Path ([System.IO.Path]::GetTempPath()) "gemcp-autodl-smoke-last.stderr.log"
$secureToken = $null
$plainToken = $null
$tokenPointer = [IntPtr]::Zero

Write-Host "Gemcp minimal AutoDL Public Elastic smoke Job"
Write-Host "Maximum authorized spend: 100 milli-CNY (CNY 0.10)"
Write-Host "Runtime ceiling: 5 seconds; price ceiling: 1,980 milli-CNY/hour"
Write-Host "Resource: westDC2 / one currently idle GPU / one visible private image"
Write-Host "Program: write gemcp-autodl-smoke-ok, then exit"
Write-Host "The Job is stopped and deleted during cleanup. Only the Token is requested."
Write-Host "Sanitized report: $resultPath"
Write-Host ""

try {
    $secureToken = Read-Host "AutoDL Public Cloud Developer Token (input hidden)" -AsSecureString
    $tokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureToken)
    $plainToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($tokenPointer)
    if ([string]::IsNullOrWhiteSpace($plainToken)) {
        throw "The AutoDL Token is empty."
    }
    $env:GEMCP_PHASE0_AUTODL_TOKEN = $plainToken

    Remove-Item -LiteralPath $resultPath, $errorPath -Force -ErrorAction SilentlyContinue
    Push-Location (Resolve-Path (Join-Path $PSScriptRoot ".."))
    try {
        Write-Host "Token accepted locally. Reading inventory and running the minimal Job."
        & go run ./cmd/gemcp phase0 smoke `
            --region westDC2 `
            --price-ceiling-milli-per-hour 1980 `
            --spend-cap-milli 100 `
            --confirm-live-spend=$confirmation `
            1> $resultPath 2> $errorPath
        $jobExitCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }

    if (Test-Path -LiteralPath $resultPath) {
        Get-Content -LiteralPath $resultPath
    }
    if ($jobExitCode -ne 0) {
        if (Test-Path -LiteralPath $errorPath) {
            Get-Content -LiteralPath $errorPath
        }
        throw "Minimal AutoDL smoke Job failed with exit code $jobExitCode."
    }
    Write-Host "Minimal AutoDL smoke Job succeeded and cleanup returned successfully."
}
catch {
    Write-Error ("ERROR: " + $_.Exception.Message)
    exit 1
}
finally {
    if ($tokenPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($tokenPointer)
    }
    $plainToken = $null
    $secureToken = $null
    $env:GEMCP_PHASE0_AUTODL_TOKEN = $null
}

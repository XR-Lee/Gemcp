[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$confirmation = "I_ACCEPT_AUTODL_CHARGES"
$tokenPath = Join-Path ([System.IO.Path]::GetTempPath()) ("gemcp-autodl-token-" + [guid]::NewGuid().ToString("N") + ".txt")
$resultPath = Join-Path ([System.IO.Path]::GetTempPath()) "gemcp-live-paid-issue5-last.log"
$secureToken = $null
$plainToken = $null
$tokenPointer = [IntPtr]::Zero

Write-Host "Gemcp Issue #5 PAID MCP end-to-end validation"
Write-Host "Maximum authorized spend: 350 milli-CNY (CNY 0.35)"
Write-Host "Calculated conservative reservation: 350 milli-CNY (CNY 0.350)"
Write-Host "Resource: westDC2 / one currently idle GPU / price ceiling 1,980 milli-CNY/hour"
Write-Host "The test stops and deletes its owned deployment through Watchdog, with compensating cleanup on failure."
Write-Host "Running this script authorizes the displayed paid validation; only the Token is requested."
Write-Host "Sanitized run log: $resultPath"
Write-Host ""

[System.IO.File]::WriteAllText($resultPath, "Gemcp Issue #5 paid MCP validation started at $([DateTime]::UtcNow.ToString('O'))`r`n", [System.Text.UTF8Encoding]::new($false))

try {
    $secureToken = Read-Host "AutoDL Public Cloud Developer Token (input hidden)" -AsSecureString
    $tokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureToken)
    $plainToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($tokenPointer)
    if ([string]::IsNullOrWhiteSpace($plainToken)) {
        throw "The AutoDL Token is empty."
    }

    [System.IO.File]::WriteAllText($tokenPath, $plainToken, [System.Text.UTF8Encoding]::new($false))
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $acl = [Security.AccessControl.FileSecurity]::new()
    $acl.SetOwner($identity.User)
    $acl.SetAccessRuleProtection($true, $false)
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new(
        $identity.User,
        [Security.AccessControl.FileSystemRights]::FullControl,
        [Security.AccessControl.AccessControlType]::Allow
    ))
    [System.IO.File]::SetAccessControl($tokenPath, $acl)

    $env:GEMCP_TEST_ELASTIC_TOKEN_FILE = $tokenPath
    $env:GEMCP_TEST_ELASTIC_REGION = "westDC2"
    $env:GEMCP_TEST_LIVE_SPEND_CONFIRM = $confirmation
    $env:GEMCP_TEST_LIVE_SPEND_CAP_MILLI = "350"

    Push-Location (Resolve-Path (Join-Path $PSScriptRoot ".."))
    try {
        Write-Host "Token accepted locally. Validation is running; AutoDL provisioning may take up to 10 minutes."
        $workflowAttempts = 3
        for ($workflowAttempt = 1; $workflowAttempt -le $workflowAttempts; $workflowAttempt++) {
            $attemptOutput = @()
            & go test ./internal/mcpserver -run '^TestLivePaidIssue5MCPWorkflow$' -count=1 -v -timeout 20m 2>&1 |
                Tee-Object -Variable attemptOutput |
                ForEach-Object {
                    $safeLine = $_.ToString()
                    Write-Host $safeLine
                    [System.IO.File]::AppendAllText($resultPath, $safeLine + "`r`n", [System.Text.UTF8Encoding]::new($false))
                }
            $testExitCode = $LASTEXITCODE
            $attemptTunnelFailure = @($attemptOutput | Where-Object {
                $_.ToString().Contains("cloudflared tunnel preflight failed after")
            }).Count -gt 0
            $attemptProviderCreated = @($attemptOutput | Where-Object {
                $_.ToString().Contains("scheduler created and durably bound the managed Provider resource")
            }).Count -gt 0
            if ($testExitCode -eq 0) {
                break
            }
            if ($attemptTunnelFailure -and -not $attemptProviderCreated -and $workflowAttempt -lt $workflowAttempts) {
                $retryLine = "Quick Tunnel failed before Provider spend; retrying without requesting the Token again ($($workflowAttempt + 1)/$workflowAttempts)."
                Write-Host $retryLine
                [System.IO.File]::AppendAllText($resultPath, $retryLine + "`r`n", [System.Text.UTF8Encoding]::new($false))
                continue
            }
            throw "Live MCP validation failed with exit code $testExitCode."
        }
    }
    finally {
        Pop-Location
    }
}
catch {
    $safeError = "ERROR: " + $_.Exception.Message
    [System.IO.File]::AppendAllText($resultPath, $safeError + "`r`n", [System.Text.UTF8Encoding]::new($false))
    Write-Error $safeError
    exit 1
}
finally {
    if ($tokenPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($tokenPointer)
    }
    $plainToken = $null
    $secureToken = $null
    $env:GEMCP_TEST_ELASTIC_TOKEN_FILE = $null
    $env:GEMCP_TEST_ELASTIC_REGION = $null
    $env:GEMCP_TEST_LIVE_SPEND_CONFIRM = $null
    $env:GEMCP_TEST_LIVE_SPEND_CAP_MILLI = $null
    if (Test-Path -LiteralPath $tokenPath) {
        Remove-Item -LiteralPath $tokenPath -Force
    }
}

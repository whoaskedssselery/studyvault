param(
    [ValidateSet('api', 'demo')][string]$Mode = 'api',
    [switch]$NoBrowser
)
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

if ($Mode -eq 'demo') {
    if (!(Get-Command node -ErrorAction SilentlyContinue)) { throw 'Install Node.js 22 or newer to open the portable demo.' }
    $demoUrl = 'http://localhost:4174/?mode=demo'
    Write-Host "StudyVault demo: $demoUrl"
    & node (Join-Path $PSScriptRoot 'deliverables/frontend/serve.mjs') --port 4174
    exit $LASTEXITCODE
}

if (!(Get-Command docker -ErrorAction SilentlyContinue)) { throw 'Install and start Docker Desktop, then run this script again.' }
& docker info --format '{{.ServerVersion}}'
if ($LASTEXITCODE -ne 0) { throw 'Docker Engine is not ready. Open Docker Desktop, wait for Engine running, and retry.' }
if (!(Test-Path -LiteralPath '.env')) { Copy-Item -LiteralPath '.env.example' -Destination '.env' }
Write-Host 'Building StudyVault and starting the API, database, search, files, mailbox and website...'
& docker compose --profile app --profile web up -d --build
if ($LASTEXITCODE -ne 0) { throw 'The project could not start. See the Docker output above.' }

$ready = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    try {
        $health = Invoke-RestMethod -Uri 'http://localhost:5173/v1/health' -TimeoutSec 3
        if ($health.status -eq 'ok') { $ready = $true; break }
    } catch { }
    Start-Sleep -Seconds 2
}
if (!$ready) { throw 'API is not ready. Run: docker compose --profile app --profile web logs --tail 60 api frontend' }
& docker compose --profile app run --rm --entrypoint /seed api
if ($LASTEXITCODE -ne 0) { throw 'Could not prepare demo materials. Check the seed output above.' }

Write-Host ''
Write-Host 'StudyVault: http://localhost:5173/?mode=api'
Write-Host 'Local mailbox: http://localhost:8025/'
Write-Host 'Student: demo@example.com | Moderator: moderator@example.com'
Write-Host 'Demo password: SEED_PASSWORD from .env (example: demo-password-123)'
Write-Host 'Stop: docker compose --profile app --profile web stop'
if (!$NoBrowser) { Start-Process 'http://localhost:5173/?mode=api' }

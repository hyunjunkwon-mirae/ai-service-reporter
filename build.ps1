# =============================================================================
# AI Service Reporter Plugin — Windows PowerShell 빌드 스크립트
#
# 사용법:
#   PS> .\build.ps1            # 풀빌드 (deps + webapp + server + tar)
#   PS> .\build.ps1 deps       # Go/npm 의존성만 받기 (go.sum 생성 등)
#   PS> .\build.ps1 webapp     # webapp 만
#   PS> .\build.ps1 server     # server linux-amd64 만
#   PS> .\build.ps1 server-all # 모든 OS/Arch
#   PS> .\build.ps1 clean
#
# 사전 조건:
#   · Go 1.21+ (https://go.dev/dl/)
#   · Node 18+ (https://nodejs.org/)
#   · tar.exe (Windows 10/11 기본 제공)
# =============================================================================

param(
  [string]$Target = "all"
)

$ErrorActionPreference = "Stop"
$PluginId = "ai_service_reporter"
$Root = $PSScriptRoot

function Step($name) {
  Write-Host ""
  Write-Host "==> $name" -ForegroundColor Cyan
}

# =============================================================================
# 의존성 받기 (go.sum 자동 생성, node_modules 자동 설치)
# =============================================================================
function Fetch-Deps {
  Step "Go 의존성 받기 (go.sum 생성)"
  Push-Location $Root
  try {
    if (-not (Test-Path "go.sum")) {
      Write-Host "  go.sum 이 없습니다. go mod download 로 받습니다..." -ForegroundColor Yellow
    }
    go mod download
    if ($LASTEXITCODE -ne 0) {
      Write-Host ""
      Write-Host "❌ go mod download 실패." -ForegroundColor Red
      Write-Host "   사내 프록시 환경이면 다음 중 하나 시도:" -ForegroundColor Yellow
      Write-Host "     1) GOPROXY 설정: " -NoNewline; Write-Host "go env -w GOPROXY=https://proxy.golang.org,direct" -ForegroundColor Gray
      Write-Host "     2) 직접 모드:    " -NoNewline; Write-Host "go env -w GOPROXY=direct" -ForegroundColor Gray
      Write-Host "     3) 회사 내부 프록시 주소가 있다면 그걸로 GOPROXY 설정" -ForegroundColor Gray
      throw "go mod download 실패"
    }
    # go mod download 만으로 go.sum 의 간접 의존성 entry 가 누락되는 환경이 있어
    # go mod tidy 로 한 번 정렬. 이게 없으면 build 단계에서 "missing go.sum entry" 에러 발생.
    go mod tidy
    if ($LASTEXITCODE -ne 0) {
      throw "go mod tidy 실패"
    }
    if (-not (Test-Path "go.sum")) {
      throw "go.sum 이 생성되지 않았습니다."
    }
    Write-Host "OK -> go.sum" -ForegroundColor Green
  } finally {
    Pop-Location
  }

  Step "Node 의존성 받기"
  Push-Location "$Root\webapp"
  try {
    if (-not (Test-Path "node_modules")) {
      npm install --no-audit --no-fund
      if ($LASTEXITCODE -ne 0) { throw "npm install 실패" }
      Write-Host "OK -> webapp\node_modules" -ForegroundColor Green
    } else {
      Write-Host "  webapp\node_modules 이미 있음. skip." -ForegroundColor DarkGray
    }
  } finally {
    Pop-Location
  }
}

# =============================================================================
# Webapp 빌드
# =============================================================================
function Build-Webapp {
  Step "Webapp 빌드"
  Push-Location "$Root\webapp"
  try {
    if (-not (Test-Path "node_modules")) {
      npm install --no-audit --no-fund
      if ($LASTEXITCODE -ne 0) { throw "npm install 실패" }
    }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "webpack build 실패" }

    if (-not (Test-Path "dist\main.js")) {
      throw "webapp\dist\main.js 가 생성되지 않았습니다."
    }
    Write-Host "OK -> webapp\dist\main.js" -ForegroundColor Green
  } finally {
    Pop-Location
  }
}

# =============================================================================
# Server 빌드 (cross-compile)
# =============================================================================
function Build-Server {
  param([string]$Os = "linux", [string]$Arch = "amd64")

  Step "Server 빌드 ($Os-$Arch)"

  # go.sum 이 없으면 먼저 받기
  if (-not (Test-Path "$Root\go.sum")) {
    Write-Host "  go.sum 미발견 — Fetch-Deps 자동 호출" -ForegroundColor Yellow
    Fetch-Deps
  }

  Push-Location "$Root\server"
  try {
    if (-not (Test-Path "dist")) {
      New-Item -ItemType Directory -Path "dist" | Out-Null
    }
    $env:GOOS = $Os
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = "0"

    $outName = "plugin-$Os-$Arch"
    if ($Os -eq "windows") { $outName = "$outName.exe" }

    go build -o "dist\$outName"
    if ($LASTEXITCODE -ne 0) { throw "go build 실패 ($Os-$Arch)" }
    Write-Host "OK -> server\dist\$outName" -ForegroundColor Green
  } finally {
    $env:GOOS = $null
    $env:GOARCH = $null
    Pop-Location
  }
}

function Build-AllServers {
  Build-Server -Os "linux"   -Arch "amd64"
  # 운영 Mattermost 가 linux-amd64 만 사용하므로 다른 플랫폼은 빌드 생략.
  # 필요시 아래 주석 해제:
  # Build-Server -Os "linux"   -Arch "arm64"
  # Build-Server -Os "darwin"  -Arch "amd64"
  # Build-Server -Os "darwin"  -Arch "arm64"
  # Build-Server -Os "windows" -Arch "amd64"
}

# =============================================================================
# 풀 번들 (deps → server → webapp → tar.gz)
# =============================================================================
function Build-Dist {
  Fetch-Deps
  Build-AllServers
  Build-Webapp

  Step "Bundle 생성"

  $DistDir = "$Root\dist"
  $StageDir = "$DistDir\$PluginId"

  if (Test-Path $DistDir) { Remove-Item -Recurse -Force $DistDir }
  New-Item -ItemType Directory -Path $StageDir | Out-Null

  # 매니페스트 + 아이콘
  Copy-Item "$Root\plugin.json" $StageDir
  if (Test-Path "$Root\assets") {
    Copy-Item -Recurse "$Root\assets" $StageDir
  }

  # 서버 바이너리 — server/dist/plugin-linux-amd64 구조 명시 생성
  # (PowerShell 의 Copy-Item -Recurse 가 환경/버전에 따라 폴더명을 보존하지 않고
  #  내용물만 복사하는 경우가 있어, 그 결과 Mattermost 가 manifest 의
  #  server_executable_path 를 찾지 못해 "unable to find manifest" 에러 발생.
  #  파일을 개별 Copy 로 명시 처리)
  New-Item -ItemType Directory -Path "$StageDir\server\dist" -Force | Out-Null
  Get-ChildItem "$Root\server\dist" -File | ForEach-Object {
    Copy-Item $_.FullName "$StageDir\server\dist\"
  }

  # webapp 번들 — webapp/dist/main.js 구조 명시 생성 (위와 같은 이유)
  New-Item -ItemType Directory -Path "$StageDir\webapp\dist" -Force | Out-Null
  Get-ChildItem "$Root\webapp\dist" -File | ForEach-Object {
    Copy-Item $_.FullName "$StageDir\webapp\dist\"
  }

  # tar.gz 묶기 — Go packer 사용 (Windows tar.exe 가 --mode 미지원이라 직접 헤더 작성)
  # plugin-linux-* / plugin-darwin-* / plugin-windows-* 는 0755, 나머지는 0644 로 박힘
  & go run "$Root\tools\pack\main.go" "$DistDir\$PluginId" "$DistDir\$PluginId.tar.gz"
  if ($LASTEXITCODE -ne 0) { throw "tar.gz 생성 실패" }

  $TarFile = "$DistDir\$PluginId.tar.gz"

  # dist 폴더 ACL — 빌드 후 현재 사용자에게 Full Control 명시
  # (Defender / 과거 admin 실행 등으로 ACL 망가져도 다음 빌드부터 본인이 삭제 가능하도록)
  icacls $DistDir /grant "$($env:USERNAME):F" /T /Q 2>&1 | Out-Null

  Write-Host ""
  Write-Host "✅ Bundle ready -> $TarFile" -ForegroundColor Green
  $sizeMB = [math]::Round((Get-Item $TarFile).Length / 1MB, 2)
  Write-Host "   Size: $sizeMB MB" -ForegroundColor DarkGray
}

function Clean {
  Step "Clean"
  @("$Root\dist", "$Root\server\dist", "$Root\webapp\dist", "$Root\webapp\node_modules") |
    ForEach-Object {
      if (Test-Path $_) {
        Remove-Item -Recurse -Force $_
        Write-Host "  removed $_"
      }
    }
}

# =============================================================================
# Entry
# =============================================================================
switch ($Target.ToLower()) {
  "all"        { Build-Dist }
  "dist"       { Build-Dist }
  "deps"       { Fetch-Deps }
  "webapp"     { Build-Webapp }
  "server"     { Build-Server -Os "linux" -Arch "amd64" }
  "server-all" { Build-AllServers }
  "clean"      { Clean }
  default {
    Write-Host "Usage: .\build.ps1 [all|deps|webapp|server|server-all|clean]" -ForegroundColor Yellow
    exit 1
  }
}

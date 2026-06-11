# TTM Installer for Windows
# Usage: irm https://raw.githubusercontent.com/vst93/ttm/main/cmd/install.ps1 | iex
# Or: irm https://cdn.jsdelivr.net/gh/vst93/ttm@main/cmd/install.ps1 | iex

param(
    [string]$InstallDir,
    [switch]$Force,
    [string]$Lang
)

$ErrorActionPreference = "Stop"

$REPO_OWNER = "vst93"
$REPO_NAME = "ttm"
$BINARY_NAME = "ttm"
$REPO_URL = "https://github.com/$REPO_OWNER/$REPO_NAME"
$API_URL = "https://api.github.com/repos/$REPO_OWNER/$REPO_NAME/releases/latest"

$GITHUB_MIRRORS = @(
    "https://ghfast.top",
    "https://mirror.ghproxy.com",
    "https://gh-proxy.com",
    "https://gh-proxy.net"
)

# ──────────────────────────────────────────────────────────────────────────────
# Language
# ──────────────────────────────────────────────────────────────────────────────

if (-not $Lang) {
    $Lang = $env:TTM_LANG
}

function t($en, $zh) {
    if ($Lang -eq "zh") { return $zh }
    return $en
}

function Select-Language {
    if ($Lang -and $Lang -match "^(en|zh)$") { return }
    
    Write-Host "  [1] English  [2] " -NoNewline
    Write-Host "中文" -NoNewline
    Write-Host " (default: 1): " -NoNewline
    $choice = Read-Host
    switch ($choice) {
        "2" { $script:Lang = "zh" }
        "cn" { $script:Lang = "zh" }
        "zh" { $script:Lang = "zh" }
        default { $script:Lang = "en" }
    }
    Write-Host "  $(t 'Language: English' '语言: 中文')" -ForegroundColor Green
}

# ──────────────────────────────────────────────────────────────────────────────
# Logging
# ──────────────────────────────────────────────────────────────────────────────

function Log-Info($msg) { Write-Host "  ✓ $msg" -ForegroundColor Green }
function Log-Warn($msg) { Write-Host "  ⚠ $msg" -ForegroundColor Yellow }
function Log-Error($msg) { Write-Host "  ✗ $msg" -ForegroundColor Red }
function Log-Step($msg) { Write-Host "  ▸ $msg" -ForegroundColor Cyan }

# ──────────────────────────────────────────────────────────────────────────────
# Platform Detection
# ──────────────────────────────────────────────────────────────────────────────

function Get-Platform {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default {
            Log-Error (t "Unsupported architecture: $arch" "不支持的架构: $arch")
            exit 1
        }
    }
}

# ──────────────────────────────────────────────────────────────────────────────
# Version Detection
# ──────────────────────────────────────────────────────────────────────────────

function Get-LatestVersion {
    $latest_asset = "$BINARY_NAME-windows-amd64.zip"
    $latest_url = "$REPO_URL/releases/latest/download/$latest_asset"
    
    # Try direct GitHub first
    Log-Info (t "Trying GitHub..." "尝试 GitHub...")
    try {
        $request = [System.Net.WebRequest]::Create($latest_url)
        $request.Method = "HEAD"
        $request.AllowAutoRedirect = $false
        $request.Timeout = 15000
        $response = $request.GetResponse()
        $location = $response.Headers["Location"]
        $response.Close()
        
        if ($location -and $location -match '/download/v?([0-9]+\.[0-9]+[0-9.]*)') {
            $version = $matches[1] -replace '^v', ''
            Log-Info (t "Latest version: $version" "最新版本: $version")
            return $version
        }
    } catch {
        Log-Warn (t "GitHub failed, trying mirrors..." "GitHub 失败，尝试镜像...")
    }
    
    # Try mirrors
    foreach ($mirror in $GITHUB_MIRRORS) {
        try {
            $mirror_url = "$mirror/$latest_url"
            $request = [System.Net.WebRequest]::Create($mirror_url)
            $request.Method = "HEAD"
            $request.AllowAutoRedirect = $false
            $request.Timeout = 15000
            $response = $request.GetResponse()
            $location = $response.Headers["Location"]
            $response.Close()
            
            if ($location -and $location -match '/download/v?([0-9]+\.[0-9]+[0-9.]*)') {
                $version = $matches[1] -replace '^v', ''
                Log-Info (t "Latest version: $version" "最新版本: $version")
                return $version
            }
        } catch {
            continue
        }
    }
    
    # Fallback: jsdelivr VERSION file
    Log-Info (t "Trying jsdelivr..." "尝试 jsdelivr...")
    try {
        $version_url = "https://cdn.jsdelivr.net/gh/$REPO_OWNER/$REPO_NAME@main/VERSION"
        $response = Invoke-WebRequest -Uri $version_url -UseBasicParsing -TimeoutSec 10
        $version = ($response.Content -replace '\s+', '').Trim()
        if ($version -match '^[0-9]+\.[0-9]+') {
            Log-Info (t "Latest version: $version" "最新版本: $version")
            return $version
        }
    } catch {
        Log-Warn (t "jsdelivr failed" "jsdelivr 失败")
    }
    
    # Last resort: GitHub API
    Log-Info (t "Trying API..." "尝试 API...")
    try {
        $response = Invoke-WebRequest -Uri $API_URL -UseBasicParsing -TimeoutSec 15
        $json = $response.Content | ConvertFrom-Json
        $version = $json.tag_name -replace '^v', ''
        Log-Info (t "Latest version: $version" "最新版本: $version")
        return $version
    } catch {
        Log-Error (t "Failed to get version" "获取版本失败")
        exit 1
    }
}

# ──────────────────────────────────────────────────────────────────────────────
# Download
# ──────────────────────────────────────────────────────────────────────────────

function Download-File($url, $output) {
    $filename = Split-Path $url -Leaf
    
    try {
        Log-Info (t "Downloading: $filename" "下载: $filename")
        
        # Use .NET WebClient for better progress support
        $wc = New-Object System.Net.WebClient
        $wc.DownloadFile($url, $output)
        $wc.Dispose()
        
        if (Test-Path $output) {
            $size = (Get-Item $output).Length
            $sizeText = if ($size -ge 1MB) { "{0:N1}MB" -f ($size / 1MB) } 
                       elseif ($size -ge 1KB) { "{0:N1}KB" -f ($size / 1KB) }
                       else { "$size B" }
            Log-Info (t "Downloaded: $filename ($sizeText)" "已下载: $filename ($sizeText)")
            return $true
        }
    } catch {
        Log-Warn (t "Direct download failed" "直连下载失败")
    }
    return $false
}

function Download-WithMirrors($url, $output) {
    if (Download-File $url $output) { return $true }
    
    foreach ($mirror in $GITHUB_MIRRORS) {
        $mirror_url = "$mirror/$url"
        Log-Warn (t "Trying mirror: $mirror" "尝试镜像: $mirror")
        if (Download-File $mirror_url $output) { return $true }
    }
    
    Log-Error (t "All downloads failed" "所有下载均失败")
    return $false
}

# ──────────────────────────────────────────────────────────────────────────────
# Verification
# ──────────────────────────────────────────────────────────────────────────────

function Get-SHA256($file) {
    try {
        $hash = Get-FileHash -Path $file -Algorithm SHA256
        return $hash.Hash.ToLower()
    } catch {
        return $null
    }
}

function Get-RemoteSHA256($url) {
    try {
        $sha_url = "$url.sha256"
        $response = Invoke-WebRequest -Uri $sha_url -UseBasicParsing -TimeoutSec 15
        $content = $response.Content -replace '\r?\n', ' '
        if ($content -match '([a-f0-9]{64})') {
            return $matches[1].ToLower()
        }
    } catch {
        # SHA256 file not available
    }
    return $null
}

function Verify-SHA256($file, $expected) {
    $actual = Get-SHA256 $file
    if (-not $actual) {
        Log-Warn (t "No SHA256 tool, skipping verification" "无 SHA256 工具，跳过验证")
        return $true
    }
    if ($actual -ne $expected) {
        Log-Error (t "SHA256 mismatch" "SHA256 不匹配")
        Log-Error "Expected: $expected"
        Log-Error "Actual:   $actual"
        return $false
    }
    Log-Info (t "SHA256 OK" "SHA256 校验通过")
    return $true
}

# ──────────────────────────────────────────────────────────────────────────────
# Installation
# ──────────────────────────────────────────────────────────────────────────────

function Get-DefaultInstallDir {
    if ($InstallDir) { return $InstallDir }
    
    # Prefer user-local install (no admin required)
    $userBin = Join-Path $env:LOCALAPPDATA "Programs\ttm"
    return $userBin
}

function Add-ToPath($dir) {
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -split ";" -contains $dir) {
        return
    }
    
    Log-Warn (t "Adding $dir to PATH" "将 $dir 添加到 PATH")
    [Environment]::SetEnvironmentVariable("Path", "$currentPath;$dir", "User")
    $env:Path = "$env:Path;$dir"
    Log-Info (t "PATH updated (restart terminal to take effect)" "PATH 已更新（重启终端生效）")
}

function Install-Binary($zipFile, $installDir) {
    $extractDir = Join-Path $env:TEMP "ttm-extract-$(Get-Random)"
    
    try {
        Log-Info (t "Extracting..." "解压中...")
        Expand-Archive -Path $zipFile -DestinationPath $extractDir -Force
        
        $exePath = Join-Path $extractDir "$BINARY_NAME.exe"
        if (-not (Test-Path $exePath)) {
            Log-Error (t "Binary not found in archive" "压缩包中未找到程序")
            exit 1
        }
        
        if (-not (Test-Path $installDir)) {
            New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        }
        
        $destPath = Join-Path $installDir "$BINARY_NAME.exe"
        Copy-Item -Path $exePath -Destination $destPath -Force
        
        Log-Info (t "Installed: $destPath" "已安装: $destPath")
        
        # Verify installation
        & $destPath --version 2>$null
        
        # Add to PATH if needed
        Add-ToPath $installDir
        
    } finally {
        if (Test-Path $extractDir) {
            Remove-Item -Path $extractDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

# ──────────────────────────────────────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────────────────────────────────────

function Main {
    Select-Language
    
    Write-Host "  ▸ TTM Installer" -ForegroundColor Cyan -Bold
    
    Log-Step (t "Version" "获取版本")
    $version = Get-LatestVersion
    
    Log-Step (t "Platform" "检测平台")
    $arch = Get-Platform
    Log-Info (t "Platform: windows-$arch" "平台: windows-$arch")
    
    Log-Step (t "Download" "下载")
    $installPath = Get-DefaultInstallDir
    Log-Info (t "Install to: $installPath" "安装到: $installPath")
    
    $filename = "$BINARY_NAME-windows-$arch.zip"
    $downloadUrl = "$REPO_URL/releases/download/$version/$filename"
    Log-Info $downloadUrl
    
    $tempDir = Join-Path $env:TEMP "ttm-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    $zipFile = Join-Path $tempDir $filename
    
    if (-not (Download-WithMirrors $downloadUrl $zipFile)) {
        Log-Error (t "Download failed" "下载失败")
        exit 1
    }
    
    Log-Step (t "Verify" "校验")
    $expectedSha = Get-RemoteSHA256 $downloadUrl
    if ($expectedSha) {
        if (-not (Verify-SHA256 $zipFile $expectedSha)) {
            if (-not $Force) {
                $reply = Read-Host (t "Checksum mismatch. Continue? (y/N)" "校验不匹配。是否继续? (y/N)")
                if ($reply -notmatch "^[yY]") {
                    Log-Error (t "Cancelled" "已取消")
                    exit 1
                }
            } else {
                Log-Warn (t "Force install despite checksum mismatch" "强制安装，忽略校验不匹配")
            }
        }
    } else {
        Log-Warn (t "Cannot fetch checksum, file integrity unknown" "无法获取校验信息，文件完整性未知")
        if (-not $Force) {
            $reply = Read-Host (t "Continue? (y/N)" "是否继续? (y/N)")
            if ($reply -notmatch "^[yY]") {
                Log-Error (t "Cancelled" "已取消")
                exit 1
            }
        }
    }
    
    Log-Step (t "Install" "安装")
    Install-Binary $zipFile $installPath
    
    # Cleanup
    if (Test-Path $tempDir) {
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    
    Write-Host ""
    Write-Host "  ✔ $(t 'Done!' '安装完成!')" -ForegroundColor Green -Bold
    Write-Host ""
}

Main

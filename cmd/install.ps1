# TTM Installer for Windows
# Usage:
#   irm https://raw.githubusercontent.com/vst93/ttm/main/cmd/install.ps1 | iex
#
# Options (via environment variables):
#   $env:SKIP_GITHUB=1       Skip GitHub direct download, use mirrors only
#   $env:FORCE_INSTALL=1     Force install on checksum fail
#   $env:INSTALL_DIR=path    Custom install directory
#   $env:TTM_LANG=zh         Set language (en/zh)
#   $env:NO_SHORTCUTS=1      Skip creating desktop/start menu shortcuts
#   $env:TTM_PREVIEW=1     Install latest pre-release version

$InstallDir = $env:INSTALL_DIR
$Force = $env:FORCE_INSTALL -eq '1'
$Lang = $env:TTM_LANG
$SkipGitHub = $env:SKIP_GITHUB -eq '1'
$NoShortcuts = $env:NO_SHORTCUTS -eq '1'
$Preview = $env:TTM_PREVIEW -eq '1'

$ErrorActionPreference = "Stop"

# Set console encoding to UTF-8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8

# UTF-8 helper
function U($bytes) {
    [System.Text.Encoding]::UTF8.GetString([byte[]]$bytes)
}

# Translation function: t("English", "中文")
function t($en, $zh) {
    if ($Lang -eq "zh") { return $zh } else { return $en }
}

# Chinese strings as UTF-8 byte arrays
$Z = @{
    LANG_NAME      = U(228,184,173,230,150,135)
    VERSION        = U(231,137,136,230,156,172)
    PLATFORM       = U(229,185,179,229,143,176)
    DOWNLOAD       = U(228,184,139,232,189,189)
    INSTALL        = U(229,174,137,232,163,133)
    VERIFY         = U(230,160,161,233,170,140)
    DONE           = U(229,174,137,232,163,133,229,174,140,230,136,144,33)
    CANCELLED      = U(229,183,178,229,143,150,230,182,136)
    FAILED         = U(229,164,177,232,180,165)
    LATEST_VER     = U(230,156,128,230,150,176,231,137,136,230,156,172,58,32)
    DL_FAILED      = U(228,184,139,232,189,189,229,164,177,232,180,165)
    ALL_DL_FAILED  = U(230,137,128,230,156,137,228,184,139,232,189,189,229,157,135,229,164,177,232,180,165)
    GITHUB_FAIL    = U(71,105,116,72,117,98,32,229,164,177,232,180,165,44,229,176,157,232,175,149,233,149,156,229,131,143,46,46,46)
    TRY_MIRROR     = U(229,176,157,232,175,149,233,149,156,229,131,143,58,32)
    SIZE           = U(229,164,167,229,176,143)
    DOWNLOADED     = U(229,183,178,228,184,139,232,189,189)
    SHA_OK         = U(230,160,161,233,170,140,32,79,75)
    SHA_MISMATCH   = U(230,160,161,233,170,140,228,184,141,229,140,185,233,133,141)
    NO_CHECKSUM    = U(230,151,160,230,179,149,232,142,183,229,143,150,230,160,161,233,170,140,228,191,161,230,129,175,239,188,140,230,150,135,228,187,182,229,174,140,230,149,180,230,128,167,230,156,170,231,159,165)
    PREVIEW_NONE   = U(230,156,170,230,137,190,229,136,176,233,162,132,232,167,136,231,137,136,230,156,172)
    CONTINUE       = U(230,152,175,229,144,166,231,187,167,231,187,173,239,188,159)
    ADDED_PATH     = U(229,183,178,230,183,187,229,138,160,229,136,176,80,65,84,72,40,233,135,141,229,144,175,231,187,136,231,171,175,231,148,159,230,149,136,41)
    EXTRACTING     = U(232,167,163,229,142,139,228,184,173)
    NO_BINARY      = U(229,142,139,231,188,169,229,140,133,228,184,173,230,156,170,230,137,190,229,136,176,231,168,139,229,186,143)
    INSTALLED      = U(229,183,178,229,174,137,232,163,133)
    FORCE_INSTALL  = U(229,188,186,229,136,182,229,174,137,232,163,133)
    TRYING         = U(229,176,157,232,175,149)
    CONNECTING     = U(232,191,158,230,142,165,228,184,173,46,46,46)
    UNINSTALL_NAME = U(84,84,77)
    UNINSTALL_INFO = U(84,84,77,32,229,183,178,230,179,168,229,134,140,229,136,176,229,141,184,232,189,189,229,136,151,232,161,168)
}

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

# Timeout settings (seconds)
$CONNECT_TIMEOUT = 10
$DOWNLOAD_TIMEOUT = 180

# ──────────────────────────────────────────────────────────────────────────────
# Language
# ──────────────────────────────────────────────────────────────────────────────

if (-not $Lang) { $Lang = $env:TTM_LANG }

function Select-Language {
    if ($Lang -and $Lang -match "^(en|zh)$") { return }
    
    Write-Host "  [1] English  [2] $($Z.LANG_NAME) (default: 1): " -NoNewline
    $choice = Read-Host
    switch ($choice) {
        "2" { $script:Lang = "zh" }
        "cn" { $script:Lang = "zh" }
        "zh" { $script:Lang = "zh" }
        default { $script:Lang = "en" }
    }
    $langName = if ($script:Lang -eq "zh") { $Z.LANG_NAME } else { "English" }
    Write-Host "  [OK] $langName" -ForegroundColor Green
}

# ──────────────────────────────────────────────────────────────────────────────
# Logging
# ──────────────────────────────────────────────────────────────────────────────

function Log-Info($msg) { Write-Host "  [OK] $msg" -ForegroundColor Green }
function Log-Warn($msg) { Write-Host "  [!] $msg" -ForegroundColor Yellow }
function Log-Error($msg) { Write-Host "  [X] $msg" -ForegroundColor Red }
function Log-Step($msg) { Write-Host "  >> $msg" -ForegroundColor Cyan }

# ──────────────────────────────────────────────────────────────────────────────
# Platform
# ──────────────────────────────────────────────────────────────────────────────

function Get-Platform {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default {
            Log-Error "Unsupported architecture: $arch"
            exit 1
        }
    }
}

# ──────────────────────────────────────────────────────────────────────────────
# Version
# ──────────────────────────────────────────────────────────────────────────────

function Get-LatestVersion {
    $latest_asset = "$BINARY_NAME-windows-amd64.zip"
    $latest_url = "$REPO_URL/releases/latest/download/$latest_asset"
    
    Log-Info "$(t 'Trying' $Z.TRYING) GitHub..."
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
            Log-Info "$(t 'Latest version: ' $Z.LATEST_VER)$version"
            return $version
        }
    } catch {
        Log-Warn "$(t 'GitHub failed, trying mirrors...' $Z.GITHUB_FAIL)"
    }
    
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
                Log-Info "$(t 'Latest version: ' $Z.LATEST_VER)$version"
                return $version
            }
        } catch {
            continue
        }
    }
    
    Log-Info "Trying jsdelivr..."
    try {
        $version_url = "https://cdn.jsdelivr.net/gh/$REPO_OWNER/$REPO_NAME@main/VERSION"
        $response = Invoke-WebRequest -Uri $version_url -UseBasicParsing -TimeoutSec 10
        $version = ($response.Content -replace '\s+', '').Trim()
        if ($version -match '^[0-9]+\.[0-9]+') {
            Log-Info "$(t 'Latest version: ' $Z.LATEST_VER)$version"
            return $version
        }
    } catch {
        Log-Warn "jsdelivr failed"
    }
    
    Log-Info "Trying API..."
    try {
        $response = Invoke-WebRequest -Uri $API_URL -UseBasicParsing -TimeoutSec 15
        $json = $response.Content | ConvertFrom-Json
        $version = $json.tag_name -replace '^v', ''
        Log-Info "$(t 'Latest version: ' $Z.LATEST_VER)$version"
        return $version
    } catch {
        Log-Error "$(t 'Failed' $Z.FAILED)"
        exit 1
    }
}

function Get-PreviewVersion {
    Log-Info "Trying API for pre-release..."
    try {
        $releasesUrl = "https://api.github.com/repos/$REPO_OWNER/$REPO_NAME/releases?per_page=10"
        $response = Invoke-WebRequest -Uri $releasesUrl -UseBasicParsing -TimeoutSec 15
        $json = $response.Content | ConvertFrom-Json
        $preview = $json | Where-Object { $_.prerelease -eq $true } | Select-Object -First 1
        if ($preview -and $preview.tag_name) {
            $version = $preview.tag_name
            Log-Info "$(t 'Preview version: ' $Z.LATEST_VER)$version"
            return $version
        }
        Log-Error "$(t 'No pre-release version found' $Z.PREVIEW_NONE)"
        exit 1
    } catch {
        Log-Error "$(t 'Failed' $Z.FAILED)"
        exit 1
    }
}

# ──────────────────────────────────────────────────────────────────────────────
# Download with Progress Bar
# ──────────────────────────────────────────────────────────────────────────────

function Format-FileSize($bytes) {
    if ($bytes -ge 1MB) { return "{0:N1} MB" -f ($bytes / 1MB) }
    if ($bytes -ge 1KB) { return "{0:N1} KB" -f ($bytes / 1KB) }
    return "$bytes B"
}

function Get-RemoteFileSize($url) {
    try {
        $request = [System.Net.WebRequest]::Create($url)
        $request.Method = "HEAD"
        $request.Timeout = 5000
        $response = $request.GetResponse()
        $size = $response.ContentLength
        $response.Close()
        return $size
    } catch {
        return 0
    }
}

function Show-ProgressBar($current, $total, $width = 40) {
    if ($total -gt 0) {
        $pct = [math]::Min(100, [math]::Round($current * 100 / $total))
    } else {
        $pct = 0
    }
    $filled = [math]::Round($pct * $width / 100)
    $empty = $width - $filled
    
    $bar = "[" + ("#" * $filled) + ("-" * $empty) + "]"
    $sizeStr = if ($total -gt 0) { "{0}/{1}" -f (Format-FileSize $current), (Format-FileSize $total) } else { Format-FileSize $current }
    $line = "`r  $bar {0,3}%  $sizeStr" -f $pct
    Write-Host $line -NoNewline
}

function Download-File($url, $output) {
    $filename = Split-Path $url -Leaf
    
    # Get remote file size
    $totalSize = Get-RemoteFileSize $url
    $sizeText = if ($totalSize -gt 0) { " ($(Format-FileSize $totalSize))" } else { "" }
    Write-Host "  $filename$sizeText" -ForegroundColor DarkGray
    
    try {
        # Create HttpWebRequest with timeout
        $request = [System.Net.HttpWebRequest]::Create($url)
        $request.Timeout = $CONNECT_TIMEOUT * 1000
        $request.ReadWriteTimeout = $DOWNLOAD_TIMEOUT * 1000
        $request.AllowAutoRedirect = $true
        
        # Get response
        $response = $request.GetResponse()
        $totalBytes = $response.ContentLength
        
        # Read stream with progress
        $stream = $response.GetResponseStream()
        $fileStream = [System.IO.File]::Create($output)
        $buffer = New-Object byte[] 8192
        $totalRead = 0
        $lastProgressUpdate = [DateTime]::MinValue
        $startTime = [DateTime]::Now
        
        while ($true) {
            $bytesRead = $stream.Read($buffer, 0, $buffer.Length)
            if ($bytesRead -eq 0) { break }
            
            $fileStream.Write($buffer, 0, $bytesRead)
            $totalRead += $bytesRead
            
            # Update progress every 200ms
            $now = [DateTime]::Now
            if (($now - $lastProgressUpdate).TotalMilliseconds -gt 200) {
                Show-ProgressBar $totalRead $totalBytes
                $lastProgressUpdate = $now
                
                # Check timeout
                if (($now - $startTime).TotalSeconds -gt $DOWNLOAD_TIMEOUT) {
                    throw "Download timeout"
                }
            }
        }
        
        # Final progress update
        Show-ProgressBar $totalRead $totalBytes
        
        $fileStream.Close()
        $stream.Close()
        $response.Close()
        
        # Clear progress line
        Write-Host "`r" -NoNewline
        Write-Host (" " * 80) -NoNewline
        Write-Host "`r" -NoNewline
        
        if (Test-Path $output) {
            $size = (Get-Item $output).Length
            $sizeText = Format-FileSize $size
            Log-Info "$(t 'Downloaded' $Z.DOWNLOADED): $filename ($sizeText)"
            return $true
        }
    } catch {
        # Cleanup on error
        Write-Host ""
        if ($fileStream) { $fileStream.Close() }
        if ($stream) { $stream.Close() }
        if ($response) { $response.Close() }
        Log-Warn "Download failed: $($_.Exception.Message)"
    }
    return $false
}

function Download-WithMirrors($url, $output) {
    if (-not $SkipGitHub) {
        Log-Info "$(t 'Trying direct...' '尝试直连...')"
        if (Download-File $url $output) { return $true }
    }
    
    foreach ($mirror in $GITHUB_MIRRORS) {
        $mirror_url = "$mirror/$url"
        Log-Warn "$(t 'Trying mirror: ' $Z.TRY_MIRROR)$mirror"
        if (Download-File $mirror_url $output) { return $true }
    }
    
    Log-Error "$(t 'All downloads failed' $Z.ALL_DL_FAILED)"
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
    $sha_url = "$url.sha256"
    $sha_file = Join-Path $env:TEMP "ttm-sha256-$(Get-Random).txt"
    
    try {
        if (Download-WithMirrors $sha_url $sha_file) {
            $content = Get-Content -Path $sha_file -Raw
            $content = $content -replace '\r?\n', ' '
            if ($content -match '([a-f0-9]{64})') {
                return $matches[1].ToLower()
            }
        }
    } catch {} finally {
        if (Test-Path $sha_file) {
            Remove-Item -Path $sha_file -Force -ErrorAction SilentlyContinue
        }
    }
    return $null
}

function Verify-SHA256($file, $expected) {
    $actual = Get-SHA256 $file
    if (-not $actual) {
        Log-Warn "SHA256 tool not available, skipping"
        return $true
    }
    if ($actual -ne $expected) {
        Log-Error "$(t 'SHA256 mismatch' $Z.SHA_MISMATCH)"
        Log-Error "Expected: $expected"
        Log-Error "Actual:   $actual"
        return $false
    }
    Log-Info "$(t 'SHA256 OK' $Z.SHA_OK)"
    return $true
}

# ──────────────────────────────────────────────────────────────────────────────
# Windows App Registration (Add/Remove Programs + Start Menu)
# ──────────────────────────────────────────────────────────────────────────────

function Create-Shortcuts($installDir) {
    if ($NoShortcuts) { return }
    
    $exePath = Join-Path $installDir "$BINARY_NAME.exe"
    $shell = New-Object -ComObject WScript.Shell
    
    # Start Menu shortcut
    try {
        $startMenuDir = [Environment]::GetFolderPath('Programs')
        $shortcutDir = Join-Path $startMenuDir "TTM"
        
        if (-not (Test-Path $shortcutDir)) {
            New-Item -ItemType Directory -Path $shortcutDir -Force | Out-Null
        }
        
        $shortcutPath = Join-Path $shortcutDir "TTM.lnk"
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath = $exePath
        $shortcut.WorkingDirectory = $installDir
        $shortcut.Description = "TTM - SSH Terminal Manager"
        $shortcut.Save()
        
        Log-Info "$(t 'Created Start Menu shortcut' $(U(229,183,178,229,136,155,229,187,186,229,188,128,229,167,139,232,143,156,229,141,149,229,191,171,230,141,183,230,150,185,229,188,143)))"
    } catch {
        Log-Warn "Failed to create Start Menu shortcut: $($_.Exception.Message)"
    }
    
    # Desktop shortcut
    try {
        $desktopDir = [Environment]::GetFolderPath('Desktop')
        $shortcutPath = Join-Path $desktopDir "TTM.lnk"
        
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath = $exePath
        $shortcut.WorkingDirectory = $installDir
        $shortcut.Description = "TTM - SSH Terminal Manager"
        $shortcut.Save()
        
        Log-Info "$(t 'Created desktop shortcut' $(U(229,183,178,229,136,155,229,187,186,230,161,140,233,157,162,229,191,171,230,141,183,230,150,185,229,188,143)))"
    } catch {
        Log-Warn "Failed to create desktop shortcut: $($_.Exception.Message)"
    }
}

function Register-App($installDir) {
    $regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$BINARY_NAME"
    
    try {
        # Get version
        $exePath = Join-Path $installDir "$BINARY_NAME.exe"
        $version = & $exePath --version 2>$null
        if (-not $version) { $version = "1.0.0" }
        
        # Create registry entries
        New-Item -Path $regPath -Force | Out-Null
        Set-ItemProperty -Path $regPath -Name "DisplayName" -Value "$(t 'TTM' $Z.UNINSTALL_NAME) - SSH $(t 'Download' $Z.DOWNLOAD)"
        Set-ItemProperty -Path $regPath -Name "DisplayVersion" -Value $version
        Set-ItemProperty -Path $regPath -Name "Publisher" -Value "vst93"
        Set-ItemProperty -Path $regPath -Name "DisplayIcon" -Value "$exePath,0"
        Set-ItemProperty -Path $regPath -Name "UninstallString" -Value "powershell.exe -Command `"& { Remove-Item -Path '$regPath' -Force; Remove-Item -Path '$installDir' -Recurse -Force }`""
        Set-ItemProperty -Path $regPath -Name "InstallLocation" -Value $installDir
        Set-ItemProperty -Path $regPath -Name "NoModify" -Value 1 -Type DWord
        Set-ItemProperty -Path $regPath -Name "NoRepair" -Value 1 -Type DWord
        Set-ItemProperty -Path $regPath -Name "EstimatedSize" -Value ([math]::Round((Get-Item $exePath).Length / 1KB)) -Type DWord
        
        Log-Info "$(t 'Registered in Apps' $Z.UNINSTALL_INFO)"
    } catch {
        Log-Warn "Failed to register app: $($_.Exception.Message)"
    }
}

# ──────────────────────────────────────────────────────────────────────────────
# Installation
# ──────────────────────────────────────────────────────────────────────────────

function Get-DefaultInstallDir {
    if ($InstallDir) { return $InstallDir }
    return Join-Path $env:LOCALAPPDATA "Programs\ttm"
}

function Add-ToPath($dir) {
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -split ";" -contains $dir) { return }
    
    Log-Warn "$(t 'Added to PATH (restart terminal to take effect)' $Z.ADDED_PATH)"
    [Environment]::SetEnvironmentVariable("Path", "$currentPath;$dir", "User")
    $env:Path = "$env:Path;$dir"
}

function Install-Binary($zipFile, $installDir) {
    $extractDir = Join-Path $env:TEMP "ttm-extract-$(Get-Random)"
    
    try {
        Log-Info "$(t 'Extracting' $Z.EXTRACTING)"
        Expand-Archive -Path $zipFile -DestinationPath $extractDir -Force
        
        $exePath = Join-Path $extractDir "$BINARY_NAME.exe"
        if (-not (Test-Path $exePath)) {
            Log-Error "$(t 'Binary not found in archive' $Z.NO_BINARY)"
            exit 1
        }
        
        if (-not (Test-Path $installDir)) {
            New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        }
        
        $destPath = Join-Path $installDir "$BINARY_NAME.exe"
        Copy-Item -Path $exePath -Destination $destPath -Force
        
        Log-Info "$(t 'Installed' $Z.INSTALLED): $destPath"
        & $destPath --version 2>$null
        
        # Add to PATH
        Add-ToPath $installDir
        
        # Register in Windows Apps
        Register-App $installDir
        
        # Create shortcuts
        Create-Shortcuts $installDir
        
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
    
    Write-Host ""
    Write-Host "  >> TTM Installer" -ForegroundColor Cyan
    Write-Host ""
    
    Log-Step "$(t 'Version' $Z.VERSION)"
    if ($Preview) {
        $version = Get-PreviewVersion
    } else {
        $version = Get-LatestVersion
    }
    
    Log-Step "$(t 'Platform' $Z.PLATFORM)"
    $arch = Get-Platform
    Log-Info "Platform: windows-$arch"
    
    Log-Step "$(t 'Download' $Z.DOWNLOAD)"
    $installPath = Get-DefaultInstallDir
    Log-Info "$(t 'Install to' $Z.INSTALL): $installPath"
    
    $filename = "$BINARY_NAME-windows-$arch.zip"
    $downloadUrl = "$REPO_URL/releases/download/$version/$filename"
    Write-Host "  URL: $downloadUrl" -ForegroundColor DarkGray
    
    $tempDir = Join-Path $env:TEMP "ttm-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    $zipFile = Join-Path $tempDir $filename
    
    if (-not (Download-WithMirrors $downloadUrl $zipFile)) {
        Log-Error "$(t 'Download failed' $Z.DL_FAILED)"
        exit 1
    }
    
    Log-Step "$(t 'Verify' $Z.VERIFY)"
    $expectedSha = Get-RemoteSHA256 $downloadUrl
    if ($expectedSha) {
        if (-not (Verify-SHA256 $zipFile $expectedSha)) {
            if (-not $Force) {
                $reply = Read-Host "$(t 'SHA256 mismatch' $Z.SHA_MISMATCH). $(t 'Continue?' $Z.CONTINUE) (y/N)"
                if ($reply -notmatch "^[yY]") {
                    Log-Error "$(t 'Cancelled' $Z.CANCELLED)"
                    exit 1
                }
            } else {
                Log-Warn "$(t 'Force install' $Z.FORCE_INSTALL)"
            }
        }
    } else {
        Log-Warn "$(t 'Cannot fetch checksum, file integrity unknown' $Z.NO_CHECKSUM)"
        if (-not $Force) {
            $reply = Read-Host "$(t 'Continue?' $Z.CONTINUE) (y/N)"
            if ($reply -notmatch "^[yY]") {
                Log-Error "$(t 'Cancelled' $Z.CANCELLED)"
                exit 1
            }
        }
    }
    
    Log-Step "$(t 'Install' $Z.INSTALL)"
    Install-Binary $zipFile $installPath
    
    if (Test-Path $tempDir) {
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    
    Write-Host ""
    Write-Host "  [OK] $(t 'Done!' $Z.DONE)" -ForegroundColor Green
    Write-Host ""
}

Main

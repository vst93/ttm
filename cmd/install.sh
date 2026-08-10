#!/usr/bin/env bash

set -euo pipefail

REPO_OWNER="vst93"
REPO_NAME="ttm"
BINARY_NAME="ttm"
REPO_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}"
API_URL="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
FORCE_INSTALL="${FORCE_INSTALL:-0}"
SKIP_GITHUB="${SKIP_GITHUB:-0}"
AUTO_DELETE_INSTALL_SCRIPT="${AUTO_DELETE_INSTALL_SCRIPT:-1}"
PREVIEW="${TTM_PREVIEW:-0}"

GITHUB_MIRRORS=(
    "https://ghfast.top"
    "https://mirror.ghproxy.com"
    "https://gh-proxy.com"
    "https://gh-proxy.net"
)

LANG="${TTM_LANG:-}"
TEMP_DIR=""
SCRIPT_PATH="${BASH_SOURCE[0]:-}"

# ──────────────────────────────────────────────────────────────────────────────
# Colors
# ──────────────────────────────────────────────────────────────────────────────

if [ -t 1 ] && [ "${NO_COLOR:-}" = "" ] && [ "${TERM:-dumb}" != "dumb" ]; then
    R='\033[0m' B='\033[1m' D='\033[2m'
    RED='\033[31m' GRN='\033[32m' YLW='\033[33m' CYN='\033[36m'
else
    R='' B='' D='' RED='' GRN='' YLW='' CYN=''
fi

# ──────────────────────────────────────────────────────────────────────────────
# Translation
# ──────────────────────────────────────────────────────────────────────────────

t() { [ "$LANG" = "zh" ] && printf '%s' "$2" || printf '%s' "$1"; }

# ──────────────────────────────────────────────────────────────────────────────
# Logging
# ──────────────────────────────────────────────────────────────────────────────

log_info()  { printf "${GRN}✓${R} %s\n" "$*"; }
log_warn()  { printf "${YLW}⚠${R} %s\n" "$*" >&2; }
log_error() { printf "${RED}✗${R} %s\n" "$*" >&2; }
log_step()  { printf "${CYN}${B}▸ %s${R}\n" "$*"; }

# ──────────────────────────────────────────────────────────────────────────────
# Spinner (animated progress indicator)
# ──────────────────────────────────────────────────────────────────────────────

SPINNER_PID=""
SPINNER_FRAMES=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")

spinner_start() {
    local msg="$1"
    local i=0
    # Run spinner in background (output to stderr to avoid polluting stdout)
    (
        while true; do
            printf "\r  ${CYN}%s${R} %s" "${SPINNER_FRAMES[i]}" "$msg" >&2
            i=$(( (i + 1) % ${#SPINNER_FRAMES[@]} ))
            sleep 0.1
        done
    ) &
    SPINNER_PID=$!
}

spinner_stop() {
    if [ -n "$SPINNER_PID" ]; then
        kill "$SPINNER_PID" 2>/dev/null || true
        wait "$SPINNER_PID" 2>/dev/null || true
        SPINNER_PID=""
    fi
    # Only clear line if stderr is a terminal
    if [ -t 2 ]; then
        printf "\r\033[K" >&2
    fi
}

# ──────────────────────────────────────────────────────────────────────────────
# Language selection
# ──────────────────────────────────────────────────────────────────────────────

select_language() {
    if [ -n "$LANG" ]; then
        case "$LANG" in en|zh) return 0 ;; esac
    fi
    if ! is_interactive; then
        LANG="en"
        return 0
    fi
    printf "${B}  [1]${R} English  ${B}[2]${R} 中文 ${D}(default: 1)${R}: "
    local choice
    read -r choice
    case "${choice:-1}" in
        1|en) LANG="en" ;;
        2|zh|cn) LANG="zh" ;;
        *) LANG="en" ;;
    esac
    log_info "$(t "Language: English" "语言: 中文")"
}

# ──────────────────────────────────────────────────────────────────────────────
# Core utilities
# ──────────────────────────────────────────────────────────────────────────────

cleanup() {
    spinner_stop
    [ -n "$TEMP_DIR" ] && rm -rf "$TEMP_DIR"
    if should_self_delete_script; then
        rm -f "$SCRIPT_PATH" || true
    fi
}
trap cleanup EXIT

is_termux() {
    [ -n "${TERMUX_VERSION:-}" ] || [ "${PREFIX:-}" = "/data/data/com.termux/files/usr" ] || [ -d "/data/data/com.termux/files/usr" ]
}
is_interactive() { [ -t 0 ]; }
has_cmd() { command -v "$1" >/dev/null 2>&1; }

should_self_delete_script() {
    [ "$AUTO_DELETE_INSTALL_SCRIPT" != "1" ] && return 1
    [ -z "$SCRIPT_PATH" ] || [ ! -f "$SCRIPT_PATH" ] && return 1
    local script_name
    script_name="$(basename "$SCRIPT_PATH")"
    [ "$script_name" != "install.sh" ] && return 1
    case "$SCRIPT_PATH" in cmd/install.sh|*/cmd/install.sh) return 1 ;; esac
    return 0
}

print_help() {
    cat <<EOF
${B}Usage:${R} ${CYN}${BINARY_NAME}-install${R} ${GRN}[OPTIONS]${R}

$(t "Install latest ${BINARY_NAME} release." "安装最新版本的 ${BINARY_NAME}。")

${B}Options:${R}
  ${GRN}-h, --help${R}              $(t "Show help" "显示帮助")
  ${GRN}    --install-dir${R} <dir> $(t "Override install dir" "覆盖安装目录")
  ${GRN}    --force${R}             $(t "Force install on checksum fail" "校验失败时强制安装")
  ${GRN}    --lang${R} <en|zh>      $(t "Set language" "设置语言")
  ${GRN}    --skip-github${R}       $(t "Skip GitHub direct download, use mirrors only" "跳过 GitHub 直连下载，仅使用镜像")
  ${GRN}    --preview${R}            $(t "Install latest pre-release version" "安装预览版")

${B}Env vars:${R}
  ${YLW}INSTALL_DIR${R}             $(t "Same as --install-dir" "等同于 --install-dir")
  ${YLW}FORCE_INSTALL${R}=1         $(t "Same as --force" "等同于 --force")
  ${YLW}SKIP_GITHUB${R}=1           $(t "Same as --skip-github" "等同于 --skip-github")
  ${YLW}TTM_LANG${R}=en|zh          $(t "Set language" "设置语言")
  ${YLW}TTM_PREVIEW${R}=1           $(t "Same as --preview" "等同于 --preview")
EOF
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            -h|--help) print_help; exit 0 ;;
            --install-dir)
                [ "$#" -lt 2 ] && { log_error "$(t "--install-dir requires argument" "--install-dir 需要参数")"; exit 2; }
                INSTALL_DIR="$2"; shift ;;
            --force) FORCE_INSTALL="1" ;;
            --skip-github) SKIP_GITHUB="1" ;;
            --preview) PREVIEW="1" ;;
            --lang)
                [ "$#" -lt 2 ] && { log_error "$(t "--lang requires value: en|zh" "--lang 需要值: en|zh")"; exit 2; }
                case "$2" in en|zh) LANG="$2" ;; *) log_error "$(t "Invalid: $2" "无效: $2")"; exit 2 ;; esac
                shift ;;
            *)
                log_error "$(t "Unknown option: $1" "未知参数: $1")"
                log_error "$(t "Available: --help, --install-dir, --force, --skip-github, --lang, --preview" "可用: --help, --install-dir, --force, --skip-github, --lang, --preview")"
                exit 2 ;;
        esac
        shift
    done
}

init_temp_dir() { TEMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t ttm)"; }

require_download_tool() {
    has_cmd curl || has_cmd wget || {
        log_error "$(t "curl or wget required" "需要 curl 或 wget")"
        exit 1
    }
}

require_extract_tool() {
    has_cmd unzip || {
        log_error "$(t "unzip required" "需要 unzip")"
        is_termux && log_info "Termux: pkg install unzip"
        exit 1
    }
}

# Get file size from URL (HEAD request)
get_remote_size() {
    local url="$1"
    if has_cmd curl; then
        curl -fsSLI --connect-timeout 5 "$url" 2>/dev/null | grep -i content-length | tail -1 | awk '{print $2}' | tr -d '\r'
    elif has_cmd wget; then
        wget --spider --timeout=5 "$url" 2>&1 | grep -i 'length:' | awk '{print $2}'
    fi
}

# Format bytes to human readable
format_size() {
    local bytes=$1
    if [ "$bytes" -ge 1048576 ]; then
        printf '%.1fMB' "$(echo "$bytes / 1048576" | bc -l)"
    elif [ "$bytes" -ge 1024 ]; then
        printf '%.1fKB' "$(echo "$bytes / 1024" | bc -l)"
    else
        printf '%dB' "$bytes"
    fi
}

# Thin progress bar
# Usage: progress_bar <current> <total>
progress_bar() {
    local current=$1 total=$2
    local width=40
    local pct=0 filled
    if [ "$total" -gt 0 ] 2>/dev/null; then
        pct=$(( current * 100 / total ))
    fi
    [ "$pct" -gt 100 ] && pct=100
    filled=$(( pct * width / 100 ))

    local bar=""
    local i
    for i in $(seq 1 $width); do
        if [ $i -le $filled ]; then
            bar+="━"
        else
            bar+="─"
        fi
    done

    # Move to beginning of line and print
    printf '\r  %s %3d%%' "$bar" "$pct" >&2
}

# Download with thin progress bar
download_file() {
    local url="$1" output_file="$2"
    local filename
    filename="$(basename "$url")"

    local total_size
    total_size="$(get_remote_size "$url")"
    total_size="${total_size:-0}"

    local size_text=""
    if [ "$total_size" -gt 0 ] 2>/dev/null; then
        size_text=" ($(format_size "$total_size"))"
    fi

    printf "  ${D}%s${R}%s\n" "$filename" "$size_text" >&2

    local ret=0
    if has_cmd curl; then
        # Start download in background (no retry since we have mirror fallback)
        curl -fSL --connect-timeout 5 --max-time 180 \
            -o "$output_file" "$url" >/dev/null 2>&1 &
        local dl_pid=$!

        # Show spinner while connecting (file is 0 bytes)
        spinner_start "$(t "Connecting..." "连接中...")"
        while kill -0 "$dl_pid" 2>/dev/null; do
            local current_size=0
            [ -f "$output_file" ] && current_size=$(wc -c < "$output_file" 2>/dev/null || echo 0)
            if [ "$current_size" -gt 0 ] 2>/dev/null; then
                break  # Download started, switch to progress bar
            fi
            sleep 0.2
        done
        spinner_stop

        # Download in progress, show progress bar
        while kill -0 "$dl_pid" 2>/dev/null; do
            if [ -f "$output_file" ]; then
                local current_size
                current_size=$(wc -c < "$output_file" 2>/dev/null || echo 0)
                progress_bar "$current_size" "$total_size"
            fi
            sleep 0.2
        done
        wait "$dl_pid" 2>/dev/null || ret=$?
        # Clear progress line
        if [ -t 2 ]; then
            printf '\r\033[K' >&2
        fi
    elif has_cmd wget; then
        wget --timeout=180 -O "$output_file" "$url" >/dev/null 2>&1 || ret=$?
    fi

    if [ $ret -ne 0 ] || [ ! -s "$output_file" ]; then
        log_error "$(t "Download failed: $filename" "下载失败: $filename")" >&2
        return 1
    fi

    local size
    size=$(ls -lh "$output_file" | awk '{print $5}')
    log_info "$(t "Downloaded: $filename ($size)" "已下载: $filename ($size)")" >&2
    return 0
}

# Download with mirror fallback
download_with_mirrors() {
    local original_url="$1" output_file="$2"

    if [ "$SKIP_GITHUB" != "1" ]; then
        log_info "$(t "Trying direct..." "尝试直连...")" >&2
        download_file "$original_url" "$output_file" && return 0
    fi

    for mirror in "${GITHUB_MIRRORS[@]}"; do
        log_warn "$(t "Trying mirror: $mirror" "尝试镜像: $mirror")" >&2
        download_file "${mirror}/${original_url}" "$output_file" && return 0
    done

    log_error "$(t "All downloads failed" "所有下载均失败")" >&2
    return 1
}

fetch_latest_version() {
    local response version

    # Method: Get version from releases redirect URL
    # GitHub redirects /releases/latest/download/FILE to /releases/download/vX.Y.Z/FILE
    # Mirrors that support redirects will show the version in the Location header
    local latest_asset="${BINARY_NAME}-linux-amd64.zip"
    local latest_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download/${latest_asset}"

    # Try direct GitHub first, then mirrors as fallback
    local try_urls=("${latest_url}")
    for mirror in "${GITHUB_MIRRORS[@]}"; do
        try_urls+=("${mirror}/${latest_url}")
    done

    local total=${#try_urls[@]}
    local i=0
    for url in "${try_urls[@]}"; do
        i=$((i + 1))
        local label
        if [[ "$url" == *ghfast.top* ]]; then
            label="ghfast"
        elif [[ "$url" == *ghproxy* ]]; then
            label="ghproxy"
        elif [[ "$url" == *gh-proxy.com* ]]; then
            label="gh-proxy"
        elif [[ "$url" == *gh-proxy.net* ]]; then
            label="gh-proxy.net"
        elif [[ "$url" == *github.com* ]]; then
            label="GitHub"
        else
            label="Mirror"
        fi
        spinner_start "$(t "Version ($label)" "版本 ($label)")"

        # HEAD request to get redirect Location header
        local location=""
        if has_cmd curl; then
            # Use -I (HEAD) without -L to get first redirect only
            location="$(curl -fsSL --connect-timeout 8 --max-time 15 -I "$url" 2>/dev/null | grep -i '^location:' | head -1)" || true
        fi

        if [ -n "$location" ]; then
            # Parse version: ...download/v1.2.3/file.zip or ...download/1.2.3/file.zip -> 1.2.3
            version="$(printf '%s' "$location" | grep -oE '/download/v?[0-9]+\.[0-9]+[0-9.]*' | sed 's|/download/||;s|^v||' | head -n 1)"
            if [ -n "$version" ]; then
                VERSION="$version"
                spinner_stop
                log_info "$(t "Latest version: $VERSION" "最新版本: $VERSION")"
                return 0
            fi
        fi

        spinner_stop
        log_warn "$(t "$label failed" "$label 失败")" >&2
    done

    # Fallback: Try jsdelivr VERSION file
    spinner_start "$(t "Version (jsdelivr)" "版本 (jsdelivr)")"
    response=""
    if has_cmd curl; then
        response="$(curl -fsSL --connect-timeout 8 --max-time 10 "https://cdn.jsdelivr.net/gh/${REPO_OWNER}/${REPO_NAME}@main/VERSION" 2>/dev/null)" || true
    fi
    if [ -n "$response" ]; then
        version="$(printf '%s' "$response" | tr -d '[:space:]' | grep -E '^[0-9]+\.[0-9]+' | head -n 1)"
        if [ -n "$version" ]; then
            VERSION="$version"
            spinner_stop
            log_info "$(t "Latest version: $VERSION" "最新版本: $VERSION")"
            return 0
        fi
    fi
    spinner_stop

    # Last resort: GitHub API
    spinner_start "$(t "Version (API)" "版本 (API)")"
    response=""
    if has_cmd curl; then
        response="$(curl -fsSL --connect-timeout 8 --max-time 15 "$API_URL" 2>/dev/null)" || true
    fi
    if [ -n "$response" ]; then
        version="$(printf '%s\n' "$response" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"v[^"]*"' | sed 's/.*"v//' | sed 's/".*//' | head -n 1)"
        if [ -n "$version" ]; then
            VERSION="$version"
            spinner_stop
            log_info "$(t "Latest version: $VERSION" "最新版本: $VERSION")"
            return 0
        fi
    fi
    spinner_stop

    log_error "$(t "Failed to get version" "获取版本失败")"
    return 1
}

fetch_preview_version() {
    local response version
    local releases_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases?per_page=10"

    spinner_start "$(t "Version (preview API)" "版本 (预览 API)")"
    response=""
    if has_cmd curl; then
        response="$(curl -fsSL --connect-timeout 8 --max-time 15 "$releases_url" 2>/dev/null)" || true
    elif has_cmd wget; then
        response="$(wget -qO- --timeout=15 "$releases_url" 2>/dev/null)" || true
    fi

    if [ -n "$response" ]; then
        if has_cmd jq; then
            version="$(printf '%s\n' "$response" | jq -r 'first(.[] | select(.prerelease == true) | .tag_name) // empty' 2>/dev/null)"
        elif has_cmd python3; then
            version="$(printf '%s\n' "$response" | python3 -c "import sys,json; rs=json.load(sys.stdin); print(next((r['tag_name'] for r in rs if r.get('prerelease')), ''))" 2>/dev/null)"
        else
            # Fallback without jq: releases are newest-first; print the tag_name of the
            # first release whose prerelease flag is true (tag_name precedes prerelease)
            version="$(printf '%s\n' "$response" | awk '
                /"tag_name"[[:space:]]*:/ { tag=$0; sub(/.*"tag_name"[[:space:]]*:[[:space:]]*"/, "", tag); sub(/"[^"]*$/, "", tag) }
                /"prerelease"[[:space:]]*:[[:space:]]*true/ { print tag; exit }
            ')"
        fi
        if [ -n "$version" ]; then
            VERSION="$version"
            spinner_stop
            log_info "$(t "Preview version: $VERSION" "预览版本: $VERSION")"
            return 0
        fi
    fi
    spinner_stop

    log_error "$(t "No pre-release version found" "未找到预览版本")"
    return 1
}

detect_platform() {
    local uname_s uname_m
    uname_s="$(uname -s)"
    uname_m="$(uname -m)"

    case "$uname_s" in
        Darwin) OS="darwin" ;;
        Linux)  is_termux && OS="android" || OS="linux" ;;
        *)      log_error "$(t "Unsupported OS: $uname_s" "不支持: $uname_s")"; exit 1 ;;
    esac

    case "$uname_m" in
        x86_64|amd64)    ARCH="amd64" ;;
        aarch64|arm64)   ARCH="arm64" ;;
        armv7l|armv8l|arm)
            log_error "$(t "32-bit ARM not supported" "不支持 32 位 ARM")"; exit 1 ;;
        *)  log_error "$(t "Unsupported arch: $uname_m" "不支持架构: $uname_m")"; exit 1 ;;
    esac

    log_info "$(t "Platform: ${OS}-${ARCH}" "平台: ${OS}-${ARCH}")"
}

get_download_info() {
    case "${OS}-${ARCH}" in
        darwin-arm64)  FILENAME="ttm-darwin-arm64.zip" ;;
        darwin-amd64)  FILENAME="ttm-darwin-amd64.zip" ;;
        linux-arm64)   FILENAME="ttm-linux-arm64.zip" ;;
        linux-amd64)   FILENAME="ttm-linux-amd64.zip" ;;
        android-arm64) FILENAME="ttm-android-arm64.zip" ;;
        android-amd64) FILENAME="ttm-android-amd64.zip" ;;
        *) log_error "$(t "No package for ${OS}-${ARCH}" "无适用于 ${OS}-${ARCH} 的包")"; exit 1 ;;
    esac
    DOWNLOAD_URL="${REPO_URL}/releases/download/${VERSION}/${FILENAME}"
}

get_sha256_hash() {
    local sha256_url="$1" sha_file="$TEMP_DIR/${FILENAME}.sha256"
    download_with_mirrors "$sha256_url" "$sha_file"
    # Remove \r (Windows line endings) and extract hash
    awk '{print $1}' "$sha_file" | tr -d '\r\n'
}

compute_sha256() {
    local file="$1"
    if has_cmd sha256sum; then sha256sum "$file" | awk '{print $1}'
    elif has_cmd shasum; then shasum -a 256 "$file" | awk '{print $1}'
    elif has_cmd openssl; then openssl dgst -sha256 "$file" | awk '{print $NF}'
    else return 1
    fi
}

verify_sha256() {
    local file="$1" expected_sha="$2" actual_sha
    if ! actual_sha="$(compute_sha256 "$file")"; then
        log_warn "$(t "No SHA256 tool, skipping" "无 SHA256 工具，跳过")"
        return 0
    fi
    if [ "$actual_sha" != "$expected_sha" ]; then
        log_error "$(t "SHA256 mismatch" "SHA256 不匹配")"
        log_error "Expected: $expected_sha"
        log_error "Actual:   $actual_sha"
        return 1
    fi
    log_info "$(t "SHA256 OK" "SHA256 校验通过")"
}

prompt_continue_or_abort() {
    local reason="$1"
    if [ "$FORCE_INSTALL" = "1" ]; then
        log_warn "$reason (FORCE_INSTALL=1)"
        return 0
    fi
    if is_interactive; then
        # Drain stdin buffer completely (clear any buffered input from download progress)
        # Multiple approaches to ensure all buffered input is discarded
        
        # Method 1: Read all available single characters with short timeout
        while IFS= read -r -t 0.1 -n 1 _ 2>/dev/null; do :; done
        
        # Method 2: If stty available, use it to flush terminal input buffer
        if has_cmd stty && [ -t 0 ]; then
            local old_tty
            old_tty="$(stty -g 2>/dev/null || true)"
            stty -echo -icanon min 0 time 1 2>/dev/null || true
            dd bs=1 count=100 of=/dev/null 2>/dev/null || true
            [ -n "$old_tty" ] && stty "$old_tty" 2>/dev/null || true
        fi

        printf "\n  ${YLW}⚠${R} %s\n" "$reason"
        printf "  ${D}%s${R} " "$(t "Continue installation? (y/N)" "是否继续安装? (y/N)")"
        read -r reply
        case "$reply" in y|Y|yes|YES) return 0 ;; esac
        log_error "$(t "Cancelled" "已取消")"; exit 1
    fi
    log_error "$(t "$reason (set FORCE_INSTALL=1 to force)" "$reason (设置 FORCE_INSTALL=1 强制)")"
    exit 1
}

determine_install_dir() {
    [ -n "${INSTALL_DIR:-}" ] && { printf '%s\n' "$INSTALL_DIR"; return; }
    is_termux && [ -n "${PREFIX:-}" ] && { printf '%s\n' "$PREFIX/bin"; return; }
    [ -d "/usr/local/bin" ] && { [ -w "/usr/local/bin" ] || { ! is_termux && has_cmd sudo; }; } && {
        printf '%s\n' "/usr/local/bin"; return
    }
    [ -n "${HOME:-}" ] && { printf '%s\n' "$HOME/.local/bin"; return; }
    log_error "$(t "Cannot determine install dir" "无法确定安装目录")"
    exit 1
}

ensure_dir_exists() {
    local dir="$1"
    [ -d "$dir" ] && return 0
    mkdir -p "$dir" 2>/dev/null && return 0
    if is_termux; then
        log_error "$(t "Cannot create: $dir" "无法创建: $dir")"; exit 1
    fi
    has_cmd sudo && { sudo mkdir -p "$dir" || { log_error "sudo mkdir failed: $dir"; exit 1; }; return 0; }
    log_error "$(t "Cannot create dir, no sudo" "无法创建目录且无 sudo")"
    exit 1
}

install_binary() {
    local zip_file="$1" install_dir="$2" extracted_dir="$TEMP_DIR/extracted" binary_path
    mkdir -p "$extracted_dir"

    spinner_start "$(t "Extracting" "解压中")"
    unzip -q "$zip_file" -d "$extracted_dir"
    spinner_stop
    log_info "$(t "Extracted" "解压完成")"

    binary_path="$extracted_dir/$BINARY_NAME"
    [ -f "$binary_path" ] || { log_error "$(t "Binary not found in archive" "压缩包中未找到程序")"; exit 1; }

    chmod +x "$binary_path"
    ensure_dir_exists "$install_dir"

    if [ -w "$install_dir" ]; then
        spinner_start "$(t "Installing to $install_dir" "安装到 $install_dir")"
        if has_cmd install; then
            install -m 0755 "$binary_path" "$install_dir/$BINARY_NAME"
        else
            cp "$binary_path" "$install_dir/$BINARY_NAME"
            chmod 0755 "$install_dir/$BINARY_NAME"
        fi
        spinner_stop
    elif ! is_termux && has_cmd sudo; then
        # Stop spinner before sudo so password prompt displays cleanly
        log_info "$(t "Need sudo to install to $install_dir" "需要 sudo 权限安装到 $install_dir")"
        if has_cmd install; then
            sudo install -m 0755 "$binary_path" "$install_dir/$BINARY_NAME" || { log_error "sudo install failed"; exit 1; }
        else
            sudo cp "$binary_path" "$install_dir/$BINARY_NAME" || { log_error "sudo cp failed"; exit 1; }
            sudo chmod 0755 "$install_dir/$BINARY_NAME" || { log_error "sudo chmod failed"; exit 1; }
        fi
    else
        log_error "$(t "Dir not writable, no sudo" "目录不可写且无 sudo")"
        exit 1
    fi

    if [ -x "$install_dir/$BINARY_NAME" ]; then
        log_info "$(t "Installed: $install_dir/$BINARY_NAME" "已安装: $install_dir/$BINARY_NAME")"
        "$install_dir/$BINARY_NAME" --version 2>/dev/null || true
    else
        log_error "$(t "Executable not found" "未找到可执行文件")"; exit 1
    fi

    case ":$PATH:" in
        *":$install_dir:"*) ;;
        *) log_warn "$(t "Add $install_dir to PATH" "请将 $install_dir 添加到 PATH")" ;;
    esac
}

main() {
    parse_args "$@"
    select_language

    printf "${B}▸ TTM Installer${R}\n"
    init_temp_dir

    log_step "$(t "Dependencies" "检查依赖")"
    require_download_tool
    require_extract_tool

    log_step "$(t "Version" "获取版本")"
    if [ "$PREVIEW" = "1" ]; then
        fetch_preview_version
    else
        fetch_latest_version
    fi

    log_step "$(t "Platform" "检测平台")"
    detect_platform

    log_step "$(t "Download" "下载")"
    INSTALL_PATH="$(determine_install_dir)"
    log_info "$(t "Install to: $INSTALL_PATH" "安装到: $INSTALL_PATH")"
    get_download_info
    log_info "$DOWNLOAD_URL"

    local zip_file="$TEMP_DIR/$FILENAME"
    download_with_mirrors "$DOWNLOAD_URL" "$zip_file" || {
        log_error "$(t "Download failed" "下载失败")"; exit 1
    }

    log_step "$(t "Verify" "校验")"
    local expected_sha sha_url="${DOWNLOAD_URL}.sha256"
    if expected_sha="$(get_sha256_hash "$sha_url")" && [ -n "$expected_sha" ]; then
        if ! verify_sha256 "$zip_file" "$expected_sha"; then
            prompt_continue_or_abort "$(t \
                "Checksum mismatch, file may be corrupted or tampered" \
                "校验不匹配，文件可能已损坏或被篡改")"
        fi
    else
        prompt_continue_or_abort "$(t \
            "Cannot fetch checksum, file integrity unknown" \
            "无法获取校验信息，文件完整性未知")"
    fi

    log_step "$(t "Install" "安装")"
    install_binary "$zip_file" "$INSTALL_PATH"

    printf "\n${GRN}${B}✔ %s${R}\n\n" "$(t "Done!" "安装完成!")"
}

main "$@"

#!/usr/bin/env bash

set -euo pipefail

REPO_OWNER="vst93"
REPO_NAME="ttm"
BINARY_NAME="ttm"
REPO_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}"
API_URL="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
FORCE_INSTALL="${FORCE_INSTALL:-0}"

TEMP_DIR=""

log_info() {
    printf '[INFO] %s\n' "$*"
}

log_warn() {
    printf '[WARN] %s\n' "$*" >&2
}

log_error() {
    printf '[ERROR] %s\n' "$*" >&2
}

cleanup() {
    if [ -n "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

trap cleanup EXIT

is_termux() {
    [ -n "${TERMUX_VERSION:-}" ] || [ "${PREFIX:-}" = "/data/data/com.termux/files/usr" ] || [ -d "/data/data/com.termux/files/usr" ]
}

is_interactive() {
    [ -t 0 ]
}

has_cmd() {
    command -v "$1" >/dev/null 2>&1
}

print_help() {
    cat <<EOF
Usage: ${BINARY_NAME}-install [OPTIONS]

Install latest ${BINARY_NAME} release for current platform.

Options:
  -h, --help                Show this help and exit
      --install-dir <dir>   Override install directory
      --force               Continue install when checksum fetch/verify fails

Environment variables:
  INSTALL_DIR      Same as --install-dir
  FORCE_INSTALL=1  Same as --force
EOF
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            -h|--help)
                print_help
                exit 0
                ;;
            --install-dir)
                if [ "$#" -lt 2 ]; then
                    log_error "--install-dir 需要传入目录路径"
                    exit 2
                fi
                INSTALL_DIR="$2"
                shift
                ;;
            --force)
                FORCE_INSTALL="1"
                ;;
            *)
                log_error "未知参数: $1"
                log_error "可用参数: --help, --install-dir <dir>, --force"
                exit 2
                ;;
        esac
        shift
    done
}

init_temp_dir() {
    TEMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t ttm)"
}

require_download_tool() {
    if has_cmd curl || has_cmd wget; then
        return 0
    fi

    log_error "需要 curl 或 wget 用于下载"
    exit 1
}

require_extract_tool() {
    if has_cmd unzip; then
        return 0
    fi

    log_error "需要 unzip 用于解压发布包"
    if is_termux; then
        log_info "Termux 可运行: pkg install unzip"
    fi
    exit 1
}

download_file() {
    local url="$1"
    local output_file="$2"

    if has_cmd curl; then
        curl -fsSL --retry 3 --retry-delay 1 -o "$output_file" "$url"
    elif has_cmd wget; then
        wget -q -O "$output_file" "$url"
    else
        log_error "需要 curl 或 wget 用于下载"
        exit 1
    fi

    if [ ! -s "$output_file" ]; then
        log_error "下载失败或文件为空: $url"
        exit 1
    fi
}

fetch_latest_version() {
    local response
    local version

    if has_cmd curl; then
        response="$(curl -fsSL "$API_URL")"
    elif has_cmd wget; then
        response="$(wget -q -O - "$API_URL")"
    else
        log_error "需要 curl 或 wget 用于获取版本信息"
        exit 1
    fi

    version="$(printf '%s\n' "$response" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"

    if [ -z "$version" ]; then
        log_error "无法从 GitHub API 解析最新版本号"
        exit 1
    fi

    VERSION="$version"
}

detect_platform() {
    local uname_s uname_m

    uname_s="$(uname -s)"
    uname_m="$(uname -m)"

    case "$uname_s" in
        Darwin)
            OS="darwin"
            ;;
        Linux)
            if is_termux; then
                OS="android"
            else
                OS="linux"
            fi
            ;;
        *)
            log_error "不支持的操作系统: $uname_s"
            exit 1
            ;;
    esac

    case "$uname_m" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        armv7l|armv8l|arm)
            log_error "暂不支持 32 位 ARM 架构: $uname_m"
            log_error "请使用 arm64 设备，或从源码构建"
            exit 1
            ;;
        *)
            log_error "不支持的 CPU 架构: $uname_m"
            exit 1
            ;;
    esac

    log_info "检测到系统: ${OS}-${ARCH}"
}

get_download_info() {
    case "${OS}-${ARCH}" in
        darwin-arm64)
            FILENAME="ttm-darwin-arm64.zip"
            ;;
        darwin-amd64)
            FILENAME="ttm-darwin-amd64.zip"
            ;;
        linux-arm64)
            FILENAME="ttm-linux-arm64.zip"
            ;;
        linux-amd64)
            FILENAME="ttm-linux-amd64.zip"
            ;;
        android-arm64)
            FILENAME="ttm-android-arm64.zip"
            ;;
        android-amd64)
            FILENAME="ttm-android-amd64.zip"
            ;;
        *)
            log_error "没有找到适用于 ${OS}-${ARCH} 的发布包"
            exit 1
            ;;
    esac

    DOWNLOAD_URL="${REPO_URL}/releases/download/${VERSION}/${FILENAME}"
}

get_sha256_hash() {
    local sha256_url="$1"
    local sha_file="$TEMP_DIR/${FILENAME}.sha256"

    download_file "$sha256_url" "$sha_file"
    awk '{print $1}' "$sha_file"
}

compute_sha256() {
    local file="$1"

    if has_cmd sha256sum; then
        sha256sum "$file" | awk '{print $1}'
    elif has_cmd shasum; then
        shasum -a 256 "$file" | awk '{print $1}'
    elif has_cmd openssl; then
        openssl dgst -sha256 "$file" | awk '{print $NF}'
    else
        return 1
    fi
}

verify_sha256() {
    local file="$1"
    local expected_sha="$2"
    local actual_sha

    if ! actual_sha="$(compute_sha256 "$file")"; then
        log_warn "未找到可用 SHA256 工具，跳过校验"
        return 0
    fi

    if [ "$actual_sha" != "$expected_sha" ]; then
        log_error "SHA256 校验失败"
        log_error "期望: $expected_sha"
        log_error "实际: $actual_sha"
        return 1
    fi

    log_info "SHA256 校验通过"
}

prompt_continue_or_abort() {
    local reason="$1"

    if [ "$FORCE_INSTALL" = "1" ]; then
        log_warn "$reason，FORCE_INSTALL=1，继续安装"
        return 0
    fi

    if is_interactive; then
        printf '%s，是否继续安装? (y/N): ' "$reason"
        read -r reply
        case "$reply" in
            y|Y|yes|YES)
                return 0
                ;;
            *)
                log_error "安装已取消"
                exit 1
                ;;
        esac
    else
        log_error "$reason，非交互环境下默认中止。可设置 FORCE_INSTALL=1 强制继续"
        exit 1
    fi
}

determine_install_dir() {
    if [ -n "${INSTALL_DIR:-}" ]; then
        printf '%s\n' "$INSTALL_DIR"
        return
    fi

    if is_termux && [ -n "${PREFIX:-}" ]; then
        printf '%s\n' "$PREFIX/bin"
        return
    fi

    if [ -d "/usr/local/bin" ] && { [ -w "/usr/local/bin" ] || { ! is_termux && has_cmd sudo; }; }; then
        printf '%s\n' "/usr/local/bin"
        return
    fi

    if [ -n "${HOME:-}" ]; then
        printf '%s\n' "$HOME/.local/bin"
        return
    fi

    log_error "无法确定安装目录"
    exit 1
}

ensure_dir_exists() {
    local dir="$1"

    if [ -d "$dir" ]; then
        return 0
    fi

    if mkdir -p "$dir" 2>/dev/null; then
        return 0
    fi

    if is_termux; then
        log_error "无法创建目录: $dir"
        exit 1
    fi

    if has_cmd sudo; then
        sudo mkdir -p "$dir" || {
            log_error "sudo 创建目录失败: $dir"
            exit 1
        }
        return 0
    fi

    log_error "无法创建目录且缺少 sudo: $dir"
    exit 1
}

install_binary() {
    local zip_file="$1"
    local install_dir="$2"
    local extracted_dir="$TEMP_DIR/extracted"
    local binary_path

    mkdir -p "$extracted_dir"
    unzip -q "$zip_file" -d "$extracted_dir"

    binary_path="$extracted_dir/$BINARY_NAME"
    if [ ! -f "$binary_path" ]; then
        log_error "在压缩包中找不到 $BINARY_NAME"
        exit 1
    fi

    chmod +x "$binary_path"
    ensure_dir_exists "$install_dir"

    if [ -w "$install_dir" ]; then
        if has_cmd install; then
            install -m 0755 "$binary_path" "$install_dir/$BINARY_NAME"
        else
            cp "$binary_path" "$install_dir/$BINARY_NAME"
            chmod 0755 "$install_dir/$BINARY_NAME"
        fi
    elif ! is_termux && has_cmd sudo; then
        if has_cmd install; then
            sudo install -m 0755 "$binary_path" "$install_dir/$BINARY_NAME" || {
                log_error "sudo 安装失败: $install_dir/$BINARY_NAME"
                exit 1
            }
        else
            sudo cp "$binary_path" "$install_dir/$BINARY_NAME" || {
                log_error "sudo 复制失败: $install_dir/$BINARY_NAME"
                exit 1
            }
            sudo chmod 0755 "$install_dir/$BINARY_NAME" || {
                log_error "sudo chmod 失败: $install_dir/$BINARY_NAME"
                exit 1
            }
        fi
    else
        log_error "目录不可写且无法提权: $install_dir"
        exit 1
    fi

    if [ -x "$install_dir/$BINARY_NAME" ]; then
        log_info "安装成功: $install_dir/$BINARY_NAME"
        "$install_dir/$BINARY_NAME" --version 2>/dev/null || true
    else
        log_error "安装后未找到可执行文件"
        exit 1
    fi

    case ":$PATH:" in
        *":$install_dir:"*)
            ;;
        *)
            log_warn "$install_dir 不在 PATH 中，请手动添加"
            ;;
    esac
}

main() {
    parse_args "$@"
    init_temp_dir

    require_download_tool
    require_extract_tool
    fetch_latest_version
    detect_platform

    INSTALL_PATH="$(determine_install_dir)"
    log_info "开始安装 ${BINARY_NAME} ${VERSION}"
    log_info "安装目录: $INSTALL_PATH"

    get_download_info
    log_info "下载地址: $DOWNLOAD_URL"

    local zip_file="$TEMP_DIR/$FILENAME"
    download_file "$DOWNLOAD_URL" "$zip_file"

    local expected_sha
    local sha_url="${DOWNLOAD_URL}.sha256"
    if expected_sha="$(get_sha256_hash "$sha_url")" && [ -n "$expected_sha" ]; then
        if ! verify_sha256 "$zip_file" "$expected_sha"; then
            prompt_continue_or_abort "SHA256 校验失败"
        fi
    else
        prompt_continue_or_abort "无法获取 SHA256 校验信息"
    fi

    install_binary "$zip_file" "$INSTALL_PATH"
}

main "$@"

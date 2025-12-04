#!/usr/bin/env bash

# TUI Terminal Manager 安装脚本
# 类似 Homebrew 的跨平台二进制文件安装逻辑

set -euo pipefail

# 配置参数
REPO="vst93/ttm"
VERSION="0.1.0"
HOMEPAGE="https://github.com/${REPO}"

# 安装目录
INSTALL_DIR="${HOME}/.local/bin"
BACKUP_DIR="${HOME}/.local/backup"
LOG_FILE="${HOME}/.local/log/ttm_install.log"

# 创建必要的目录
mkdir -p "${INSTALL_DIR}" "${BACKUP_DIR}" "${HOME}/.local/log"

# 日志函数
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "${LOG_FILE}"
}

# 错误处理函数
error_exit() {
    log "❌ 错误: $1"
    exit 1
}

# 检测系统信息
detect_platform() {
    local os=""
    local arch=""
    
    # 检测操作系统
    case "$(uname -s)" in
        Linux*)
            os="linux"
            ;;
        Darwin*)
            os="darwin"
            ;;
        *)
            error_exit "不支持的操作系统: $(uname -s)"
            ;;
    esac
    
    # 检测架构
    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        armv7l|armv8l)
            arch="arm"
            ;;
        *)
            error_exit "不支持的架构: $(uname -m)"
            ;;
    esac
    
    echo "${os}-${arch}"
}

# 获取下载 URL
get_download_url() {
    local platform="$1"
    
    case "${platform}" in
        darwin-arm64)
            echo "${HOMEPAGE}/releases/download/${VERSION}/ttm-darwin-arm64.zip"
            echo "1d726ce214fad246a3911ed3f9c98988a66df1610d373a633659da7f1551d3a3"
            ;;
        darwin-amd64)
            echo "${HOMEPAGE}/releases/download/${VERSION}/ttm-darwin-amd64.zip"
            echo "3208667d66aadfd560fa2d9b6171d266d0c5e5de69d2e9556aabea5cfd62c74f"
            ;;
        linux-arm64)
            echo "${HOMEPAGE}/releases/download/${VERSION}/ttm-linux-arm64.zip"
            echo "902a0b784d2746f4fa818afed42fbc0e86aa5cb19aee2ff95401c1aa763493ae"
            ;;
        linux-amd64)
            echo "${HOMEPAGE}/releases/download/${VERSION}/ttm-linux-amd64.zip"
            echo "e795778242c04e3554e6a0f35ca934d507b294663f1af119604ec15fd35385a5"
            ;;
        # 如果需要支持 Android
        # android-arm64)
        #     echo "${HOMEPAGE}/releases/download/${VERSION}/ttm-android-arm64.zip"
        #     echo "0d466f44afdab4484d6e6242d5329bbbfe0c38587573a082323c65509a97dbae"
        #     ;;
        *)
            error_exit "没有找到适用于 ${platform} 的发布版本"
            ;;
    esac
}

# 验证文件 SHA256
verify_sha256() {
    local file="$1"
    local expected_sha="$2"
    
    if ! command -v sha256sum &> /dev/null; then
        if command -v shasum &> /dev/null; then
            local actual_sha=$(shasum -a 256 "${file}" | cut -d' ' -f1)
        else
            log "⚠️  警告: 找不到 sha256sum 或 shasum 命令，跳过校验"
            return 0
        fi
    else
        local actual_sha=$(sha256sum "${file}" | cut -d' ' -f1)
    fi
    
    if [ "${actual_sha}" != "${expected_sha}" ]; then
        error_exit "SHA256 校验失败\n期望: ${expected_sha}\n实际: ${actual_sha}"
    fi
    
    log "✅ SHA256 校验通过"
}

# 下载文件
download_file() {
    local url="$1"
    local output_file="$2"
    local expected_sha="$3"
    
    log "下载: ${url}"
    
    # 尝试使用 curl 或 wget
    if command -v curl &> /dev/null; then
        curl -L -f -o "${output_file}" "${url}" || error_exit "下载失败: ${url}"
    elif command -v wget &> /dev/null; then
        wget -O "${output_file}" "${url}" || error_exit "下载失败: ${url}"
    else
        error_exit "需要 curl 或 wget 来下载文件"
    fi
    
    # 验证文件
    verify_sha256 "${output_file}" "${expected_sha}"
}

# 解压文件
extract_file() {
    local archive="$1"
    local extract_dir="$2"
    
    log "解压文件: ${archive}"
    
    case "${archive}" in
        *.zip)
            if command -v unzip &> /dev/null; then
                unzip -q -o "${archive}" -d "${extract_dir}" || error_exit "解压失败"
            else
                error_exit "需要 unzip 命令来解压 .zip 文件"
            fi
            ;;
        *.tar.gz|*.tgz)
            tar -xzf "${archive}" -C "${extract_dir}" || error_exit "解压失败"
            ;;
        *.tar.bz2)
            tar -xjf "${archive}" -C "${extract_dir}" || error_exit "解压失败"
            ;;
        *.tar.xz)
            tar -xJf "${archive}" -C "${extract_dir}" || error_exit "解压失败"
            ;;
        *)
            error_exit "不支持的文件格式: ${archive}"
            ;;
    esac
}

# 备份旧版本
backup_old_version() {
    local target="${INSTALL_DIR}/ttm"
    
    if [ -f "${target}" ]; then
        local backup_file="${BACKUP_DIR}/ttm_$(date '+%Y%m%d_%H%M%S')"
        log "备份旧版本到: ${backup_file}"
        cp "${target}" "${backup_file}"
    fi
}

# 安装二进制文件
install_binary() {
    local extract_dir="$1"
    local binary_name="ttm"
    
    # 查找二进制文件
    local binary_path=""
    if [ -f "${extract_dir}/${binary_name}" ]; then
        binary_path="${extract_dir}/${binary_name}"
    elif [ -f "${extract_dir}/bin/${binary_name}" ]; then
        binary_path="${extract_dir}/bin/${binary_name}"
    else
        # 在解压目录中查找
        binary_path=$(find "${extract_dir}" -name "${binary_name}" -type f -executable | head -n1)
        if [ -z "${binary_path}" ]; then
            error_exit "在解压文件中找不到可执行文件 ${binary_name}"
        fi
    fi
    
    # 备份旧版本
    backup_old_version
    
    # 安装新版本
    log "安装到: ${INSTALL_DIR}/ttm"
    chmod +x "${binary_path}"
    cp "${binary_path}" "${INSTALL_DIR}/ttm"
    
    # 确保安装目录在 PATH 中
    if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
        log "⚠️  提示: ${INSTALL_DIR} 不在 PATH 中"
        log "请将以下行添加到你的 shell 配置文件 (~/.bashrc, ~/.zshrc 等):"
        echo "export PATH=\"\${HOME}/.local/bin:\$PATH\""
    fi
}

# 清理临时文件
cleanup() {
    if [ -d "${TEMP_DIR:-}" ]; then
        log "清理临时文件"
        rm -rf "${TEMP_DIR}"
    fi
}

# 测试安装
test_installation() {
    log "测试安装..."
    
    if command -v ttm &> /dev/null; then
        log "✅ ttm 命令已可用"
    elif [ -x "${INSTALL_DIR}/ttm" ]; then
        log "✅ 二进制文件已安装到 ${INSTALL_DIR}/ttm"
        log "请确保 ${INSTALL_DIR} 在你的 PATH 环境变量中"
    else
        error_exit "安装测试失败"
    fi
}

# 显示系统信息
show_system_info() {
    log "系统信息:"
    log "  操作系统: $(uname -s)"
    log "  架构: $(uname -m)"
    log "  主机名: $(uname -n)"
    log "  内核版本: $(uname -r)"
}

# 主安装函数
main() {
    log "开始安装 ttm v${VERSION}"
    show_system_info
    
    # 检测平台
    local platform=$(detect_platform)
    log "检测到平台: ${platform}"
    
    # 获取下载信息
    local download_info=($(get_download_url "${platform}"))
    local download_url="${download_info[0]}"
    local expected_sha="${download_info[1]}"
    
    log "下载 URL: ${download_url}"
    
    # 创建临时目录
    TEMP_DIR=$(mktemp -d)
    trap cleanup EXIT
    
    local archive_file="${TEMP_DIR}/ttm.zip"
    local extract_dir="${TEMP_DIR}/extract"
    
    mkdir -p "${extract_dir}"
    
    # 下载并安装
    download_file "${download_url}" "${archive_file}" "${expected_sha}"
    extract_file "${archive_file}" "${extract_dir}"
    install_binary "${extract_dir}"
    
    # 测试
    test_installation
    
    log "🎉 ttm v${VERSION} 安装完成！"
    log "运行 'ttm --help' 开始使用"
}

# 运行主函数
main "$@"
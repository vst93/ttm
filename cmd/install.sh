#!/bin/bash

set -e

# 配置信息
VERSION="0.1.0"
REPO="https://github.com/vst93/ttm"
BINARY_NAME="ttm"
TEMP_DIR=$(mktemp -d)

# 确定安装目录
determine_install_dir() {
    # 优先级：用户指定 > /usr/local/bin > ~/.local/bin > ~/bin
    local install_dir=""
    
    # 1. 检查用户是否指定了安装目录
    if [ -n "$INSTALL_DIR" ]; then
        install_dir="$INSTALL_DIR"
        echo "使用用户指定的安装目录: $install_dir"
        echo "$install_dir"
        return 0
    fi
    
    # 2. 检查 /usr/local/bin
    if [ -d "/usr/local/bin" ]; then
        if [ -w "/usr/local/bin" ]; then
            echo "/usr/local/bin"
            return 0
        fi
    fi
    
    # 3. 检查 ~/../usr/local/bin (用户主目录上级的usr/local/bin)
    local home_parent_local="${HOME%/*}/usr/local/bin"
    if [ -n "$HOME" ] && [ -d "$home_parent_local" ]; then
        if [ -w "$home_parent_local" ]; then
            echo "$home_parent_local"
            return 0
        fi
    fi
    
    # 4. 检查 ~/.local/bin (用户本地目录)
    local user_local_bin="$HOME/.local/bin"
    if [ -n "$HOME" ]; then
        echo "$user_local_bin"
        return 0
    fi
    
    # 5. 最后尝试 ~/bin
    if [ -n "$HOME" ]; then
        echo "$HOME/bin"
        return 0
    fi

    # 如果所有选项都失败
    echo "错误: 无法确定安装目录"
    exit 1
}

# 清理函数
cleanup() {
    echo "清理临时文件..."
    rm -rf "$TEMP_DIR"
}

# 错误处理
trap cleanup EXIT
trap 'echo "安装过程中出现错误"; exit 1' ERR

# 检测系统平台和架构
detect_platform() {
    OS=""
    ARCH=""
    
    # 检测操作系统
    case "$(uname -o)" in
        Darwin)
            OS="darwin"
            ;;
        Linux)
            OS="linux"
            ;;
        Android)
            OS="android"
            ;;
        *)
            echo "不支持的操作系统: $(uname -o)"
            exit 1
            ;;
    esac
    
    # 检测CPU架构
    case "$(uname -m)" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        arm64|aarch64)
            ARCH="arm64"
            ;;
        *)
            echo "不支持的CPU架构: $(uname -m)"
            exit 1
            ;;
    esac
    
    echo "检测到系统: $OS-$ARCH"
}

# 构建下载URL
get_download_info() {
    local os="$1"
    local arch="$2"
    
    case "$os-$arch" in
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
            echo "没有找到适用于 $os-$arch 的版本"
            exit 1
            ;;
    esac
    
    DOWNLOAD_URL="${REPO}/releases/download/${VERSION}/${FILENAME}"
}

# 下载文件
download_file() {
    local url="$1"
    local output_file="$2"
    
    # echo "正在下载: $url"
    
    if command -v curl &> /dev/null; then
        curl -L -o "$output_file" "$url"
    elif command -v wget &> /dev/null; then
        wget -O "$output_file" "$url"
    else
        echo "错误: 需要curl或wget进行下载"
        exit 1
    fi
    
    # 检查下载是否成功
    if [ ! -f "$output_file" ] || [ ! -s "$output_file" ]; then
        echo "错误: 文件下载失败或为空"
        exit 1
    fi
}

# 获取SHA256校验值
get_sha256_hash() {
    local sha256_url="$1"
    local temp_sha_file="$TEMP_DIR/$(basename "$sha256_url")"
    
    # 下载SHA256文件
    download_file "$sha256_url" "$temp_sha_file"
    
    # 从SHA256文件中提取校验值
    # SHA256文件格式通常是: "hash filename" 或只有 "hash"
    if grep -q " " "$temp_sha_file"; then
        # 格式为 "hash filename"，提取第一个字段
        cut -d ' ' -f 1 "$temp_sha_file"
    else
        # 格式为只有 "hash"
        cat "$temp_sha_file"
    fi
}

# 验证SHA256
verify_sha256() {
    local file="$1"
    local expected_sha="$2"
    
    if ! command -v shasum &> /dev/null && ! command -v sha256sum &> /dev/null; then
        echo "警告: 找不到shasum或sha256sum命令，跳过校验"
        return 0
    fi
    
    local actual_sha=""
    if command -v shasum &> /dev/null; then
        actual_sha=$(shasum -a 256 "$file" | cut -d ' ' -f1)
    elif command -v sha256sum &> /dev/null; then
        actual_sha=$(sha256sum "$file" | cut -d ' ' -f1)
    fi
    
    if [ "$actual_sha" != "$expected_sha" ]; then
        echo "SHA256校验失败!"
        echo "期望: $expected_sha"
        echo "实际: $actual_sha"
        return 1
    fi
    
    echo "SHA256校验通过"
    return 0
}

# 确保目录存在并返回是否需要sudo
ensure_dir_exists() {
    local dir="$1"
    
    if [ ! -d "$dir" ]; then
        echo "创建目录: $dir"
        # 尝试创建目录
        if mkdir -p "$dir" 2>/dev/null; then
            echo "目录创建成功"
            return 0  # 不需要sudo
        else
            echo "需要sudo权限创建目录"
            if sudo mkdir -p "$dir"; then
                return 1  # 需要sudo
            else
                echo "无法创建目录: $dir"
                exit 1
            fi
        fi
    else
        # 检查写权限
        if [ -w "$dir" ]; then
            return 0  # 不需要sudo
        else
            echo "目录 $dir 存在但无写权限"
            return 1  # 需要sudo
        fi
    fi
}

# 安装二进制文件
install_binary() {
    local zip_file="$1"
    local install_dir="$2"
    
    echo "解压文件..."
    unzip -q "$zip_file" -d "$TEMP_DIR"
    
    local binary_path="$TEMP_DIR/$BINARY_NAME"
    
    if [ ! -f "$binary_path" ]; then
        echo "错误: 在压缩包中找不到 $BINARY_NAME"
        exit 1
    fi
    
    echo "安装到 $install_dir"
    chmod +x "$binary_path"
    
    # 确保安装目录存在并检查权限
    if ensure_dir_exists "$install_dir"; then
        # 不需要sudo
        mv "$binary_path" "$install_dir/"
        echo "文件移动成功"
    else
        # 需要sudo
        echo "需要sudo权限写入 $install_dir"
        sudo mv "$binary_path" "$install_dir/"
        echo "文件移动成功 (使用sudo)"
    fi
    
    # 验证安装
    if command -v "$BINARY_NAME" &> /dev/null; then
        echo "安装成功!"
        echo "$BINARY_NAME 版本: $($BINARY_NAME --version 2>/dev/null || echo '已安装')"
    else
        echo "注意: $BINARY_NAME 可能不在你的PATH中"
        echo "安装位置: $install_dir"
        echo "请确保 $install_dir 在你的PATH环境变量中"
        
        # 提供添加PATH的建议
        local shell_rc=""
        if [ -f "$HOME/.bashrc" ]; then
            shell_rc="~/.bashrc"
        elif [ -f "$HOME/.zshrc" ]; then
            shell_rc="~/.zshrc"
        elif [ -f "$HOME/.profile" ]; then
            shell_rc="~/.profile"
        fi
        
        if [ -n "$shell_rc" ]; then
            echo ""
            echo "你可以运行以下命令将其添加到PATH:"
            echo "  echo 'export PATH=\"$install_dir:\$PATH\"' >> $shell_rc"
            echo "然后重新加载配置: source $shell_rc"
        fi
    fi
}

# 主安装流程
main() {
    echo "开始安装 $BINARY_NAME v$VERSION"
    
    # 确定安装目录
    INSTALL_DIR=$(determine_install_dir)
    echo "最终安装目录: $INSTALL_DIR"
    
    # 检测平台
    detect_platform
    
    # 获取下载信息
    get_download_info "$OS" "$ARCH"
    
    echo "下载地址: $DOWNLOAD_URL"
    
    # 下载文件
    local zip_file="$TEMP_DIR/$FILENAME"
    download_file "$DOWNLOAD_URL" "$zip_file"
    
    # 获取并验证SHA256
    local sha256_url="${DOWNLOAD_URL}.sha256"
    echo "获取SHA256校验值: $sha256_url"
    
    local expected_sha=""
    if expected_sha=$(get_sha256_hash "$sha256_url"); then
        echo "验证文件完整性..."
        if ! verify_sha256 "$zip_file" "$expected_sha"; then
            echo "警告: SHA256校验失败，但继续安装"
            read -p "是否继续安装? (y/N): " -n 1 -r
            echo
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                echo "安装已取消"
                exit 1
            fi
        fi
    else
        echo "警告: 无法获取SHA256校验值，跳过校验"
        read -p "是否继续安装? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "安装已取消"
            exit 1
        fi
    fi
    
    # 安装
    install_binary "$zip_file" "$INSTALL_DIR"
}

# 运行主函数
main "$@"
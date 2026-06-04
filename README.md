[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8.svg?logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows%20%7C%20Android-lightgrey.svg)](.)

<div align="center">

# TTM — Tiny Terminal Manager

Lightweight SSH bookmark manager built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

Manage, sync and connect to your servers — all from the terminal.

**[English](#english)** · **[中文](#中文)**

<img src="https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/screenshot/1.png" alt="TTM Screenshot 1" width="800" />
<img src="https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/screenshot/2.png" alt="TTM Screenshot 2" width="800" />

</div>

---

## English

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Gist Sync](#gist-sync)
- [Keybindings](#keybindings)
- [License](#license)

### Features

- **Full TUI** — no config files to hand-edit, everything is in the interface
- **SSH connect** — password, private key, or keyboard-interactive auth
- **Bookmark management** — add, edit, delete, star/favorite
- **Gist sync** — push/pull bookmarks via GitHub or Gitee Gist
- **In-app config** — set token and gist ID without leaving the TUI
- **Filter & paginate** — fuzzy search across bookmarks
- **Private key input modes** — paste key text or provide a key file path (resolved on save, ≤256KB)
- **Clipboard-aware copy** — `Ctrl+Y` supports system clipboard with terminal fallback
- **Config export/import** — share config across machines via `Ctrl+B` export + `--import-config`
- **i18n** — English / 中文, toggle with `L`
- **Update check** — press `u` to check for new releases
- **Cross-platform** — macOS, Linux, Windows, Android (Termux)

### Installation

**Homebrew (macOS / Linux):**

```bash
brew install vst93/tap/ttm
```

**Shell script (macOS / Linux):**

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

Options: `--help` to show installer flags, `INSTALL_DIR="$HOME/.local/bin"` for custom path, `AUTO_DELETE_INSTALL_SCRIPT=0` to keep the script, `FORCE_INSTALL=1` to skip checksum verification.

**Windows:** Download `ttm-windows-*.zip` from [Releases](https://github.com/vst93/ttm/releases), extract `ttm.exe`, add to `PATH`.

**Termux (Android):**

```bash
pkg update && pkg install curl unzip
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

**Build from source:**

```bash
git clone https://github.com/vst93/ttm.git && cd ttm
make build   # or: go build -o ttm .
```

### Quick Start

```bash
ttm
```

1. `c` — Open config editor, set GitHub/Gitee token and gist ID
2. `a` — Add a new bookmark (host, user, port, auth type)
3. `Enter` — Connect to the selected server
4. `s` / `S` — Push or pull bookmarks via Gist

### Configuration

Config directory: `~/.config/ttm/` (Linux), `~/Library/Application Support/ttm/` (macOS), `%APPDATA%\ttm\` (Windows)

| File | Description |
|------|-------------|
| `config.json` | Platform, token, gist ID, locale |
| `bookmarks.json` | Bookmark entries |

```json
{
  "platform": "github",
  "token": "ghp_xxxxxxxxxxxx",
  "gistId": "abcdef1234567890abcdef1234567890",
  "locale": "en"
}
```

**Export / Import:** Press `c` → `Ctrl+B` to export config as a base64-encoded import command. Run `ttm --import-config <base64>` on another machine to import (details displayed, confirmation required).

**Clipboard notes:** In remote SSH/tmux sessions, clipboard falls back to terminal channel if system clipboard is unavailable. Enable tmux passthrough with `set -g set-clipboard on`.

**Private key path input:** The Private Key field accepts either raw key text or a file path. On save, if the value reads as a file (≤256KB), TTM stores the content. If reading fails, the path text is preserved with a tip.

### Gist Sync

1. `c` — Open config editor
2. Select platform (`github` / `gitee`)
3. Enter Personal Access Token (needs `gist` scope)
   - [GitHub Tokens](https://github.com/settings/tokens) · [Gitee Tokens](https://gitee.com/personal_access_tokens)
4. Enter Gist ID (leave empty to auto-create on first push)
5. `Ctrl+S` to save
6. `s` to push, `S` to pull

### Keybindings

| Key | Action | Key | Action |
|-----|--------|-----|--------|
| `Enter` | Connect | `a` | Add bookmark |
| `e` | Edit bookmark | `d` | Delete bookmark |
| `*` | Star / unstar | `s` / `S` | Push / Pull Gist |
| `c` | Config editor | `u` | Check updates |
| `L` | Toggle EN / 中文 | `/` | Filter |
| `j` / `k` `↑` / `↓` | Navigate | `h` / `l` `←` / `→` | Page |
| `?` | Help | `q` | Quit |

**Editor shortcuts:** `Ctrl+S` save, `Ctrl+R` reveal/hide secrets, `Ctrl+Y` copy field, `Ctrl+U` clear field, `Ctrl+V` paste, `Ctrl+B` export config.

### License

[MIT](LICENSE) © 2025 [vst](https://github.com/vst93)

---

## 中文

- [功能特性](#功能特性)
- [安装](#安装)
- [快速开始](#快速开始)
- [配置说明](#配置说明)
- [Gist 同步](#gist-同步)
- [快捷键](#快捷键)
- [许可证](#许可证)

### 功能特性

- **全 TUI 界面** — 无需手动编辑配置文件，所有操作均在界面内完成
- **SSH 连接** — 支持密码、私钥、键盘交互三种认证方式
- **书签管理** — 新增、编辑、删除、收藏
- **Gist 同步** — 通过 GitHub 或 Gitee Gist 推送 / 拉取书签
- **应用内配置** — 无需离开 TUI 即可设置 Token 和 Gist ID
- **搜索与分页** — 模糊过滤书签列表
- **私钥输入模式** — 支持粘贴私钥文本或填写私钥文件路径（保存时解析，≤256KB）
- **剪贴板感知复制** — `Ctrl+Y` 优先系统剪贴板，可用时回退终端通道
- **配置导入导出** — 通过 `Ctrl+B` 导出 + `--import-config` 跨机器共享配置
- **双语** — English / 中文，按 `L` 切换
- **版本检查** — 按 `u` 检查新版本
- **跨平台** — macOS、Linux、Windows、Android (Termux)

### 安装

**Homebrew (macOS / Linux)：**

```bash
brew install vst93/tap/ttm
```

**脚本安装（macOS / Linux）：**

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

可选参数：`--help` 查看安装参数、`INSTALL_DIR="$HOME/.local/bin"` 自定义路径、`AUTO_DELETE_INSTALL_SCRIPT=0` 保留脚本、`FORCE_INSTALL=1` 跳过校验。

**Windows：** 从 [Releases](https://github.com/vst93/ttm/releases) 下载 `ttm-windows-*.zip`，解压 `ttm.exe`，添加到 `PATH`。

**Termux（Android）：**

```bash
pkg update && pkg install curl unzip
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

**从源码构建：**

```bash
git clone https://github.com/vst93/ttm.git && cd ttm
make build   # 或：go build -o ttm .
```

### 快速开始

```bash
ttm
```

1. `c` — 打开配置编辑器，设置 GitHub/Gitee 令牌和 Gist ID
2. `a` — 新增书签（主机、用户、端口、认证方式）
3. `Enter` — 连接选中服务器
4. `s` / `S` — 通过 Gist 推送或拉取书签

### 配置说明

配置文件路径：`~/.config/ttm/`（Linux）、`~/Library/Application Support/ttm/`（macOS）、`%APPDATA%\ttm\`（Windows）

| 文件 | 说明 |
|------|------|
| `config.json` | 平台、令牌、Gist ID、语言 |
| `bookmarks.json` | 书签数据 |

```json
{
  "platform": "github",
  "token": "ghp_xxxxxxxxxxxx",
  "gistId": "abcdef1234567890abcdef1234567890",
  "locale": "zh"
}
```

**导出 / 导入：** 按 `c` → `Ctrl+B` 导出配置为 base64 编码的导入命令。在其他机器运行 `ttm --import-config <base64>` 即可导入（展示配置详情，确认后写入）。

**剪贴板说明：** 远程 SSH/tmux 会话中，系统剪贴板不可用时自动回退终端通道。tmux 建议开启 `set -g set-clipboard on`。

**私钥路径输入：** Private Key 字段可填写私钥文本或文件路径。保存时若读取文件成功（≤256KB）则存入内容，失败则保留路径文本并给出提示。

### Gist 同步

1. `c` — 打开配置编辑器
2. 选择平台（`github` / `gitee`）
3. 输入 Personal Access Token（需要 `gist` 权限）
   - [GitHub Tokens](https://github.com/settings/tokens) · [Gitee Tokens](https://gitee.com/personal_access_tokens)
4. 输入 Gist ID（留空则首次推送时自动创建）
5. `Ctrl+S` 保存
6. `s` 推送，`S` 拉取

### 快捷键

| 按键 | 功能 | 按键 | 功能 |
|------|------|------|------|
| `Enter` | 连接 | `a` | 新增书签 |
| `e` | 编辑书签 | `d` | 删除书签 |
| `*` | 标星 / 取消 | `s` / `S` | 推送 / 拉取 Gist |
| `c` | 配置编辑器 | `u` | 检查更新 |
| `L` | 切换 EN / 中文 | `/` | 过滤 |
| `j` / `k` `↑` / `↓` | 导航 | `h` / `l` `←` / `→` | 翻页 |
| `?` | 帮助 | `q` | 退出 |

**编辑器快捷键：** `Ctrl+S` 保存、`Ctrl+R` 显示/隐藏敏感字段、`Ctrl+Y` 复制字段、`Ctrl+U` 清空字段、`Ctrl+V` 粘贴、`Ctrl+B` 导出配置。

### 许可证

[MIT](LICENSE) © 2025 [vst](https://github.com/vst93)
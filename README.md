![](https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/screenshot/1.png)
![](https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/screenshot/2.png)

# TTM — Tiny Terminal Manager

Lightweight SSH bookmark manager built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

Manage, sync and connect to your servers — all from the terminal.

## Features

- **Full TUI** — no config files to hand-edit, everything is in the interface
- **SSH connect** — password, private key, or keyboard-interactive auth
- **Bookmark management** — add, edit, delete, star/favorite
- **Gist sync** — push/pull bookmarks via GitHub or Gitee Gist
- **In-app config** — set token and gist ID without leaving the TUI
- **Filter & paginate** — fuzzy search across bookmarks
- **Private key input modes** — paste key text or provide a key file path (resolved on save)
- **Clipboard-aware copy** — `Ctrl+Y` supports system clipboard with terminal fallback where applicable
- **i18n** — English / 中文, toggle with `L`
- **Update check** — press `u` to check for new releases
- **Cross-platform** — macOS, Linux, Windows, Android (Termux)

## Install

### Homebrew (macOS / Linux)

```bash
brew install vst93/tap/ttm
```

### Shell script (macOS / Linux)

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

Show installer options:

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh --help
```

Custom install dir (optional):

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && INSTALL_DIR="$HOME/.local/bin" bash install.sh
```

### Windows

Download the latest `ttm-windows-*.zip` from [GitHub Releases](https://github.com/vst93/ttm/releases), extract `ttm.exe`, and add its directory to your `PATH`.

Or build from source (requires [Go](https://go.dev/dl/)):

```powershell
git clone https://github.com/vst93/ttm.git && cd ttm
```

```powershell
go build -o ttm.exe .
```

```powershell
.\ttm.exe
```

### Termux (Android)

```bash
pkg update && pkg install curl unzip
```

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

> - Default install path: `$PREFIX/bin`
> - `install.sh` removes itself after execution (keep it with `AUTO_DELETE_INSTALL_SCRIPT=0`)
> - Checksum failure in non-interactive mode aborts by default; force with `FORCE_INSTALL=1` only when you trust the source

## Keybindings

| Key | Action |
|-----|--------|
| `Enter` | Connect |
| `a` | Add bookmark |
| `e` | Edit bookmark |
| `d` | Delete bookmark |
| `*` | Star / unstar |
| `s` | Push to Gist |
| `S` | Pull from Gist |
| `c` | Open config editor |
| `u` | Check for updates |
| `L` | Toggle EN / 中文 |
| `/` | Filter |
| `j/k` `↑/↓` | Navigate |
| `h/l` `←/→` | Page |
| `?` | Help |
| `q` | Quit |

### Editor Shortcuts (Bookmark / Config)

| Key | Action |
|-----|--------|
| `Ctrl+S` | Save editor form |
| `Ctrl+R` | Reveal / hide secret fields |
| `Ctrl+Y` | Copy focused field |
| `Ctrl+U` | Clear focused field |
| `Ctrl+V` | Paste from clipboard (Private Key field supports multiline paste) |

### Private Key Path Input

- In bookmark editor, `Private Key` accepts either raw key text or a file path.
- On save, if the value looks like a path and file reading succeeds, TTM stores the file content.
- If file reading fails (unreadable/path is directory/too large), TTM keeps the original path text and shows a clear tip.
- Current size guard for path-loaded key files: up to `256KB`.

### Clipboard Notes (SSH / tmux / terminal)

- In some remote terminal sessions, clipboard behavior depends on terminal support.
- If terminal clipboard fallback is used, TTM will indicate it in the success tip.
- If copy cannot reach a usable clipboard backend, TTM returns an explicit failure instead of reporting false success.
- tmux tip: enable clipboard passthrough (for example `set -g set-clipboard on`) in your tmux config.

## Gist Sync

Bookmarks can be synced across machines via a private Gist.

1. Press `c` to open the config editor
2. Select platform — `github` or `gitee`
3. Enter your Personal Access Token (needs `gist` scope)
   - [GitHub Tokens](https://github.com/settings/tokens) · [Gitee Tokens](https://gitee.com/personal_access_tokens)
4. Enter Gist ID (leave empty to auto-create on first push)
5. `Ctrl+S` to save

Then `s` to push, `S` to pull.

## Config

| OS | Path |
|----|------|
| Linux | `~/.config/ttm/` |
| macOS | `~/Library/Application Support/ttm/` |
| Windows | `%APPDATA%\ttm\` |

```
config.json      # platform, token, gist_id, locale
bookmarks.json   # bookmark entries
```

## Build from source

```bash
git clone https://github.com/vst93/ttm.git && cd ttm
```

```bash
make build
```

```bash
./ttm
```

> You can also run `go build -o ttm .` directly without Make.

## License

[MIT](LICENSE)

---

# TTM — Tiny Terminal Manager（中文）

轻量级终端 SSH 书签管理器，基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 构建。

在终端中管理、同步和连接你的服务器。

## 特性

- **全 TUI 界面** — 无需手动编辑配置文件，所有操作均在界面内完成
- **SSH 连接** — 支持密码、私钥、键盘交互三种认证方式
- **书签管理** — 新增、编辑、删除、收藏
- **Gist 同步** — 通过 GitHub 或 Gitee Gist 推送 / 拉取书签
- **应用内配置** — 无需离开 TUI 即可设置 Token 和 Gist ID
- **搜索与分页** — 模糊过滤书签列表
- **私钥输入模式** — 支持粘贴私钥文本或填写私钥文件路径（保存时解析）
- **剪贴板感知复制** — `Ctrl+Y` 优先系统剪贴板，并在可用时回退终端通道
- **双语** — English / 中文，按 `L` 切换
- **版本检查** — 按 `u` 检查新版本
- **跨平台** — macOS、Linux、Windows、Android (Termux)

## 安装

### Homebrew (macOS / Linux)

```bash
brew install vst93/tap/ttm
```

### 脚本安装（macOS / Linux）

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

查看安装脚本参数：

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh --help
```

自定义安装目录（可选）：

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && INSTALL_DIR="$HOME/.local/bin" bash install.sh
```

### Windows

从 [GitHub Releases](https://github.com/vst93/ttm/releases) 下载最新的 `ttm-windows-*.zip`，解压 `ttm.exe`，将其所在目录添加到 `PATH` 即可使用。

或从源码构建（需要 [Go](https://go.dev/dl/)）：

```powershell
git clone https://github.com/vst93/ttm.git && cd ttm
```

```powershell
go build -o ttm.exe .
```

```powershell
.\ttm.exe
```

### Termux（Android）

```bash
pkg update && pkg install curl unzip
```

```bash
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh
```

> - 默认安装目录：`$PREFIX/bin`
> - `install.sh` 执行后自动删除（如需保留可设置 `AUTO_DELETE_INSTALL_SCRIPT=0`）
> - 非交互环境下校验失败默认中止；仅在确认来源可信时使用 `FORCE_INSTALL=1` 强制继续

## 快捷键

| 按键 | 功能 |
|------|------|
| `Enter` | 连接 |
| `a` | 新增书签 |
| `e` | 编辑书签 |
| `d` | 删除书签 |
| `*` | 标星 / 取消标星 |
| `s` | 推送到 Gist |
| `S` | 从 Gist 拉取 |
| `c` | 打开配置编辑器 |
| `u` | 检查更新 |
| `L` | 切换 EN / 中文 |
| `/` | 过滤 |
| `j/k` `↑/↓` | 上下导航 |
| `h/l` `←/→` | 翻页 |
| `?` | 帮助 |
| `q` | 退出 |

### 编辑器快捷键（书签 / 配置）

| 按键 | 功能 |
|------|------|
| `Ctrl+S` | 保存编辑内容 |
| `Ctrl+R` | 显示 / 隐藏敏感字段 |
| `Ctrl+Y` | 复制当前字段 |
| `Ctrl+U` | 清空当前字段 |
| `Ctrl+V` | 从剪贴板粘贴（Private Key 字段支持多行） |

### Private Key 路径输入

- 在书签编辑器中，`Private Key` 既可填写私钥文本，也可填写文件路径。
- 保存时，如果输入看起来是路径且读取成功，TTM 会将文件内容写入私钥字段。
- 如果读取失败（不可读/是目录/文件过大），TTM 会保留原路径文本并给出明确提示。
- 当前路径读取大小限制：`256KB`。

### 剪贴板说明（SSH / tmux / 终端）

- 在部分远端终端环境中，剪贴板行为受终端能力影响。
- 当使用终端剪贴板回退通道时，TTM 会在成功提示中明确标注。
- 若无法写入可用剪贴板通道，TTM 会明确报错，不再出现“提示成功但实际为空白”。
- tmux 建议：在配置中开启剪贴板透传（例如 `set -g set-clipboard on`）。

## Gist 同步

书签可通过私有 Gist 在多台设备间同步。

1. 按 `c` 打开配置编辑器
2. 选择平台 — `github` 或 `gitee`
3. 输入 Personal Access Token（需要 `gist` 权限）
   - [GitHub Tokens](https://github.com/settings/tokens) · [Gitee Tokens](https://gitee.com/personal_access_tokens)
4. 输入 Gist ID（留空则首次推送时自动创建）
5. `Ctrl+S` 保存

之后按 `s` 推送，`S` 拉取。

## 配置文件

| 系统 | 路径 |
|------|------|
| Linux | `~/.config/ttm/` |
| macOS | `~/Library/Application Support/ttm/` |
| Windows | `%APPDATA%\ttm\` |

```
config.json      # 平台、令牌、Gist ID、语言
bookmarks.json   # 书签数据
```

## 从源码构建

```bash
git clone https://github.com/vst93/ttm.git && cd ttm
```

```bash
make build
```

```bash
./ttm
```

> 也可以直接运行 `go build -o ttm .`，无需 Make。

## 许可证

[MIT](LICENSE)

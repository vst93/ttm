# TTM — Tiny Terminal Manager

Lightweight SSH bookmark manager built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

Manage, sync and connect to your servers from the terminal.

## Features

- Full TUI — no config files to hand-edit, everything is in the interface
- SSH connect with password, private key, or keyboard-interactive auth
- Bookmark CRUD — add, edit, delete, star/favorite
- Gist sync — push/pull bookmarks via GitHub or Gitee Gist
- In-app config — set token and gist ID without leaving the TUI
- Filter & paginate — fuzzy search across bookmarks
- i18n — English / 中文, toggle with `L`
- Update check — press `u` to check for new releases
- Cross-platform — macOS, Linux, Windows

## Install

```bash
# Homebrew
brew install vst93/tap/ttm

# Shell script (recommended: review before run)
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh
bash install.sh
rm -f install.sh

# Show installer options
bash install.sh --help

# Custom install dir (optional)
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh
INSTALL_DIR="$HOME/.local/bin" bash install.sh
rm -f install.sh

# 查看安装脚本参数
bash install.sh --help
```

### Termux (Android)

```bash
pkg update
pkg install curl unzip
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh
bash install.sh
rm -f install.sh
```

- Default install path on Termux: `$PREFIX/bin`
- If checksum fetch/verify fails in non-interactive mode, installer aborts by default
- You can force continue only when you trust the source: `FORCE_INSTALL=1 ...`

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
| `c` | Sync config |
| `u` | Check for updates |
| `L` | Toggle EN / 中文 |
| `/` | Filter |
| `j/k` `↑/↓` | Navigate |
| `h/l` `←/→` | Page |
| `?` | Help |
| `q` | Quit |

## Gist Sync

Bookmarks can be synced across machines via a private Gist.

1. Press `c` to open the config editor
2. Select platform (`github` / `gitee`)
3. Enter your Personal Access Token (needs `gist` scope)
   - GitHub: https://github.com/settings/tokens
   - Gitee: https://gitee.com/personal_access_tokens
4. Enter Gist ID (optional — leave empty to auto-create on first push)
5. `Ctrl+S` to save

Then `s` to push, `S` to pull.

## Config

Stored in `~/.config/ttm/` (macOS: `~/Library/Application Support/ttm/`):

```
config.json      # platform, token, gist_id, locale
bookmarks.json   # bookmark entries
```

## Build from source

```bash
git clone https://github.com/vst93/ttm.git && cd ttm
make build    # or: go build -o ttm .
./ttm
```

## License

[MIT](LICENSE)

---

# TTM — Tiny Terminal Manager（中文）

轻量级终端 SSH 书签管理器，基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 构建。

在终端中管理、同步和连接你的服务器。

## 特性

- 全 TUI 界面 — 无需手动编辑配置文件，所有操作均在界面内完成
- SSH 连接 — 支持密码、私钥、键盘交互三种认证方式
- 书签管理 — 新增、编辑、删除、收藏
- Gist 同步 — 通过 GitHub 或 Gitee Gist 推送 / 拉取书签
- 应用内配置 — 无需离开 TUI 即可设置 Token 和 Gist ID
- 搜索与分页 — 模糊过滤书签列表
- 双语 — English / 中文，按 `L` 切换
- 版本检查 — 按 `u` 检查新版本
- 跨平台 — macOS、Linux、Windows

## 安装

```bash
# Homebrew
brew install vst93/tap/ttm

# 脚本安装
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh
bash install.sh
rm -f install.sh

# 可选：自定义安装目录
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh
INSTALL_DIR="$HOME/.local/bin" bash install.sh
rm -f install.sh
```

### Termux（Android）

```bash
pkg update
pkg install curl unzip
curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh
bash install.sh
rm -f install.sh
```

- Termux 默认安装目录为 `$PREFIX/bin`
- 在非交互环境中，如果校验信息获取失败或 SHA256 不匹配，脚本默认中止
- 仅在确认来源可信时才强制继续：`FORCE_INSTALL=1 ...`

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
| `c` | 同步配置 |
| `u` | 检查更新 |
| `L` | 切换 EN / 中文 |
| `/` | 过滤 |
| `j/k` `↑/↓` | 上下导航 |
| `h/l` `←/→` | 翻页 |
| `?` | 帮助 |
| `q` | 退出 |

## Gist 同步

书签可通过私有 Gist 在多台设备间同步。

1. 按 `c` 打开配置编辑器
2. 选择平台（`github` / `gitee`）
3. 输入 Personal Access Token（需要 `gist` 权限）
   - GitHub：https://github.com/settings/tokens
   - Gitee：https://gitee.com/personal_access_tokens
4. 输入 Gist ID（可选 — 留空则首次推送时自动创建）
5. `Ctrl+S` 保存

之后按 `s` 推送，`S` 拉取。

## 配置文件

存储于 `~/.config/ttm/`（macOS：`~/Library/Application Support/ttm/`）：

```
config.json      # 平台、令牌、Gist ID、语言
bookmarks.json   # 书签数据
```

## 从源码构建

```bash
git clone https://github.com/vst93/ttm.git && cd ttm
make build    # 或：go build -o ttm .
./ttm
```

## 许可证

[MIT](LICENSE)

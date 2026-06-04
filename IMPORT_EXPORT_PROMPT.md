# 配置导入导出功能实现 Prompt

## 功能概述

在 TUI 应用中实现配置的导出（一键复制导入命令）和导入（CLI 解码 + 确认写入）功能。

## 需求

### 导出（TUI 内）

在配置编辑页面增加快捷键（例如 `^B`），实现：
1. 读取 **已保存的配置**（而非编辑器中未保存的修改），序列化为 JSON
2. Base64 编码
3. 构建一条 CLI 导入命令：`<app> --import-config <base64编码>`
4. 将完整命令写入剪贴板（系统剪贴板优先，OSC52 回退）
5. 在 TUI 中弹出一个居中覆盖层弹窗，展示：
   - 标题：配置已导出 ✓
   - 复制目标（剪贴板 / 终端剪贴板）
   - 导入命令全文（深色背景框）
   - 操作说明：分享到其他机器运行即可导入
   - 安全警告：命令包含敏感凭据，勿泄露
   - 关闭快捷键（esc / enter / q / y）

### 导入（CLI）

CLI 层面提供 `--import-config <base64>` 参数，实现：
1. 接收一个 base64 编码字符串作为参数
2. Base64 解码 → JSON 解析
3. 在终端输出清晰的表格展示配置详情：
   - 各字段名称和值
   - 敏感字段（如 token）进行**掩码显示**（仅显示首尾，中间用 `*` 替代）
   - 空字段友好提示（"(empty)"、"(auto-create)" 等）
4. 如果包含敏感凭据，附加警告提示
5. 询问用户确认：`Import this config? [y/N]`
6. 用户输入 `y`/`yes` 才执行写入，否则取消
7. 写入成功后输出成功信息

## 实现要点

### 导出侧（TUI 代码）

**数据结构：**
```go
type configExportState struct {
    importCmd string // 完整的导入命令
    copiedTo  string // "clipboard" 或 "terminal clipboard"（用于界面文案）
}
```

**AppModel 中新增字段：**
```go
pendingConfigExport *configExportState
```

**导出函数逻辑（快捷键触发）：**
1. 取已保存的配置快照（编辑器打开时的原始值拷贝 `editor.original`，或直接读持久化配置）
2. `json.Marshal(config)` → `base64.StdEncoding.EncodeToString(jsonBytes)` → 构建 `"<app> --import-config " + encoded`
3. 调用 `writeTextToClipboard(importCmd)` 写入剪贴板
4. 设置 `AM.pendingConfigExport = &configExportState{...}`，返回 `nil`（不显示 tip）

**Update 按键分派：**
- 在检查 `configEditor != nil` 之前先检查 `pendingConfigExport != nil`
- 进入专用按键处理器，监听到 `esc` / `enter` / `q` / `y` 将 `pendingConfigExport` 置为 `nil`

**View 渲染：**
- 在 `configEditor != nil` 分支内部，先检查 `pendingConfigExport`：
  - 如果有：将 configEditor 内容 `dimBaseForOverlay` 变暗，居中叠加导出弹窗
  - 否则：正常渲染 configEditor + tip
- 弹窗内容使用 `lipgloss.JoinVertical` 组装，外框 `RoundedBorder` + 绿色或蓝色边框

### 导入侧（CLI main.go）

**CLI 入口：**
```go
if len(os.Args) > 2 && os.Args[1] == "--import-config" {
    importConfig(os.Args[2])
    return
}
```

**`importConfig` 函数：**
1. Base64 解码 + JSON 反序列化
2. 打印分隔线和表头
3. 逐字段打印，敏感字段通过 `maskToken()` 处理
4. 空字段显示友好占位
5. 含 Token 时附加警告
6. 读取 stdin 确认（`bufio.NewReader(os.Stdin).ReadString('\n')`）
7. 仅当输入 `y`/`yes` 时调用保存函数，否则取消

**Token 掩码函数：**
```go
func maskToken(token string) string {
    if len(token) <= 6 {
        return strings.Repeat("*", len(token))
    }
    return token[:4] + strings.Repeat("*", len(token)-7) + token[len(token)-3:]
}
```

### 剪贴板写入（复用现有基础设施）

使用 `atotto/clipboard` 库写入系统剪贴板，失败时回退到 OSC52 终端剪贴板序列（SSH/Tmux 环境）。对外暴露统一函数：
```go
func writeTextToClipboard(text string) (usedOSC52Only bool, err error)
```

## 文件结构参考

| 文件 | 职责 |
|------|------|
| `main.go` | CLI 入口，解析 `--import-config`，调用 `importConfig()` |
| `server/config_editor.go` | 导出快捷键响应、exportState 定义、弹窗渲染、弹窗按键处理 |
| `server/app_model.go` | 新增 `pendingConfigExport` 字段、Update/View 中集成 |
| `server/clipboard.go` | 剪贴板写入函数（系统剪贴板 + OSC52 回退） |
# DLSS5 一键开启工具

DLSS5 一键开启工具（Go 原生版）是一个用于在 Windows 平台上将 DLSS5 功能注入到游戏/应用的辅助工具。本仓库为工具的 Go 实现，默认运行原生 Win32 GUI（基于 github.com/lxn/walk），同时支持静默命令行安装/卸载。

本 README 已基于仓库源码生成，包含准确的命令行参数、运行行为、日志位置与常见注意事项。

---

## 关键特性（从源码提取）

- Windows 原生 GUI（lxn/walk）；同时支持静默命令行 -install / -uninstall
- 环境体检（GPU / 驱动 / 组件完整性）
- 多路由安装引擎（支持多种图形 API / DXVK / ReShade 配合）
- 组件获取链路：cache → components（手动投放）→ 本机扫描 → 在线镜像
- 可生成清单（dlss5-oneclick.manifest.json）并支持零残留卸载

---

## 要求与依赖

- 操作系统：Windows（GUI 使用 lxn/walk，源码针对 Windows 系统调用）
- Go 1.20+（用于本地编译）
- PowerShell 可用（若需要提权写入系统文件）
- 推荐 GPU：NVIDIA RTX 系列，Driver >= 570（源码中以 570 作为最低判断）
- 如果使用 GUI，需在 Windows 下构建并运行（交叉编译或在非 Windows 平台运行 GUI 会失败）

第三方库（示例）：
- github.com/lxn/walk （用于 Win32 GUI）

---

## 快速开始（构建与运行）

1. 克隆仓库：

```bash
git clone https://github.com/metal-vest/DLSS5.git
cd DLSS5
```

2. 构建（在 Windows 上，启用 Go Modules）：

```bash
# 在仓库根目录构建可执行文件
go build -o DLSS5.exe .
```

3. 运行：

- 启动 GUI（默认行为）：

```powershell
.\DLSS5.exe
```

- 静默模式（命令行安装/卸载）—— 源码中的支持参数：

```powershell
# 静默安装到指定游戏 exe（示例）
DLSS5.exe -install "C:\Games\YourGame\Game.exe"

# 静默卸载（从指定游戏 exe 卸载）
DLSS5.exe -uninstall "C:\Games\YourGame\Game.exe"
```

注意：静默安装流程会检测目标 exe 是否存在、分析目标游戏（位数/API/是否已有 ReShade 等）、收集所需组件并进行部署；失败时会写入错误信息并以非零状态码退出（见下文 Exit Code）。

---

## 命令行行为与退出码

- 支持参数：`-install <path>`、`-uninstall <path>`（使用 `flag` 包解析，参数名如上）
- 如果在静默安装时目标文件不存在，程序会以退出码 2 退出。
- 如果组件缺失（无法获取所需组件）会以退出码 3 退出。
- 其他安装或卸载失败通常以退出码 1 退出；成功则退出码为 0。

示例（伪流程）：
- DLSS5.exe -install "C:\Games\Game.exe"
  - 若成功：输出“静默安装完成”，退出码 0
  - 若游戏 exe 不存在：输出错误并退出 2
  - 若某些必需组件缺失：输出错误并退出 3
  - 若安装过程发生错误：输出错误并退出 1

---

## GUI 功能概览（窗口中的按钮与行为）

- ① 选择游戏：输入或拖拽游戏主程序（.exe）路径
- 环境体检（按钮）：运行一系列检查（显卡、驱动、组件完整性等），并以醒目方式展示通过/警告/失败项
- 一键开启（按钮）：按仓库实现的“安装引擎”逻辑将组件写入游戏目录并生成清单
- 一键卸载（按钮）：根据游戏目录下的清单（dlss5-oneclick.manifest.json）进行清理，尽量做到零残留
- 记录区：工具会产生日志并在 UI 中实时追加
- 选项：是否创建桌面/启动器快捷方式（UI 中有复选框）

---

## 重要文件与目录（运行时）

- 根目录（exe 所在目录），若不可写则回退到 %LOCALAPPDATA%\DLSS5Feeder
- cache/ — 下载缓存
- components/ — 手动投放组件目录（当自动获取失败时，要求用户把组件放在此处）
- logs/ — 日志目录，当前日志名样例：oneclick-20060102-150405.log（格式由源码生成）
- 清单文件：`dlss5-oneclick.manifest.json`（写入到目标游戏目录，用于卸载与回滚）

---

## ReShade 与组件相关注意事项

- 源码在安装前会检测目标目录是否已有 ReShade（探测 dxgi.dll/d3d11.dll/d3d12.dll/d3d9.dll/opengl32.dll 等），若已有 ReShade 且版本 >= 6.8，则会跳过本体安装并尽量复用已有组件。
- 若已有 ReShade 但版本 < 6.8，会在 UI 中给出警告并建议先卸载旧版。
- Vulkan 路由会尝试把游戏登记到 ProgramData\ReShade\ReShadeApps.ini（需要管理员权限），代码会备份并追加记录。

---

## 日志与调试

- 程序会在 logs/ 下写入运行日志，UI 中也会实时显示（仅限 GUI）。
- 若静默安装失败，请检查日志文件并在 issues 中附上日志片段以便排查。

---

## 开发与贡献

- 本项目使用 Go 及部分 Windows API，若要修改 GUI 代码请在 Windows 环境下进行测试。
- 代码风格与常见步骤：
  - 格式化：gofmt -w .
  - 静态检查：go vet ./...
  - 单元测试：go test ./...

如果你希望我自动为仓库添加 CONTRIBUTING.md、ISSUE 模板或 LICENSE（例如 MIT），我可以继续为你创建这些文件。

---

## 联系 / 作者

仓库：metal-vest/DLSS5

---

(文档基于仓库当前源码自动生成并已写入 README.md。若需要我将 README 做成中英双语、补充示意图或根据特定发行版生成可执行二进制打包说明，我可以继续替你更新。)

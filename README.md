# DLSS5 一键开启工具

DLSS5 一键开启工具（Go 原生版），用于在 Windows 平台将 DLSS5 功能注入到游戏/应用。

## 特性

- Windows 原生 GUI（基于 lxn/walk），支持静默命令行 -install / -uninstall
- 环境检测（GPU / 驱动 / 组件完整性）
- 多路由安装引擎（支持多种图形 API，兼容 ReShade/DXVK 等）
- 支持缓存与本地组件目录（cache/、components/），并能生成清单用于卸载

## 要求

- 操作系统：Windows
- Go 1.20+（用于本地编译）
- 推荐 GPU：NVIDIA RTX，Driver >= 570

## 快速开始

1. 克隆仓库并进入目录：

```bash
git clone https://github.com/metal-vest/DLSS5.git
cd DLSS5
```

2. 在 Windows 上构建：

```bash
go build -o DLSS5.exe .
```

3. 运行：

- 启动 GUI：

```powershell
.\DLSS5.exe
```

- 静默模式示例：

```powershell
# 静默安装到指定游戏 exe
DLSS5.exe -install "C:\Games\YourGame\Game.exe"

# 静默卸载
DLSS5.exe -uninstall "C:\Games\YourGame\Game.exe"
```

## 命令行与退出码

- 支持参数：`-install <path>`、`-uninstall <path>`（使用 `flag` 包解析）
- 退出码说明：
  - 0：成功
  - 1：安装或卸载错误
  - 2：目标文件不存在
  - 3：组件缺失（无法获取所需组件）

## 运行时文件与日志

- 根目录（exe 所在目录），不可写时回退到 %LOCALAPPDATA%\DLSS5Feeder
- cache/ — 下载缓存
- components/ — 手动投放组件目录
- logs/ — 日志目录（如 oneclick-20060102-150405.log）
- 清单文件：`dlss5-oneclick.manifest.json`（写入目标游戏目录，用于卸载）

## 调试

静默安装失败时请查看 logs/ 下的日志文件，必要时在 issues 中附上日志片段以便排查。

## 开发

- 格式化：`gofmt -w .`
- 静态检查：`go vet ./...`
- 单元测试：`go test ./...`

如果需要，我可以为仓库添加 CONTRIBUTING.md、ISSUE 模板或 LICENSE。
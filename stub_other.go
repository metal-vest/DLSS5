//go:build !windows

// stub_other.go — 非 Windows 平台的空实现。
// 图形界面（lxn/walk）与 version.dll 版本探测均为 Windows 专属能力；
// 提供空实现使 go test ./... 与 go vet ./... 可在 Linux/macOS 上执行
// （本地回归与 CI 质量门禁）。Windows 构建不受影响：
// ui.go 与 winver.go 由 //go:build windows 排除，本文件在 Windows 上不参与编译。
package main

import (
	"fmt"
	"os"
)

func runGUI() {
	fmt.Fprintln(os.Stderr, "图形界面仅支持 Windows。其他平台请使用命令行：")
	fmt.Fprintln(os.Stderr, "  本工具 -install <游戏exe路径>   # 静默安装")
	fmt.Fprintln(os.Stderr, "  本工具 -uninstall <游戏exe路径> # 静默卸载")
	os.Exit(2)
}

func isReShadeDll(dllPath string) (bool, string, int, int, int) {
	return false, "", 0, 0, 0
}

func isDxvkDll(dllPath string) bool {
	return false
}

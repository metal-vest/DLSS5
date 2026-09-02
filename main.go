// main.go — DLSS5 一键开启工具（Go 原生版）· 入口
// 默认启动图形界面；也支持静默命令行：
//
//	DLSS5一键开启工具.exe -install  "C:\Games\Game.exe"
//	DLSS5一键开启工具.exe -uninstall "C:\Games\Game.exe"
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	exePath, _ := os.Executable()
	initDirs(filepath.Dir(exePath))

	silentInstall := flag.String("install", "", "静默安装到指定游戏 exe")
	silentUninstall := flag.String("uninstall", "", "从指定游戏 exe 静默卸载")
	flag.Parse()

	if *silentUninstall != "" {
		n, err := uninstallGame(*silentUninstall)
		if err != nil {
			fmt.Fprintln(os.Stderr, "卸载失败: "+err.Error())
			os.Exit(1)
		}
		fmt.Printf("卸载完成，删除 %d 项\n", n)
		return
	}
	if *silentInstall != "" {
		if !fileExists(*silentInstall) {
			fmt.Fprintln(os.Stderr, "游戏 exe 不存在: "+*silentInstall)
			os.Exit(2)
		}
		a := analyzeGame(*silentInstall)
		fmt.Printf("目标: %s · 路由 [%s]\n", a.GameName, a.Route)
		c := getAllComponents()
		if len(c.Missing) > 0 {
			fmt.Fprintln(os.Stderr, "组件缺失: "+fmt.Sprint(c.Missing))
			os.Exit(3)
		}
		if _, err := installGameRoute(a, c); err != nil {
			fmt.Fprintln(os.Stderr, "安装失败: "+err.Error())
			os.Exit(1)
		}
		fmt.Println("静默安装完成")
		return
	}

	runGUI()
}

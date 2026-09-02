// analyze.go — 游戏目标分析：位数 / 图形 API / DXVK / D3D9 判定
// 判定策略参照 DLSS5-Feeder 的 DEPLOY-DEV.md 第 0/7 节（移植自 analyze.ps1）
package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type analysis struct {
	ExePath        string
	ExeDir         string
	GameName       string
	Bitness        string // "32" | "64" | "unknown"
	Api            string // d3d11 | d3d12 | d3d9 | vulkan | opengl | unknown
	Route          string // d3d64 | d3d32 | d3d9 | vulkan | vulkan32 | opengl64 | opengl32
	HasDxvk        bool
	HasReShade     bool
	ReShadeVersion string
	ReShadeDllName string
	Notes          []string
	Warnings       []string
	PeValid        bool
}

var reshadeProbeDlls = []string{"dxgi.dll", "d3d11.dll", "d3d12.dll", "d3d9.dll", "d3d8.dll", "opengl32.dll"}

func analyzeGame(exePath string) *analysis {
	a := &analysis{
		ExePath:  exePath,
		ExeDir:   filepath.Dir(exePath),
		GameName: strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath)),
		Bitness:  "unknown",
		Api:      "unknown",
		Route:    "unknown",
	}
	if !fileExists(exePath) {
		a.Warnings = append(a.Warnings, "游戏 exe 不存在")
		return a
	}
	pe := parsePe(exePath)
	a.Bitness = pe.Bitness
	a.PeValid = pe.Valid
	dir := a.ExeDir

	// ---- 已装 ReShade 检测 ----
	for _, dll := range reshadeProbeDlls {
		ok, ver, maj, min, _ := isReShadeDll(filepath.Join(dir, dll))
		if ok {
			a.HasReShade = true
			a.ReShadeVersion = ver
			a.ReShadeDllName = dll
			if maj < 6 || (maj == 6 && min < 8) {
				a.Warnings = append(a.Warnings, fmt.Sprintf("已有 ReShade %s 过旧（需 ≥6.8，否则加载附加组件会静默失败），建议先卸载旧版", ver))
			} else {
				a.Notes = append(a.Notes, fmt.Sprintf("已有 ReShade %s，将复用并只补齐组件", ver))
			}
			break
		}
	}

	// ---- DXVK 检测（32 位 Vulkan ≈ DXVK）----
	for _, dll := range []string{"dxgi.dll", "d3d9.dll", "d3d11.dll", "d3d10.dll"} {
		if isDxvkDll(filepath.Join(dir, dll)) {
			a.HasDxvk = true
			break
		}
	}

	// ---- API 判定 ----
	imp := pe.Imports
	is64 := pe.Bitness == "64"
	has := func(name string) bool {
		for _, v := range imp {
			if v == name {
				return true
			}
		}
		return false
	}
	switch {
	case is64 && has("vulkan-1.dll"):
		a.Api = "vulkan"
	case is64 && has("opengl32.dll"):
		a.Api = "opengl"
	case is64 && has("d3d12.dll"):
		a.Api = "d3d12"
	case is64 && has("d3d11.dll"):
		a.Api = "d3d11"
	case is64 && has("d3d10.dll"):
		a.Api = "d3d11" // D3D10 与 11/12 同走 dxgi
	case is64:
		a.Api = "d3d12" // 64 位默认按 dxgi 路由
	case has("vulkan-1.dll"):
		a.Api = "vulkan"
	case has("opengl32.dll"):
		a.Api = "opengl"
	case has("d3d11.dll"):
		a.Api = "d3d11"
	case has("d3d12.dll"):
		a.Api = "d3d12"
	case has("d3d9.dll") || has("d3d8.dll"):
		a.Api = "d3d9"
	case pe.IsUe3Era:
		a.Api = "d3d9"
		a.Notes = append(a.Notes, "PE 为旧子系统版本（≤5.00），按 D3D9 时代程序处理")
	default:
		a.Api = "unknown"
	}

	// 32 位 Vulkan：几乎必然 DXVK（DEPLOY-DEV 第 0 节）
	if !is64 && a.Api == "vulkan" && !a.HasDxvk {
		a.Notes = append(a.Notes, "32 位 Vulkan 但未检出 DXVK DLL，仍按 DXVK 路径处理")
		a.HasDxvk = true
	}

	// ---- 路由判定 ----
	switch a.Api {
	case "d3d11", "d3d12":
		if is64 {
			a.Route = "d3d64"
		} else {
			a.Route = "d3d32"
		}
	case "d3d9":
		a.Route = "d3d9" // 32 位 D3D9 → dgVoodoo2 + 32 位全链
	case "vulkan":
		if is64 {
			a.Route = "vulkan"
		} else {
			a.Route = "vulkan32"
		}
	case "opengl":
		if is64 {
			a.Route = "opengl64"
		} else {
			a.Route = "opengl32"
		}
	default:
		if is64 {
			a.Route = "d3d64"
		} else {
			a.Route = "d3d32"
		}
	}

	// ---- 冲突警告 ----
	if fileExists(filepath.Join(dir, "OptiScaler.dll")) {
		a.Warnings = append(a.Warnings, "检测到 OptiScaler（与 DLSS5-Feeder 不兼容，请先移除）")
	}
	if fileExists(filepath.Join(dir, "OptiScaler.asi")) {
		a.Warnings = append(a.Warnings, "检测到 OptiScaler.asi（与 DLSS5-Feeder 不兼容，请先移除）")
	}
	return a
}

// 把分析结果翻译成人话（GUI 展示用）
func routeDescription(a *analysis) string {
	bit := "未知位数"
	switch a.Bitness {
	case "64":
		bit = "64 位"
	case "32":
		bit = "32 位"
	}
	api := "未知 API"
	switch a.Api {
	case "d3d11":
		api = "Direct3D 11"
	case "d3d12":
		api = "Direct3D 12"
	case "d3d9":
		api = "Direct3D 9（需 dgVoodoo2 转译）"
	case "vulkan":
		if a.HasDxvk {
			api = "Vulkan（DXVK 转译）"
		} else {
			api = "Vulkan"
		}
	case "opengl":
		api = "OpenGL"
	}
	return fmt.Sprintf("%s · %s", bit, api)
}

// 已有 ReShade 是否满足 ≥6.8
func reshadeGoodEnough(a *analysis) bool {
	if !a.HasReShade {
		return false
	}
	var maj, min int
	if n, _ := fmt.Sscanf(a.ReShadeVersion, "%d.%d", &maj, &min); n != 2 {
		return false
	}
	return maj > 6 || (maj == 6 && min >= 8)
}

// envcheck.go — 环境体检：GPU / 驱动 / 系统 / 组件完整性 / 网络（移植自 envcheck.ps1）
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type envItem struct {
	Name   string
	Status string // 通过 | 警告 | 失败 | 信息 | 提示
	Detail string
	Ok     bool
}

type envReport struct {
	Items         []envItem
	GpuName       string
	DriverVersion string
	IsNvidia      bool
	Pass          bool
}

func getEnvironmentReport() *envReport {
	r := &envReport{Pass: true}
	add := func(name, status, detail string, ok bool) {
		r.Items = append(r.Items, envItem{name, status, detail, ok})
		if !ok {
			r.Pass = false
		}
	}

	// ---- 1. NVIDIA GPU ----
	gpuOut, err := runPS("(Get-CimInstance Win32_VideoController).Name | Out-String")
	var gpuName string
	if err == nil && gpuOut != "" {
		for _, line := range strings.Split(gpuOut, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && strings.Contains(line, "NVIDIA") {
				gpuName = line
				break
			}
		}
	}
	r.GpuName = gpuName
	if gpuName != "" {
		r.IsNvidia = true
		isRtx := strings.Contains(gpuName, "RTX")
		if isRtx {
			add("显卡", "通过", gpuName, true)
		} else {
			add("显卡", "警告", gpuName+"（非 RTX 系列，DLSS 需要 RTX 显卡）", false)
		}
	} else {
		add("显卡", "失败", "未检测到 NVIDIA GPU —— DLSS 仅能在 NVIDIA RTX 显卡上运行", false)
	}

	// ---- 2. 驱动版本（nvidia-smi）----
	smi := filepath.Join(os.Getenv("WINDIR"), "System32", "nvidia-smi.exe")
	if fileExists(smi) {
		// F-04：路径经环境变量传入，不拼进脚本字符串
		out, err := runPS("& $env:DLSS5_SMI --query-gpu=driver_version --format=csv,noheader | Select-Object -First 1", "DLSS5_SMI="+smi)
		if err == nil && out != "" {
			dv := strings.TrimSpace(strings.Split(out, "\n")[0])
			r.DriverVersion = dv
			numRe := regexp.MustCompile(`^(\d+)\.(\d+)`)
			m := numRe.FindStringSubmatch(dv)
			ok := false
			if m != nil {
				maj, _ := strconv.Atoi(m[1])
				ok = maj >= 570 // 2026 年 DLSS 5 时代驱动，57X 起步
			}
			if ok {
				add("NVIDIA 驱动", "通过", "版本 "+dv, true)
			} else {
				add("NVIDIA 驱动", "警告", "版本 "+dv+"（建议升级到 2026 年后的新驱动以获得 DLSS 5 支持）", false)
			}
		} else {
			add("NVIDIA 驱动", "警告", "nvidia-smi 运行失败，无法确认驱动版本", true)
		}
	} else {
		add("NVIDIA 驱动", "警告", "未找到 nvidia-smi（可能驱动未装全）", true)
	}

	// ---- 3. 操作系统 ----
	if osOut, err := runPS("$o=Get-CimInstance Win32_OperatingSystem; \"$($o.Caption)|$($o.BuildNumber)\""); err == nil && strings.Contains(osOut, "|") {
		parts := strings.SplitN(osOut, "|", 2)
		build, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		ok := build >= 17763 // Win10 1809+
		add("操作系统", statusOf(ok), strings.TrimSpace(parts[0])+fmt.Sprintf(" (Build %d)", build), ok)
	}

	// ---- 4. 冲突提示 ----
	add("兼容性提示", "提示", "若启用了 NVIDIA Smooth Motion 或 OptiScaler，请先关闭/移除 —— 与 DLSS5-Feeder 冲突", true)

	// ---- 5. components\ 手动投递目录 ----
	var compDetail []string
	for _, f := range []string{"renodx-dlss5.addon64", "nvngx_dlssnr.dll", "nvngx_dlss.dll"} {
		if fileExists(filepath.Join(compDir, f)) {
			compDetail = append(compDetail, f+" ✓")
		} else {
			compDetail = append(compDetail, f+" ✗")
		}
	}
	add("本地组件投递", "信息", strings.Join(compDetail, "  ·  "), true)

	// ---- 6. nvngx_dlss.dll 本机扫描（不阻塞）----
	local := findLocalNvNgx()
	if local != "" {
		add("DLSS 运行库", "通过", "本机扫描到: "+local, true)
	} else {
		add("DLSS 运行库", "信息", "本机未扫描到 nvngx_dlss.dll —— 不阻塞：NVIDIA 驱动自带运行库可兜底", true)
	}

	// ---- 7. 网络连通性 ----
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", "https://raw.githubusercontent.com/crosire/reshade-shaders/master/Shaders/ReShade.fxh", nil)
	req.Header.Set("User-Agent", "DLSS5-OneClick-Go")
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
		add("网络", "通过", "GitHub 下载链路可达", true)
	} else {
		add("网络", "警告", "GitHub 直连失败 —— 若持续失败请开加速器，或把组件手动放进 components\\ 目录", true)
	}
	return r
}

func statusOf(ok bool) string {
	if ok {
		return "通过"
	}
	return "警告"
}

func formatEnvReport(r *envReport) []string {
	var lines []string
	for _, it := range r.Items {
		lines = append(lines, fmt.Sprintf("[%s] %s：%s", it.Status, it.Name, it.Detail))
	}
	return lines
}

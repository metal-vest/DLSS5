// fetch.go — 组件获取引擎（移植自 fetch.ps1）
// 链路优先级：cache\ 缓存 → components\ 手动投递 → 本机已有游戏扫描 → 在线下载（社区镜像）
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type comps struct {
	Files        map[string]string // 逻辑名 → 磁盘路径
	LumeniteDir  string
	DgvRoot      string
	ReshadeSetup string
	Missing      []string
	Steps        []string
}

func newComps() *comps { return &comps{Files: map[string]string{}} }

func (c *comps) note(msg string) {
	c.Steps = append(c.Steps, msg)
	logLine("STEP", msg)
}

func (c *comps) need(what string) { c.Missing = append(c.Missing, what) }

var reshadeHomeRe = regexp.MustCompile(`href="(/downloads/ReShade_Setup_[\d\.]+_Addon\.exe)"`)
var reshadeHomeRe2 = regexp.MustCompile(`href="(/downloads/ReShade_Setup_[\d\.]+)\.exe"`)

func reshadeSetupURL() string {
	home := filepath.Join(cacheDir, "reshade_home.html")
	if download("https://reshade.me", home, 2, "reshade.me 首页") || fileExists(home) {
		if data, err := os.ReadFile(home); err == nil {
			if m := reshadeHomeRe.FindSubmatch(data); m != nil {
				return "https://reshade.me" + string(m[1])
			}
			if m := reshadeHomeRe2.FindSubmatch(data); m != nil {
				return "https://reshade.me" + string(m[1]) + ".exe"
			}
		}
	}
	return "https://reshade.me/downloads/ReShade_Setup_6.8.0_Addon.exe" // 已知可用回退
}

func dgVoodooURL() string {
	tag := latestTag("dege-diosg/dgVoodoo2")
	if tag == "" {
		tag = "v2.87.3"
	}
	url := githubAssetURL("dege-diosg/dgVoodoo2", tag,
		regexp.QuoteMeta("/dege-diosg/dgVoodoo2/releases/download/"+tag+"/")+"(dgVoodoo2_[\\d_]+\\.zip)")
	if url != "" {
		return url
	}
	return fmt.Sprintf("https://github.com/dege-diosg/dgVoodoo2/releases/download/%s/dgVoodoo2_87_3.zip", tag)
}

func rhiSetupURL() (url, asset, tag string) {
	tag = latestTag("RankFTW/RHI")
	if tag == "" {
		tag = "RHI-2.5.1"
	}
	asset = "RHI-Setup.exe"
	got := githubAssetURL("RankFTW/RHI", tag,
		regexp.QuoteMeta("/RankFTW/RHI/releases/download/"+tag+"/")+`([^"]+\.exe)`)
	if got != "" {
		asset = filepath.Base(strings.Split(got, "?")[0])
		return got, asset, tag
	}
	return fmt.Sprintf("https://github.com/RankFTW/RHI/releases/download/%s/RHI-Setup.exe", tag), asset, tag
}

// ============ 主入口：准备全部组件 ============
func getAllComponents() *comps {
	c := newComps()
	C := c.Files

	// ---------- 1. DLSS5-Feeder 本体 ----------
	c.note("【1/8】获取 DLSS5-Feeder 本体")
	feederZip := filepath.Join(cacheDir, "DLSS5-Feeder.zip")
	if !fileExists(feederZip) {
		tag := latestTag("jlrouzies-fr/DLSS5-Feeder")
		if tag == "" {
			tag = "v0.10.0-beta.2"
		}
		url := fmt.Sprintf("https://github.com/jlrouzies-fr/DLSS5-Feeder/releases/download/%s/DLSS5-Feeder-%s.zip", tag, tag)
		if !download(url, feederZip, 3, "DLSS5-Feeder "+tag) {
			c.need("dlss5-feed.addon64（Feeder 本体）")
		}
	}
	feederSrc := filepath.Join(cacheDir, "feeder_extracted")
	if fileExists(feederZip) && !fileExists(filepath.Join(feederSrc, "dlss5-feed.addon64")) {
		if err := expandZip(feederZip, feederSrc); err != nil {
			logf("ERROR", "Feeder zip 解压失败: %v", err)
		}
	}
	for _, f := range []string{"dlss5-feed.addon64", "dlss5-feed.addon32"} {
		p := filepath.Join(feederSrc, f)
		if fileExists(p) {
			C[f] = p
		} else {
			c.need(f)
		}
	}
	C["DLSS5_Feed.fx"] = filepath.Join(feederSrc, "reshade-shaders", "Shaders", "DLSS5_Feed.fx")
	C["host64exe"] = filepath.Join(feederSrc, "host64", "dlss5-feed-host64.exe")
	C["layer-x64"] = filepath.Join(feederSrc, "layer-x64")
	C["layer-x86"] = filepath.Join(feederSrc, "layer-x86")

	// ---------- 2. LumeniteFX 运动矢量着色器 ----------
	c.note("【2/8】获取 LumeniteFX Kernel（推荐运动矢量着色器）")
	lumZip := filepath.Join(cacheDir, "LumeniteFX.zip")
	lumSrc := filepath.Join(cacheDir, "lumenitefx_extracted")
	if !fileExists(lumZip) {
		download("https://codeload.github.com/umar-afzaal/LumeniteFX/zip/refs/heads/mainline", lumZip, 3, "LumeniteFX (mainline)")
	}
	if fileExists(lumZip) && !dirExists(lumSrc) {
		expandZip(lumZip, lumSrc)
	}
	hit := findFileRecursive(lumSrc, "lumenite_Kernel.fx", 5)
	if hit != "" {
		C["lumenite_dir"] = filepath.Dir(hit)
	} else {
		c.need("LumeniteFX 着色器（运动矢量提供者）")
	}

	// ---------- 3. ReShade 框架头文件 ----------
	c.note("【3/8】获取 ReShade 框架头文件（ReShade.fxh 等）")
	hdrDir := filepath.Join(cacheDir, "reshade_headers")
	ensureDir(hdrDir)
	for _, h := range []string{"ReShade.fxh", "ReShadeUI.fxh", "DrawText.fxh"} {
		hp := filepath.Join(hdrDir, h)
		if !fileExists(hp) {
			download("https://raw.githubusercontent.com/crosire/reshade-shaders/master/Shaders/"+h, hp, 2, h)
		}
		if fileExists(hp) {
			C["header_"+h] = hp
		} else {
			c.need(h)
		}
	}

	// ---------- 4. ReShade 安装器（Addon 变体） ----------
	c.note("【4/8】获取 ReShade 安装器（附加组件支持版）")
	rsURL := reshadeSetupURL()
	rsName := filepath.Base(strings.Split(rsURL, "?")[0])
	rsPath := filepath.Join(cacheDir, rsName)
	if download(rsURL, rsPath, 3, rsName) {
		C["reshade_setup"] = rsPath
	} else {
		c.need("ReShade 安装器")
	}

	// ---------- 5. Generic Depth 附加组件 ----------
	c.note("【5/8】获取 Generic Depth 附加组件（深度缓冲采集）")
	for _, arch := range []string{"addon64", "addon32"} {
		gp := filepath.Join(cacheDir, "generic_depth."+arch)
		if !fileExists(gp) {
			download("https://github.com/crosire/reshade-docs/releases/latest/download/generic_depth."+arch, gp, 2, "generic_depth."+arch)
		}
		if fileExists(gp) {
			C["generic_depth_"+arch] = gp
		}
	}

	// ---------- 6. renodx-dlss5.addon64 + nvngx_dlssnr.dll ----------
	c.note("【6/8】获取 renodx-dlss5.addon64 + nvngx_dlssnr.dll（DLSS 5 神经渲染附加组件）")
	gotRenodx, gotDlssnr := false, false
	if p := filepath.Join(compDir, "renodx-dlss5.addon64"); fileExists(p) {
		C["renodx-dlss5.addon64"] = p
		gotRenodx = true
		logf("OK", "  使用 components\\ 手动投递的 renodx-dlss5.addon64")
	}
	if p := filepath.Join(compDir, "nvngx_dlssnr.dll"); fileExists(p) {
		C["nvngx_dlssnr.dll"] = p
		gotDlssnr = true
		logf("OK", "  使用 components\\ 手动投递的 nvngx_dlssnr.dll")
	}
	if !(gotRenodx && gotDlssnr) {
		logLine("INFO", "  在本机已装游戏中扫描这两个文件…")
		for _, drv := range []string{"C:", "D:", "E:", "F:", "G:"} {
			common := drv + `\SteamLibrary\steamapps\common`
			if !dirExists(common) {
				continue
			}
			if !gotRenodx {
				if h := findFileRecursive(common, "renodx-dlss5.addon64", 3); h != "" {
					C["renodx-dlss5.addon64"] = h
					gotRenodx = true
					logf("OK", "  本机扫描到: %s", h)
				}
			}
			if !gotDlssnr {
				if h := findFileRecursive(common, "nvngx_dlssnr.dll", 3); h != "" {
					C["nvngx_dlssnr.dll"] = h
					gotDlssnr = true
					logf("OK", "  本机扫描到: %s", h)
				}
			}
			if gotRenodx && gotDlssnr {
				break
			}
		}
	}
	if !(gotRenodx && gotDlssnr) {
		logLine("INFO", "  从社区镜像（RankFTW/RHI）下载 RHI-Setup.exe 并静默解包…")
		rhiURL, rhiAsset, rhiTag := rhiSetupURL()
		rhiExe := filepath.Join(cacheDir, rhiAsset)
		if download(rhiURL, rhiExe, 3, rhiAsset+" ("+rhiTag+")") {
			rhiOut := filepath.Join(cacheDir, "rhi_extracted")
			if !fileExists(filepath.Join(rhiOut, "renodx-dlss5.addon64")) {
				code := runExe(rhiExe, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", `/DIR="`+rhiOut+`"`)
				if code != 0 {
					logf("WARN", "  RHI 静默解包退出码 %d", code)
				}
			}
			if !gotRenodx {
				if h := findFileRecursive(rhiOut, "renodx-dlss5.addon64", 5); h != "" {
					C["renodx-dlss5.addon64"] = h
					gotRenodx = true
				}
			}
			if !gotDlssnr {
				if h := findFileRecursive(rhiOut, "nvngx_dlssnr.dll", 5); h != "" {
					C["nvngx_dlssnr.dll"] = h
					gotDlssnr = true
				}
			}
		}
	}
	if !gotRenodx {
		c.need("renodx-dlss5.addon64（DLSS 5 神经渲染附加组件）")
	}
	if !gotDlssnr {
		c.need("nvngx_dlssnr.dll（DLSS 5 神经渲染运行库）")
	}

	// ---------- 7. nvngx_dlss.dll（DLSS 超分运行库，不阻塞） ----------
	c.note("【7/8】获取 nvngx_dlss.dll（DLSS 超分运行库）")
	if p := filepath.Join(compDir, "nvngx_dlss.dll"); fileExists(p) {
		C["nvngx_dlss.dll"] = p
		logf("OK", "  使用 components\\ 手动投递的 nvngx_dlss.dll")
	} else if hit := findLocalNvNgx(); hit != "" {
		C["nvngx_dlss.dll"] = hit
	} else {
		logf("WARN", "  未找到 nvngx_dlss.dll —— 不阻塞（NVIDIA 驱动自带运行库可兜底）")
	}

	// ---------- 8. dgVoodoo2（仅 D3D9 路径需要，先备好） ----------
	c.note("【8/8】获取 dgVoodoo2（D3D9 转译层，仅老游戏需要）")
	dgvZip := filepath.Join(cacheDir, "dgVoodoo2.zip")
	if !fileExists(dgvZip) {
		download(dgVoodooURL(), dgvZip, 3, "dgVoodoo2")
	}
	dgvSrc := filepath.Join(cacheDir, "dgvoodoo2_extracted")
	if fileExists(dgvZip) && !dirExists(dgvSrc) {
		expandZip(dgvZip, dgvSrc)
	}
	c.DgvRoot = dgvSrc
	return c
}

type compRow struct {
	Name   string
	Ok     bool
	Detail string
}

func componentSummary(c *comps) []compRow {
	C := c.Files
	has := func(k string) (bool, string) {
		p := C[k]
		return p != "" && fileExists(p), p
	}
	rows := []compRow{}
	ok, p := has("dlss5-feed.addon64")
	rows = append(rows, compRow{"DLSS5-Feeder 本体", ok, p})
	ok, p = has("lumenite_dir")
	rows = append(rows, compRow{"LumeniteFX 着色器", ok, c.Files["lumenite_dir"]})
	rows = append(rows, compRow{"ReShade 安装器", C["reshade_setup"] != "" && fileExists(C["reshade_setup"]), C["reshade_setup"]})
	ok, p = has("generic_depth_addon64")
	rows = append(rows, compRow{"Generic Depth", ok, p})
	ok, p = has("renodx-dlss5.addon64")
	rows = append(rows, compRow{"DLSS 5 神经渲染附加组件", ok, p})
	ok, p = has("nvngx_dlssnr.dll")
	rows = append(rows, compRow{"DLSS 5 神经渲染运行库", ok, p})
	ok, p = has("nvngx_dlss.dll")
	rows = append(rows, compRow{"DLSS 超分运行库（可选）", ok, p})
	rows = append(rows, compRow{"dgVoodoo2（D3D9 用）", dirExists(c.DgvRoot), c.DgvRoot})
	return rows
}

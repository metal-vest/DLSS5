// fetch.go — 组件获取引擎（移植自 fetch.ps1）
// 链路优先级：cache\ 缓存 → components\ 手动投递 → 本机已有游戏扫描 → 在线下载（固定版本源）
//
// 安全修复（详见 安全修复说明.md）：
//
//	F-01  全部在线构件下载/缓存命中时强制 SHA-256 校验（integrity.go）
//	F-02  下载源全部锁定 tag/commit，删除 latest 探测与首页抓取，升级随工具版本发布
//	F-03  RHI 固定资产名；第三方安装器执行前 GUI 明示来源并征得同意
//	F-07  关键组件缺失逐一登记 + 安装前路由级预检（install.go）
package main

import (
	"fmt"
	"path/filepath"
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

// ============ 固定下载源（F-02：升级须随工具版本发布更新此处与 pinnedSHA256） ============
const (
	// DLSS5-Feeder：tag 锁定。注意官方资产名不带 v 前缀
	// （DLSS5-Feeder-v0.10.0-beta.2.zip 形式不存在，旧版拼接方式必 404）
	pinFeederTag = "v0.11.0-beta.1"
	// LumeniteFX：commit 锁定（原 mainline 分支滚动更新属移动目标）
	pinLumeniteCommit = "76fa3e4d601c97e9bc63f119c01405b7b9938885"
	// ReShade 框架头文件：固定 commit；为空时走 master，内容漂移由 pinnedSHA256 拒收
	pinReshadeShadersCommit = ""
	// ReShade 安装器：固定版本直链（原首页 HTML 正则抓取属移动目标且解析脆弱）
	pinReshadeSetupURL = "https://reshade.me/downloads/ReShade_Setup_6.8.0_Addon.exe"
	// Generic Depth：上游 crosire/reshade-docs 自 v2026-08-02 起不再分发 generic_depth.*，
	// 该下载源已死；官方获取途径是 ReShade 安装器交互勾选，或手动投递 components\
	pinGenericDepthTag = "v2026-08-02"
	// RHI：固定 tag + 固定资产名（原"任意 exe 正则匹配"过宽，F-03）
	pinRHIRepo  = "RankFTW/RHI"
	pinRHITag   = "RHI-2.5.1"
	pinRHIAsset = "RHI-Setup.exe"
	// dgVoodoo2：tag + 资产名双固定
	pinDgVoodooTag   = "v2.87.3"
	pinDgVoodooAsset = "dgVoodoo2_87_3.zip"
)

func feederDownloadURL() string {
	asset := "DLSS5-Feeder-" + strings.TrimPrefix(pinFeederTag, "v") + ".zip"
	return fmt.Sprintf("https://github.com/jlrouzies-fr/DLSS5-Feeder/releases/download/%s/%s", pinFeederTag, asset)
}

func reshadeHeadersBase() string {
	if pinReshadeShadersCommit != "" {
		return "https://raw.githubusercontent.com/crosire/reshade-shaders/" + pinReshadeShadersCommit + "/Shaders/"
	}
	// 内容完整性由 pinnedSHA256 固定基准兜底：任何上游漂移都会被拒收
	return "https://raw.githubusercontent.com/crosire/reshade-shaders/master/Shaders/"
}

// F-03：执行第三方安装器前明示来源；GUI 模式弹"是/否"确认，静默模式记录警告后放行
func confirmThirdPartyInstaller(url string) bool {
	logf("WARN", "  即将下载并静默执行第三方安装器: %s", url)
	logf("WARN", "  来源为社区镜像（github.com/%s），非 NVIDIA / ReShade 官方构件", pinRHIRepo)
	if uiConfirmFn == nil {
		logLine("WARN", "  静默模式：默认允许执行（GUI 模式会先征询用户）")
		return true
	}
	return uiConfirmFn("是否执行第三方安装器？",
		"本机未找到 renodx-dlss5.addon64 与 nvngx_dlssnr.dll。\n\n"+
			"继续需下载并静默执行第三方安装器 RHI-Setup.exe：\n"+
			"来源：github.com/RankFTW/RHI（社区镜像，非官方构件）\n\n"+
			"要继续吗？\n选「否」可中止，之后把文件放进工具目录 components\\ 内即可。")
}

// ============ 主入口：准备全部组件 ============
func getAllComponents() *comps {
	c := newComps()
	C := c.Files

	// ---------- 1. DLSS5-Feeder 本体 ----------
	c.note("【1/8】获取 DLSS5-Feeder 本体（固定版本 " + pinFeederTag + "）")
	feederZip := filepath.Join(cacheDir, "DLSS5-Feeder.zip")
	if !fileExists(feederZip) {
		if !download(feederDownloadURL(), feederZip, 3, "DLSS5-Feeder "+pinFeederTag) {
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
	// F-07：Feeder 着色器是所有路由的必需件，缺失必须登记（原实现静默缺失）
	if !fileExists(C["DLSS5_Feed.fx"]) {
		c.need("DLSS5_Feed.fx（Feeder 着色器）")
	}
	C["host64exe"] = filepath.Join(feederSrc, "host64", "dlss5-feed-host64.exe")
	C["layer-x64"] = filepath.Join(feederSrc, "layer-x64")
	C["layer-x86"] = filepath.Join(feederSrc, "layer-x86")

	// ---------- 2. LumeniteFX 运动矢量着色器（commit 锁定） ----------
	c.note("【2/8】获取 LumeniteFX Kernel（推荐运动矢量着色器，commit 固定）")
	lumZip := filepath.Join(cacheDir, "LumeniteFX.zip")
	lumSrc := filepath.Join(cacheDir, "lumenitefx_extracted")
	if !fileExists(lumZip) {
		download("https://codeload.github.com/umar-afzaal/LumeniteFX/zip/"+pinLumeniteCommit,
			lumZip, 3, "LumeniteFX @ "+pinLumeniteCommit[:12])
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

	// ---------- 3. ReShade 框架头文件（内容由固定基准锚定） ----------
	c.note("【3/8】获取 ReShade 框架头文件（ReShade.fxh 等，SHA-256 锚定）")
	hdrDir := filepath.Join(cacheDir, "reshade_headers")
	ensureDir(hdrDir)
	for _, h := range []string{"ReShade.fxh", "ReShadeUI.fxh", "DrawText.fxh"} {
		hp := filepath.Join(hdrDir, h)
		if !fileExists(hp) {
			download(reshadeHeadersBase()+h, hp, 2, h)
		}
		if fileExists(hp) {
			C["header_"+h] = hp
		} else {
			c.need(h)
		}
	}

	// ---------- 4. ReShade 安装器（Addon 变体，固定版本直链） ----------
	c.note("【4/8】获取 ReShade 安装器（附加组件支持版，固定 6.8.0）")
	rsURL := pinReshadeSetupURL
	rsName := filepath.Base(strings.Split(rsURL, "?")[0])
	rsPath := filepath.Join(cacheDir, rsName)
	if download(rsURL, rsPath, 3, rsName) {
		C["reshade_setup"] = rsPath
		c.ReshadeSetup = rsPath
	} else {
		c.need("ReShade 安装器")
	}

	// ---------- 5. Generic Depth 附加组件（上游已死 → components\ 投递优先） ----------
	c.note("【5/8】获取 Generic Depth 附加组件（深度缓冲采集；官方分发已下架，优先手动投递）")
	for _, arch := range []string{"addon64", "addon32"} {
		gp := filepath.Join(cacheDir, "generic_depth."+arch)
		if fileExists(gp) {
			C["generic_depth_"+arch] = gp
			continue
		}
		// components\ 手动投递（含哈希记录，便于人工核对来源）
		if p := filepath.Join(compDir, "generic_depth."+arch); fileExists(p) {
			logComponentHash(p)
			if copyFile(p, gp) == nil {
				C["generic_depth_"+arch] = gp
				logf("OK", "  使用 components\\ 手动投递的 generic_depth.%s", arch)
				continue
			}
		}
		// 保留历史源探测（当前必然 404，快速失败并给出明确报缺）
		download("https://github.com/crosire/reshade-docs/releases/download/"+pinGenericDepthTag+"/generic_depth."+arch,
			gp, 1, "generic_depth."+arch)
		if fileExists(gp) {
			C["generic_depth_"+arch] = gp
		}
	}

	// ---------- 6. renodx-dlss5.addon64 + nvngx_dlssnr.dll ----------
	c.note("【6/8】获取 renodx-dlss5.addon64 + nvngx_dlssnr.dll（DLSS 5 神经渲染附加组件）")
	gotRenodx, gotDlssnr := false, false
	if p := filepath.Join(compDir, "renodx-dlss5.addon64"); fileExists(p) {
		logComponentHash(p)
		C["renodx-dlss5.addon64"] = p
		gotRenodx = true
		logf("OK", "  使用 components\\ 手动投递的 renodx-dlss5.addon64")
	}
	if p := filepath.Join(compDir, "nvngx_dlssnr.dll"); fileExists(p) {
		logComponentHash(p)
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
		// F-03：固定 tag + 固定资产名；执行前明示第三方来源并征得同意（GUI）
		rhiURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", pinRHIRepo, pinRHITag, pinRHIAsset)
		if confirmThirdPartyInstaller(rhiURL) {
			logLine("INFO", "  从社区镜像（RankFTW/RHI）下载 RHI-Setup.exe 并静默解包…")
			rhiExe := filepath.Join(cacheDir, pinRHIAsset)
			if download(rhiURL, rhiExe, 3, pinRHIAsset+" ("+pinRHITag+")") {
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
		logComponentHash(p)
		C["nvngx_dlss.dll"] = p
		logf("OK", "  使用 components\\ 手动投递的 nvngx_dlss.dll")
	} else if hit := findLocalNvNgx(); hit != "" {
		C["nvngx_dlss.dll"] = hit
	} else {
		logf("WARN", "  未找到 nvngx_dlss.dll —— 不阻塞（NVIDIA 驱动自带运行库可兜底）")
	}

	// ---------- 8. dgVoodoo2（仅 D3D9 路径需要，先备好） ----------
	c.note("【8/8】获取 dgVoodoo2（D3D9 转译层，仅老游戏需要，固定 " + pinDgVoodooTag + "）")
	dgvZip := filepath.Join(cacheDir, "dgVoodoo2.zip")
	if !fileExists(dgvZip) {
		download(fmt.Sprintf("https://github.com/dege-diosg/dgVoodoo2/releases/download/%s/%s", pinDgVoodooTag, pinDgVoodooAsset),
			dgvZip, 3, "dgVoodoo2 "+pinDgVoodooTag)
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

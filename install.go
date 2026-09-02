// install.go — 安装引擎：五条路由 + 模板写入 + 清单（移植自 install.ps1，逻辑严格对照 DEPLOY-DEV.md）
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ============ 模板：ReShade.ini ============
func reshadeIniContent(gameDir string, vulkan, d3d9, hostMinimal bool) string {
	shaderDir := filepath.Join(gameDir, "reshade-shaders", "Shaders")
	textureDir := filepath.Join(gameDir, "reshade-shaders", "Textures")
	if hostMinimal {
		// 32 位 host64\ 的极简版：绝不带 [ADDON] AddonPath（DEPLOY-DEV 第 7 节）
		return "[GENERAL]\r\nEffectSearchPaths=.\\\r\nTextureSearchPaths=.\\\r\n\r\n[INPUT]\r\nKeyOverlay=36,0,0,0\r\n"
	}
	var b strings.Builder
	b.WriteString("[GENERAL]\r\n")
	fmt.Fprintf(&b, "EffectSearchPaths=%s\\**\r\n", shaderDir)
	fmt.Fprintf(&b, "TextureSearchPaths=%s\\**\r\n", textureDir)
	b.WriteString("PresetPath=ReShadePreset.ini\r\n\r\n")
	if vulkan {
		b.WriteString("[ADDON]\r\nAddonPath=.\\\r\n\r\n")
	}
	b.WriteString("[DEPTH]\r\n")
	if d3d9 {
		b.WriteString("DepthCopyBeforeClears=1\r\n") // UE3 时代的深度清除行为（DEPLOY-DEV 第 7 节）
	} else {
		b.WriteString("DepthCopyBeforeClears=0\r\n")
	}
	b.WriteString("UseAspectRatioHeuristics=3\r\n\r\n")
	b.WriteString("[INPUT]\r\nKeyOverlay=36,0,0,0\r\n")
	return b.String()
}

// ============ 模板：ReShadePreset.ini ============
// 关键：DLSS5_MV_PROVIDER 是 per-effect 预处理器定义，必须写在预设文件的 [DLSS5_Feed.fx] 段（DEPLOY-DEV 第 5 节）
func reshadePresetContent(mvProvider int) string {
	return "[GENERAL]\r\nTechniques=Lumenite_Kernel@lumenite_Kernel.fx,DLSS5_Feed@DLSS5_Feed.fx\r\n" +
		"TechniqueSorting=Lumenite_Kernel@lumenite_Kernel.fx,DLSS5_Feed@DLSS5_Feed.fx\r\n\r\n" +
		fmt.Sprintf("[DLSS5_Feed.fx]\r\nPreprocessorDefinitions=DLSS5_MV_PROVIDER=%d\r\n", mvProvider)
}

const dgVoodooConf = "[General]\r\nOutputAPI=d3d11_fl11_0\r\n\r\n[DirectX]\r\nDisableAndPassThru=false\r\nVideoCard=internal3D\r\nVRAM=1GB\r\ndgVoodooWatermark=true\r\n"

const inGameSteps = `【DLSS 5 已装入本游戏 · 游戏内启用步骤】
本目录文件由『DLSS5 一键开启工具』部署。首次进游戏后请确认：

1. 画面设置里关掉 MSAA / SSAA（多重采样抗锯齿）。
2. 按 Home 打开 ReShade 菜单：
   - 确认没有编译报错；
   - 勾选 'LUMENITE: Kernel 2.0'，再勾选 'DLSS 5 Feed'；
   - 两者顺序必须是 Kernel 在上、DLSS 5 Feed 在下。
3. 仍在 ReShade 菜单 → Add-ons（附加组件）页 → 'DLSS 5 Neural Rendering' 面板 → 打开神经渲染（Neural Rendering）开关。
4. 观察画面：建筑边缘应当更"干净锐利"。快速移动时的轻微拖影属正常（估算运动矢量的特性）。
5. 排错看本目录 dlss5-feed.log：应出现 "feature ready ... DLAA" 与 "frame N delivered"。
   · 32 位 / D3D9 游戏额外看 host64\ 内的日志。
   · Vulkan 游戏若日志说 interop 入口缺失 → 用本目录 run-with-feed-layer.bat 启动游戏。
6. 成功后可运行 dgVoodooCpl.exe 关闭水印（仅 D3D9 游戏适用）。

【重要】本方案与 NVIDIA Smooth Motion、OptiScaler 互斥，开启其中之一会失效。
`

// ============ 通用：投放着色器文件集 ============
func installShaderSet(c *comps, gameDir string) []string {
	shaders := filepath.Join(gameDir, "reshade-shaders", "Shaders")
	textures := filepath.Join(gameDir, "reshade-shaders", "Textures")
	ensureDir(shaders)
	ensureDir(textures)
	var installed []string
	rec := func(rel string, ok bool) {
		if ok {
			installed = append(installed, rel)
		}
	}
	rec("reshade-shaders\\Shaders\\DLSS5_Feed.fx", copyIfExists(c.Files["DLSS5_Feed.fx"], shaders, ""))
	for _, h := range []string{"ReShade.fxh", "ReShadeUI.fxh", "DrawText.fxh"} {
		rec("reshade-shaders\\Shaders\\"+h, copyIfExists(c.Files["header_"+h], shaders, ""))
	}
	lumDir := c.Files["lumenite_dir"]
	if lumDir != "" && dirExists(lumDir) {
		entries, _ := os.ReadDir(lumDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "lumenite_") && strings.EqualFold(filepath.Ext(e.Name()), ".fx") {
				if copyFile(filepath.Join(lumDir, e.Name()), filepath.Join(shaders, e.Name())) == nil {
					installed = append(installed, "reshade-shaders\\Shaders\\"+e.Name())
				}
			}
		}
		incDir := filepath.Join(lumDir, "include")
		if dirExists(incDir) {
			dstInc := filepath.Join(shaders, "include")
			ensureDir(dstInc)
			filepath.WalkDir(incDir, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".fxh") {
					if copyFile(p, filepath.Join(dstInc, filepath.Base(p))) == nil {
						installed = append(installed, "reshade-shaders\\Shaders\\include\\"+filepath.Base(p))
					}
				}
				return nil
			})
		}
		lumParent := filepath.Dir(lumDir)
		noise := filepath.Join(lumParent, "Textures", "lumenite_bluenoise256.png")
		if !fileExists(noise) {
			noise = findFileRecursive(lumParent, "lumenite_bluenoise256.png", 4)
		}
		if noise != "" && fileExists(noise) {
			if copyFile(noise, filepath.Join(textures, "lumenite_bluenoise256.png")) == nil {
				installed = append(installed, "reshade-shaders\\Textures\\lumenite_bluenoise256.png")
			}
		}
	}
	return installed
}

// ============ 通用：ReShade 静默安装（headless） ============
func reshadeHeadless(setupPath, gameExe, api string) bool {
	logf("STEP", "ReShade 静默安装: --api %s · %s", api, gameExe)
	code := runExe(setupPath, "--headless", "--api", api, gameExe)
	logf("INFO", "ReShade 安装器退出码: %d", code)
	return code == 0
}

// 用系统 64 位程序当"假游戏"，为 32 位路径炼制 x64 版 dxgi.dll（host64 用）
func bakeHost64Dxgi(setupPath, workDir string) string {
	ensureDir(workDir)
	dummy := filepath.Join(workDir, "dummy_host64_target.exe")
	for _, src := range []string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "where.exe"),
		filepath.Join(os.Getenv("SystemRoot"), "System32", "notepad.exe"),
	} {
		if !fileExists(src) {
			continue
		}
		copyFile(src, dummy)
		logf("INFO", "为 host64 生成 64 位 ReShade dxgi.dll（对替身 exe 静默安装）")
		reshadeHeadless(setupPath, dummy, "d3d11")
		if p := filepath.Join(workDir, "dxgi.dll"); fileExists(p) {
			return p
		}
	}
	return ""
}

// 把目录整体复制并逐文件记录（清单用），返回相对 srcDir 的文件名列表
func copyDirRecorded(srcDir, dstDir string) []string {
	var installed []string
	filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, p)
		target := filepath.Join(dstDir, rel)
		if copyFile(p, target) == nil {
			installed = append(installed, strings.ReplaceAll(filepath.ToSlash(rel), "/", "\\"))
		}
		return nil
	})
	return installed
}

// ============ 通用：投放 Feeder 主文件集（游戏根目录） ============
func installFeederFiles(c *comps, gameDir string, is32 bool) []string {
	var installed []string
	rec := func(rel string, ok bool) {
		if ok {
			installed = append(installed, rel)
			logf("OK", "  已投放 %s", rel)
		}
	}
	addon := "dlss5-feed.addon64"
	if is32 {
		addon = "dlss5-feed.addon32"
	}
	rec(addon, copyIfExists(c.Files[addon], gameDir, ""))
	if !is32 {
		// 64 位：renodx/nvngx 全在游戏根目录
		rec("renodx-dlss5.addon64", copyIfExists(c.Files["renodx-dlss5.addon64"], gameDir, ""))
		rec("nvngx_dlssnr.dll", copyIfExists(c.Files["nvngx_dlssnr.dll"], gameDir, ""))
		if p := c.Files["nvngx_dlss.dll"]; p != "" && fileExists(p) {
			rec("nvngx_dlss.dll", copyIfExists(p, gameDir, ""))
		}
		rec("generic_depth.addon64", copyIfExists(c.Files["generic_depth_addon64"], gameDir, ""))
	} else {
		// 32 位：游戏侧只要 Generic Depth 32
		rec("generic_depth.addon32", copyIfExists(c.Files["generic_depth_addon32"], gameDir, ""))
	}
	return installed
}

// ============ host64\ 部署（32 位游戏 / D3D9） ============
func installHost64(c *comps, gameDir string) []string {
	hostDir := filepath.Join(gameDir, "host64")
	ensureDir(hostDir)
	var installed []string
	rec := func(rel string, ok bool) {
		if ok {
			installed = append(installed, rel)
			logf("OK", "  已投放 %s", rel)
		}
	}
	rec("host64\\dlss5-feed-host64.exe", copyIfExists(c.Files["host64exe"], hostDir, ""))
	rec("host64\\renodx-dlss5.addon64", copyIfExists(c.Files["renodx-dlss5.addon64"], hostDir, ""))
	rec("host64\\nvngx_dlssnr.dll", copyIfExists(c.Files["nvngx_dlssnr.dll"], hostDir, ""))
	if p := c.Files["nvngx_dlss.dll"]; p != "" && fileExists(p) {
		rec("host64\\nvngx_dlss.dll", copyIfExists(p, hostDir, ""))
	}
	// x64 ReShade dxgi.dll：优先 components\ 里的现成 dxgi_x64.dll，否则用替身 exe 炼制
	x64 := filepath.Join(compDir, "dxgi_x64.dll")
	if !fileExists(x64) {
		x64 = bakeHost64Dxgi(c.Files["reshade_setup"], filepath.Join(cacheDir, "host64_bake"))
	}
	if x64 != "" && fileExists(x64) {
		if copyFile(x64, filepath.Join(hostDir, "dxgi.dll")) == nil {
			rec("host64\\dxgi.dll", true)
		}
	} else {
		logf("ERROR", "host64\\dxgi.dll 生成失败 —— 32 位游戏将无法启用（把 dxgi_x64.dll 放进 components\\ 可手动补齐）")
	}
	// host64\ReShade.ini：极简版
	writeTextBOM(filepath.Join(hostDir, "ReShade.ini"), reshadeIniContent("", false, false, true))
	rec("host64\\ReShade.ini", true)
	return installed
}

// ============ dgVoodoo2 部署（D3D9 路径第一步） ============
func installDgVoodoo(c *comps, gameDir string) []string {
	root := c.DgvRoot
	var installed []string
	if root == "" || !dirExists(root) {
		logf("ERROR", "dgVoodoo2 包缺失，D3D9 路径无法继续")
		return installed
	}
	// 主 DLL：MS\x86\D3D9.dll（优先 x86）
	d3d9 := findFileRecursive(root, "D3D9.dll", 3)
	if d3d9 != "" {
		if !strings.Contains(strings.ToLower(d3d9), `\x86\`) {
			if x86 := findFileRecursive(filepath.Join(root, "MS", "x86"), "D3D9.dll", 2); x86 != "" {
				d3d9 = x86
			}
		}
		if copyFile(d3d9, filepath.Join(gameDir, "D3D9.dll")) == nil {
			installed = append(installed, "D3D9.dll")
			logf("OK", "  已投放 D3D9.dll（dgVoodoo2 转译层）")
		}
	} else {
		logf("ERROR", "dgVoodoo2 包内未找到 D3D9.dll")
	}
	// 附属目录（3Dfx / Cpl / MS）
	for _, sub := range []string{"3Dfx", "Cpl", "MS"} {
		src := filepath.Join(root, sub)
		if !dirExists(src) {
			// 顶层没有就递归找同名目录
			if found := findDirRecursive(root, sub, 3); found != "" {
				src = found
			}
		}
		if src != "" && dirExists(src) {
			files := copyDirRecorded(src, filepath.Join(gameDir, sub))
			for _, f := range files {
				installed = append(installed, sub+"\\"+f)
			}
		}
	}
	cpl := findFileRecursive(root, "dgVoodooCpl.exe", 3)
	if cpl != "" && copyFile(cpl, filepath.Join(gameDir, "dgVoodooCpl.exe")) == nil {
		installed = append(installed, "dgVoodooCpl.exe")
	}
	writeTextBOM(filepath.Join(gameDir, "dgVoodoo.conf"), dgVoodooConf)
	installed = append(installed, "dgVoodoo.conf")
	logf("OK", "dgVoodoo2 已部署（水印开启，成功后可在 dgVoodooCpl 里关闭）")
	return installed
}

func findDirRecursive(rootDir, dirName string, maxDepth int) string {
	if !dirExists(rootDir) {
		return ""
	}
	var found string
	filepath.WalkDir(rootDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipAll
		}
		depth := strings.Count(p, string(os.PathSeparator)) + 1
		if d.IsDir() && strings.EqualFold(filepath.Base(p), dirName) && p != rootDir && depth <= maxDepth+3 {
			found = p
			return filepath.SkipAll
		}
		if d.IsDir() && depth > maxDepth+3 {
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

// ============ Vulkan：全局层 App 名单登记 ============
func ensureReshadeAppsEntry(gameExe string) {
	appsIni := filepath.Join(os.Getenv("ProgramData"), "ReShade", "ReShadeApps.ini")
	if !fileExists(appsIni) {
		return // 全局层未安装或安装器未生成名单，交由官方安装器处理
	}
	data, err := os.ReadFile(appsIni)
	if err != nil {
		return
	}
	content := string(data)
	if strings.Contains(content, gameExe) {
		logf("OK", "ReShadeApps.ini 已包含本游戏")
		return
	}
	if os.WriteFile(appsIni, []byte(content), 0o644) != nil {
		// 尝试追加写入（ACL 通常只允许管理员写）
		if err := appendReshadeApps(appsIni, gameExe); err != nil {
			logf("WARN", "ReShadeApps.ini 需要手动登记（权限不足）。请用【管理员】PowerShell 执行：")
			logf("WARN", `  $p='%s'; $c=(Get-Content -Raw $p).TrimEnd()+',%s'; [IO.File]::WriteAllText($p,$c,(New-Object Text.UTF8Encoding $true))`, appsIni, gameExe)
			return
		}
	} else {
		// 直接可写：追加
		nb := strings.TrimRight(content, "\r\n") + "," + gameExe
		writeTextBOM(appsIni, nb+"\r\n")
	}
	logf("OK", "已把本游戏登记进 ReShadeApps.ini（全局 Vulkan 层生效名单）")
}

func appendReshadeApps(appsIni, gameExe string) error {
	// 经由提权 PowerShell 追加（会弹一次 UAC，用户确认即可）
	ps := filepath.Join(logsDir, "reshade_apps_append.ps1")
	script := fmt.Sprintf("$p='%s'\r\n$c=(Get-Content -Raw $p).TrimEnd()+',%s'\r\n[IO.File]::WriteAllText($p,$c,(New-Object Text.UTF8Encoding $true))\r\n", appsIni, gameExe)
	if err := os.WriteFile(ps, []byte("\ufeff"+script), 0o644); err != nil {
		return err
	}
	return exec.Command("powershell", "-NoProfile", "-Command",
		"Start-Process powershell -Verb RunAs -Wait -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','"+ps+"'").Run()
}

// ============ 清单 ============
type manifestData struct {
	GameExe              string   `json:"GameExe"`
	Route                string   `json:"Route"`
	Timestamp            string   `json:"Timestamp"`
	ReshadeInstalledByUs bool     `json:"ReshadeInstalledByUs"`
	Files                []string `json:"Files"`
}

const manifestName = "dlss5-oneclick.manifest.json"

func relRecord(gameDir, p string) string {
	rel, err := filepath.Rel(gameDir, p)
	if err != nil {
		return p
	}
	return strings.ReplaceAll(filepath.ToSlash(rel), "/", "\\")
}

// ============ 主安装入口 ============
func installGameRoute(a *analysis, c *comps) (*manifestData, error) {
	gameDir := a.ExeDir
	gameExe := a.ExePath
	route := a.Route
	is32 := a.Bitness == "32"
	var allInstalled []string
	reshadeInstalledByUs := false

	logf("STEP", "开始安装 · 路由 [%s] · %s", route, a.GameName)

	// ---------- ReShade 本体 ----------
	hasGoodReShade := false
	for _, d := range reshadeProbeDlls {
		ok, _, maj, min, _ := isReShadeDll(filepath.Join(gameDir, d))
		if ok && (maj > 6 || (maj == 6 && min >= 8)) {
			hasGoodReShade = true
			break
		}
	}
	if hasGoodReShade {
		logf("OK", "已有 ReShade ≥6.8，跳过本体安装")
	} else {
		if c.Files["reshade_setup"] == "" {
			return nil, fmt.Errorf("缺少 ReShade 安装器，无法继续")
		}
		api := "d3d11"
		switch route {
		case "d3d64", "d3d32":
			if a.Api == "d3d12" {
				api = "d3d12"
			}
		case "d3d9":
			api = "dxgi" // 装成 dxgi.dll，把 d3d9.dll 的名字留给 dgVoodoo
		case "vulkan", "vulkan32":
			api = "vulkan"
		case "opengl64", "opengl32":
			api = "opengl"
		}
		ok := reshadeHeadless(c.Files["reshade_setup"], gameExe, api)
		reshadeInstalledByUs = ok
		found := false
		for _, d := range reshadeProbeDlls {
			if ok2, _, _, _, _ := isReShadeDll(filepath.Join(gameDir, d)); ok2 {
				found = true
				break
			}
		}
		if !found {
			if ok {
				logf("WARN", "安装器报告成功但未检出 ReShade DLL，尝试以 GUI 模式兜底")
			}
			logf("WARN", "静默安装未检出 ReShade DLL —— 打开 GUI 安装器，请在弹出的窗口中完成安装")
			cmd := exec.Command(c.Files["reshade_setup"], gameExe, "--api", api)
			cmd.Start()
			if uiMsgBoxFn != nil {
				uiMsgBoxFn("手动步骤",
					"已打开 ReShade 官方安装器。\n\n1. 确认游戏路径正确\n2. 选择 API："+api+"\n3. 勾选『Enable loading of add-ons』\n4. 完成后回到本工具重新点击『一键开启』完成校验", false)
			}
		} else {
			logf("OK", "ReShade 本体就绪")
		}
	}

	// ---------- 各路由专属 ----------
	switch route {
	case "d3d9":
		allInstalled = append(allInstalled, installDgVoodoo(c, gameDir)...)
	case "vulkan":
		if dir := c.Files["layer-x64"]; dir != "" && dirExists(dir) {
			files := copyDirRecorded(dir, gameDir)
			allInstalled = append(allInstalled, files...)
			logf("OK", "  Vulkan 回退层已就位（interop 缺失时用 run-with-feed-layer.bat 启动）")
		}
		ensureReshadeAppsEntry(gameExe)
	case "vulkan32":
		if dir := c.Files["layer-x86"]; dir != "" && dirExists(dir) {
			dst := filepath.Join(gameDir, "layer-x86")
			ensureDir(dst)
			files := copyDirRecorded(dir, dst)
			for _, f := range files {
				allInstalled = append(allInstalled, "layer-x86\\"+f)
			}
		}
		ensureReshadeAppsEntry(gameExe)
	}

	// ---------- Feeder 文件 + 着色器 ----------
	allInstalled = append(allInstalled, installFeederFiles(c, gameDir, is32)...)
	allInstalled = append(allInstalled, installShaderSet(c, gameDir)...)

	// ---------- host64（32 位与 D3D9 路径） ----------
	if is32 || route == "d3d9" {
		allInstalled = append(allInstalled, installHost64(c, gameDir)...)
	}

	// ---------- ReShade.ini / ReShadePreset.ini ----------
	iniPath := filepath.Join(gameDir, "ReShade.ini")
	presetPath := filepath.Join(gameDir, "ReShadePreset.ini")
	if !fileExists(iniPath) {
		vulkan := route == "vulkan" || route == "vulkan32"
		d3d9 := route == "d3d9"
		writeTextBOM(iniPath, reshadeIniContent(gameDir, vulkan, d3d9, false))
		allInstalled = append(allInstalled, "ReShade.ini")
	} else {
		keys := map[string]string{
			"EffectSearchPaths":  gameDir + `\reshade-shaders\Shaders\**`,
			"TextureSearchPaths": gameDir + `\reshade-shaders\Textures\**`,
			"PresetPath":         "ReShadePreset.ini",
		}
		iniSetKeys(iniPath, "GENERAL", keys)
		if route == "vulkan" || route == "vulkan32" {
			iniSetKeys(iniPath, "ADDON", map[string]string{"AddonPath": ".\\"})
		}
		if route == "d3d9" {
			iniSetKeys(iniPath, "DEPTH", map[string]string{"DepthCopyBeforeClears": "1"})
		}
		allInstalled = append(allInstalled, "ReShade.ini(合并)")
	}
	if !fileExists(presetPath) {
		writeTextBOM(presetPath, reshadePresetContent(3))
		allInstalled = append(allInstalled, "ReShadePreset.ini")
	} else {
		iniSetKeys(presetPath, "DLSS5_Feed.fx", map[string]string{"PreprocessorDefinitions": "DLSS5_MV_PROVIDER=3"})
		allInstalled = append(allInstalled, "ReShadePreset.ini(合并)")
	}

	// ---------- 游戏内启用步骤 ----------
	writeTextBOM(filepath.Join(gameDir, "DLSS5_游戏内启用步骤.txt"), inGameSteps)

	// ---------- 清单 ----------
	setProgress(100, "安装完成")
	m := &manifestData{
		GameExe:              gameExe,
		Route:                route,
		Timestamp:            time.Now().Format("2006-01-02 15:04:05"),
		ReshadeInstalledByUs: reshadeInstalledByUs,
	}
	seen := map[string]bool{}
	for _, f := range allInstalled {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		m.Files = append(m.Files, f)
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(filepath.Join(gameDir, manifestName), data, 0o644)
	logf("OK", "安装完成，共写入 %d 项 · 清单 %s", len(m.Files), manifestName)
	return m, nil
}

// ============ 桌面启动器 ============
func desktopDir() string {
	out, err := runPS("[Environment]::GetFolderPath('Desktop')")
	if err == nil && out != "" {
		return out
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
}

func newGameShortcut(a *analysis) (string, error) {
	lnkPath := filepath.Join(desktopDir(), "DLSS5 - "+a.GameName+".lnk")
	target, args, workdir := a.ExePath, "", a.ExeDir
	if a.Route == "vulkan" || a.Route == "vulkan32" {
		bat := filepath.Join(a.ExeDir, "run-with-feed-layer.bat")
		if a.Route == "vulkan32" {
			bat = filepath.Join(a.ExeDir, "layer-x86", "run-with-feed-layer32.bat")
		}
		if fileExists(bat) {
			target = os.Getenv("COMSPEC")
			args = fmt.Sprintf("/c \"\"%s\" \"%s\"\"\"", bat, a.ExePath)
			workdir = a.ExeDir
		}
	}
	// 写临时 ps1 避免 -Command 转义地狱
	ps := filepath.Join(logsDir, "make_shortcut.ps1")
	script := fmt.Sprintf(`
$ws = New-Object -ComObject WScript.Shell
$s = $ws.CreateShortcut('%s')
$s.TargetPath = '%s'
$s.Arguments = '%s'
$s.WorkingDirectory = '%s'
$s.Description = '通过 DLSS5-Feeder 启动（DLSS5 一键开启工具创建）'
$s.Save()
`, lnkPath, target, args, workdir)
	if err := os.WriteFile(ps, []byte("\ufeff"+script), 0o644); err != nil {
		return "", err
	}
	if err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps).Run(); err != nil {
		return "", err
	}
	logf("OK", "桌面启动器已创建: %s", lnkPath)
	return lnkPath, nil
}

func removeGameShortcut(gameExe string) {
	name := "DLSS5 - " + strings.TrimSuffix(filepath.Base(gameExe), filepath.Ext(gameExe)) + ".lnk"
	p := filepath.Join(desktopDir(), name)
	if fileExists(p) {
		os.Remove(p)
	}
}

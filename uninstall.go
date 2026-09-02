// uninstall.go — 一键卸载（移植自 install.ps1 的 Uninstall-Game，零残留）
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func uninstallGame(gameExe string) (int, error) {
	gameDir := filepath.Dir(gameExe)
	manifestPath := filepath.Join(gameDir, manifestName)
	if !fileExists(manifestPath) {
		return 0, fmt.Errorf("未在该游戏目录找到 %s —— 本工具未在此游戏上安装过", manifestName)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return 0, err
	}
	var m manifestData
	if err := json.Unmarshal(data, &m); err != nil {
		return 0, fmt.Errorf("清单解析失败: %v", err)
	}
	removed := 0

	for _, f := range m.Files {
		merged := strings.HasSuffix(f, "(合并)")
		rel := strings.TrimSuffix(f, "(合并)")
		if rel == "" || strings.HasSuffix(rel, `\`) {
			continue // 目录条目最后统一清空壳
		}
		p := filepath.Join(gameDir, rel)
		if merged {
			// 合并写入的 ini：只移除我们写过的键
			if filepath.Base(p) == "ReShade.ini" {
				iniRemoveKeys(p, "GENERAL", []string{"PresetPath"})
				iniRemoveKeys(p, "GENERAL", []string{"EffectSearchPaths", "TextureSearchPaths"})
			}
			if filepath.Base(p) == "ReShadePreset.ini" {
				iniRemoveKeys(p, "DLSS5_Feed.fx", []string{"PreprocessorDefinitions"})
			}
			continue
		}
		if fileExists(p) {
			if os.Remove(p) == nil {
				removed++
			}
		}
	}

	// 清空壳目录
	for _, d := range []string{
		`reshade-shaders\Shaders\include`, `reshade-shaders\Shaders`, `reshade-shaders\Textures`,
		`reshade-shaders`, `host64`, `Addons`, `layer-x86`, `3Dfx`, `Cpl`, `MS`,
	} {
		dp := filepath.Join(gameDir, d)
		if dirExists(dp) && dirHasNoFiles(dp) {
			os.Remove(dp)
		}
	}

	// Vulkan：从 ReShadeApps.ini 摘掉本游戏
	appsIni := filepath.Join(os.Getenv("ProgramData"), "ReShade", "ReShadeApps.ini")
	if fileExists(appsIni) && strings.HasPrefix(m.Route, "vulkan") {
		if data, err := os.ReadFile(appsIni); err == nil {
			content := string(data)
			if strings.Contains(content, gameExe) {
				copyFile(appsIni, appsIni+".bak")
				lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
				for i, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "Apps=") {
						nb := strings.ReplaceAll(line, gameExe, "")
						for strings.Contains(nb, ",,") {
							nb = strings.ReplaceAll(nb, ",,", ",")
						}
						nb = strings.TrimRight(nb, ",")
						lines[i] = nb
						break
					}
				}
				writeTextBOM(appsIni, strings.Join(lines, "\r\n"))
				logLine("INFO", "已从 ReShadeApps.ini 移除本游戏（全局层不再对其生效）")
			}
		}
	}

	// 可选：卸载本工具安装的 ReShade 本体
	if m.ReshadeInstalledByUs {
		logLine("INFO", "本工具安装的 ReShade 一并卸载（若有其他游戏共用会保留其文件）")
		for _, d := range reshadeProbeDlls {
			p := filepath.Join(gameDir, d)
			if ok, _, _, _, _ := isReShadeDll(p); ok {
				if os.Remove(p) == nil {
					removed++
				}
			}
		}
	}

	removeGameShortcut(m.GameExe)
	os.Remove(filepath.Join(gameDir, "DLSS5_游戏内启用步骤.txt"))
	os.Remove(manifestPath)

	logf("OK", "卸载完成，删除 %d 个文件", removed)
	return removed, nil
}

// 目录内（含子目录）是否已无任何文件
func dirHasNoFiles(dir string) bool {
	noFiles := true
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			noFiles = false
			return filepath.SkipAll
		}
		return nil
	})
	return noFiles
}

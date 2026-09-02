// uninstall.go — 一键卸载（移植自 install.ps1 的 Uninstall-Game，零残留）
//
// 安全修复（详见 安全修复说明.md）：
//
//	F-09  安装时合并写入过的 INI，优先还原安装前备份（.dlss5bak），
//	      仅在无备份时才退回删键，避免连带清除用户原有配置
//	附加  ReShadeApps.ini 清理改为处理全部 Apps= 行（原实现只处理首行）
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
	restored := map[string]bool{} // F-09：已从备份还原的文件，后续"(合并)"条目不再删键

	for _, f := range m.Files {
		// F-09：安装前备份 → 还原用户原始配置，备份自身随后删除
		if strings.HasSuffix(f, iniBackupSuffix) {
			rel := strings.TrimSuffix(f, iniBackupSuffix)
			if rel == "" || strings.HasSuffix(rel, `\`) {
				continue
			}
			p := filepath.Join(gameDir, rel)
			bak := filepath.Join(gameDir, f)
			if fileExists(bak) {
				if copyFile(bak, p) == nil {
					os.Remove(bak)
					restored[rel] = true
					logf("INFO", "已还原用户原始配置: %s", rel)
				} else {
					logf("WARN", "还原失败，保留备份文件: %s", f)
				}
			}
			continue
		}
		merged := strings.HasSuffix(f, "(合并)")
		rel := strings.TrimSuffix(f, "(合并)")
		if rel == "" || strings.HasSuffix(rel, `\`) {
			continue // 目录条目最后统一清空壳
		}
		// F-09：该文件已从备份整体还原，不能对其再执行删键
		if restored[rel] {
			continue
		}
		p := filepath.Join(gameDir, rel)
		if merged {
			// 无备份可还原（如旧版本清单）时，退回只移除我们写过的键
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

	// Vulkan：还原 / 清理 ReShadeApps.ini（全局层名单）
	if strings.HasPrefix(m.Route, "vulkan") {
		appsIni := filepath.Join(os.Getenv("ProgramData"), "ReShade", "ReShadeApps.ini")
		if fileExists(appsIni) {
			if bak := appsIni + iniBackupSuffix; fileExists(bak) {
				// F-09：优先整体还原修改前的名单
				if copyFile(bak, appsIni) == nil {
					os.Remove(bak)
					logLine("INFO", "已还原修改前的 ReShadeApps.ini（全局层名单回到安装前状态）")
				} else {
					logf("WARN", "ReShadeApps.ini 还原失败，保留备份: %s", bak)
				}
			} else if data, err := os.ReadFile(appsIni); err == nil {
				// 无备份（旧版本安装）时逐行摘除本游戏
				content := stripBOM(string(data))
				if strings.Contains(content, gameExe) {
					lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
					var out []string
					for _, line := range lines {
						t := strings.TrimSpace(line)
						if !strings.HasPrefix(t, "Apps=") {
							out = append(out, line)
							continue
						}
						nb := strings.ReplaceAll(line, gameExe, "")
						for strings.Contains(nb, ",,") {
							nb = strings.ReplaceAll(nb, ",,", ",")
						}
						nb = strings.TrimRight(nb, ",\r")
						// 仅剩 "Apps=" 键头（本游戏是唯一登记）时整行移除
						if i := strings.Index(nb, "="); i >= 0 {
							val := strings.TrimLeft(nb[i+1:], ",")
							if val == "" {
								continue
							}
							nb = nb[:i+1] + val
						}
						out = append(out, nb)
					}
					writeTextBOM(appsIni, strings.Join(out, "\r\n"))
					logLine("INFO", "已从 ReShadeApps.ini 移除本游戏（全局层不再对其生效）")
				}
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

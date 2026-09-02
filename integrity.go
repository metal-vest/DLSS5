// integrity.go — 供应链完整性校验（F-01 修复）
//
// 策略（双层基准）：
//  1. pinnedSHA256 中有固定基准的构件：下载完成与缓存命中时强制比对，
//     不符即删除该文件并判定获取失败（fail-closed），拦截上游投毒/篡改/打包漂移；
//  2. 无固定基准的构件：首次成功下载时把哈希记入 TOFU 基准
//     （cache\integrity-baseline.json），此后每次命中与安装前复验。
//
// 全部固定基准为 2026-09-02 从官方源实测记录（来源与升级流程见 安全修复说明.md）。
// 升级任何组件时必须随工具版本发布同步更新本表。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 固定版本构件的 SHA-256 基准（键 = cache\ 中的缓存文件名）
var pinnedSHA256 = map[string]string{
	// DLSS5-Feeder v0.11.0-beta.1 官方资产 DLSS5-Feeder-0.11.0-beta.1.zip 实测
	"DLSS5-Feeder.zip": "516899f38a978b9c58d107667102439296e79868173e8be01501c9d083be3a87",
	// LumeniteFX mainline @ commit 76fa3e4d601c97e9bc63f119c01405b7b9938885 打包实测
	"LumeniteFX.zip": "bf574543a6af6527587af0bad139922e8c0363bb154cdfb3e41133c7dca2ee3f",
	// crosire/reshade-shaders 头文件实测（URL 仍走 master，内容漂移由此表拒收）
	"ReShade.fxh":   "6dabfbbaf968c3871905d2ea17f96572ff7b1cec01310b5d0e5252b66b30174f",
	"ReShadeUI.fxh": "78adf672df47460297eb9fe6dd238d2aafa24510b52b84feb1a745dff70eb901",
	"DrawText.fxh":  "b79cc4dfb3e98bcf4c06193d00ea7631d74f467f73a4deeeee13e71336d3e680",
	// reshade.me 官方安装器 6.8.0 Addon 变体实测
	"ReShade_Setup_6.8.0_Addon.exe": "afe4c8f13048306307983b8b3d41d5bf00a86820440b0e57dea10950e1176445",
	// RankFTW/RHI RHI-2.5.1 固定资产实测（解包产物的确定性由此哈希间接锚定）
	"RHI-Setup.exe": "3eadcc03af8b3ba7ec277339bc0a9fb51077d5dc0781bba94790aa6e70864bc2",
	// dege-diosg/dgVoodoo2 v2.87.3 官方资产 dgVoodoo2_87_3.zip 实测
	"dgVoodoo2.zip": "6fb954bed55bf70e948c5045a663a9df31ea206faf105e327bafe46c318f867f",
	// generic_depth.addon64/32：上游 crosire/reshade-docs 已停止分发，改为
	// components\ 手动投递为主，不设固定基准（见 fetch.go 与 安全修复说明.md）
}

type integrityBaseline map[string]string

func baselinePath() string { return filepath.Join(cacheDir, "integrity-baseline.json") }

func loadBaseline() integrityBaseline {
	b := integrityBaseline{}
	if data, err := os.ReadFile(baselinePath()); err == nil {
		_ = json.Unmarshal(data, &b)
	}
	return b
}

func saveBaseline(b integrityBaseline) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err == nil {
		_ = os.WriteFile(baselinePath(), data, 0o644)
	}
}

// shortHash 防御性截断：基准文件被损坏时不应 panic
func shortHash(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// checkIntegrity 校验文件完整性：优先固定基准，其次 TOFU 运行期基准（首次自动记录）。
// 返回 false 表示校验未通过，调用方应删除该文件并按获取失败处理。
func checkIntegrity(path, name string) bool {
	sum := sha256sum(path)
	if sum == "" {
		logf("WARN", "完整性校验：无法读取 %s", name)
		return false
	}
	if want, ok := pinnedSHA256[name]; ok {
		if !strings.EqualFold(want, sum) {
			logf("ERROR", "完整性校验失败: %s · SHA-256 不符（期望 %s…，实际 %s…）", name, shortHash(want), shortHash(sum))
			return false
		}
		logf("OK", "完整性校验通过（固定基准）: %s", name)
		return true
	}
	b := loadBaseline()
	if base, ok := b[name]; ok {
		if !strings.EqualFold(base, sum) {
			logf("ERROR", "完整性校验失败: %s · 与首次记录基准不符（基准 %s…，实际 %s…）", name, shortHash(base), shortHash(sum))
			return false
		}
		logf("OK", "完整性校验通过（运行期基准）: %s", name)
		return true
	}
	b[name] = sum
	saveBaseline(b)
	logf("INFO", "记录完整性基准（首次获取）: %s · SHA-256 %s", name, sum)
	return true
}

// logComponentHash 对用户手动投递的 components\ 文件记录哈希，
// 供人工与官方渠道核对（不强制比对，投递文件本就由用户自担来源）。
func logComponentHash(path string) {
	if sum := sha256sum(path); sum != "" {
		logf("INFO", "手动投递组件哈希: %s · SHA-256 %s", filepath.Base(path), sum)
	}
}

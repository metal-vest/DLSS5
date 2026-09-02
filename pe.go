// pe.go — PE 解析：位数 / 导入 API 粗扫 / 旧子系统判定
// 移植自 core.ps1 的 Get-PeInfo
package main

import (
	"encoding/binary"
	"os"
	"strings"
)

type peInfo struct {
	Path           string
	Bitness        string // "32" | "64" | "unknown"
	Imports        []string
	SubsystemMajor int
	IsUe3Era       bool
	Valid          bool
}

var knownImports = []string{
	"d3d8.dll", "d3d9.dll", "d3d10.dll", "d3d11.dll", "d3d12.dll",
	"dxgi.dll", "vulkan-1.dll", "opengl32.dll",
	"dxvk_d3d9.dll", "dxvk_d3d11.dll",
}

func parsePe(exePath string) peInfo {
	info := peInfo{Path: exePath, Bitness: "unknown"}
	f, err := os.Open(exePath)
	if err != nil {
		return info
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < 264 {
		return info
	}
	hdr := make([]byte, 4096)
	n, _ := f.Read(hdr)
	if n < 264 {
		return info
	}
	eLfanew := int(binary.LittleEndian.Uint32(hdr[0x3C:]))
	if eLfanew <= 0 || eLfanew+264 > n {
		return info
	}
	if binary.LittleEndian.Uint32(hdr[eLfanew:]) != 0x00004550 { // 'PE\0\0'
		return info
	}
	machine := binary.LittleEndian.Uint16(hdr[eLfanew+4:])
	switch machine {
	case 0x014C:
		info.Bitness = "32"
	case 0x8664, 0xAA64:
		info.Bitness = "64"
	}
	optStart := eLfanew + 4 + 20
	if optStart+50 <= n {
		info.SubsystemMajor = int(binary.LittleEndian.Uint16(hdr[optStart+48:]))
	}
	info.IsUe3Era = info.SubsystemMajor > 0 && info.SubsystemMajor <= 5
	info.Valid = true

	// 导入表粗扫：读前 12MB 做 ASCII 包含匹配（与 PS1 原型一致）
	cap64 := int64(12 * 1024 * 1024)
	if st.Size() < cap64 {
		cap64 = st.Size()
	}
	buf := make([]byte, cap64)
	if _, err := f.ReadAt(buf, 0); err != nil && cap64 <= int64(n) {
		copy(buf, hdr)
	}
	ascii := strings.ToLower(string(buf))
	for _, k := range knownImports {
		if strings.Contains(ascii, k) {
			info.Imports = append(info.Imports, strings.ToLower(k))
		}
	}
	return info
}

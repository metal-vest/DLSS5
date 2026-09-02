// winver.go — 通过 version.dll 读取 PE 版本资源（ReShade / DXVK 识别）
package main

import (
	"fmt"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	modversion            = syscall.NewLazyDLL("version.dll")
	procGetFileVersionSz  = modversion.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInf = modversion.NewProc("GetFileVersionInfoW")
	procVerQueryValueW    = modversion.NewProc("VerQueryValueW")
)

type vsFixedFileInfo struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
	FileFlagsMask    uint32
	FileFlags        uint32
	FileOS           uint32
	FileType         uint32
	FileSubtype      uint32
	FileDateMS       uint32
	FileDateLS       uint32
}

type fileVerInfo struct {
	OK                                        bool
	ProductName                               string
	CompanyName                               string
	FileDescription                           string
	VerMajor, VerMinor, VerBuild, VerRevision int
}

func verString(p *byte, block string) string {
	sub, err := syscall.UTF16PtrFromString(block)
	if err != nil {
		return ""
	}
	var bufPtr unsafe.Pointer
	var bufLen uint32
	r1, _, _ := procVerQueryValueW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(sub)),
		uintptr(unsafe.Pointer(&bufPtr)),
		uintptr(unsafe.Pointer(&bufLen)),
	)
	if r1 == 0 || bufLen == 0 || bufPtr == nil {
		return ""
	}
	// puLen 语义在不同实现里是字节/字符不一致：用指针偏移算可用空间，安全上限
	base := uintptr(unsafe.Pointer(&verBuf[0]))
	avail := uintptr(verBufSize) - (uintptr(bufPtr) - base)
	chars := avail / 2
	if int(chars) > int(bufLen) {
		chars = uintptr(bufLen)
	}
	u16 := unsafe.Slice((*uint16)(bufPtr), chars)
	s := strings.TrimRight(string(utf16.Decode(u16)), "\x00")
	if i := strings.IndexRune(s, 0); i >= 0 {
		s = s[:i]
	}
	return s
}

var (
	verBuf     []byte
	verBufSize uintptr
)

func fileVerInfoOf(path string) fileVerInfo {
	var vi fileVerInfo
	p16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return vi
	}
	size, _, _ := procGetFileVersionSz.Call(uintptr(unsafe.Pointer(p16)), 0)
	if size == 0 {
		return vi
	}
	buf := make([]byte, size)
	if r1, _, _ := procGetFileVersionInf.Call(uintptr(unsafe.Pointer(p16)), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(size)); r1 == 0 {
		return vi
	}
	verBuf = buf
	verBufSize = uintptr(size)
	data := buf
	// 固定信息（VS_FIXEDFILEINFO）
	var fixedPtr unsafe.Pointer
	var fixedLen uint32
	root, _ := syscall.UTF16PtrFromString("\\")
	if r1, _, _ := procVerQueryValueW.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&fixedPtr)), uintptr(unsafe.Pointer(&fixedLen))); r1 != 0 && fixedLen >= 13 {
		f := (*vsFixedFileInfo)(fixedPtr)
		vi.VerMajor = int(f.FileVersionMS >> 16)
		vi.VerMinor = int(f.FileVersionMS & 0xFFFF)
		vi.VerBuild = int(f.FileVersionLS >> 16)
		vi.VerRevision = int(f.FileVersionLS & 0xFFFF)
	}
	// 字符串表（取第一个翻译项：首 DWORD 低位=lang，高位=codepage）
	var trPtr unsafe.Pointer
	var trLen uint32
	trPath, _ := syscall.UTF16PtrFromString("\\VarFileInfo\\Translation")
	if r1, _, _ := procVerQueryValueW.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(trPath)), uintptr(unsafe.Pointer(&trPtr)), uintptr(unsafe.Pointer(&trLen))); r1 != 0 && trLen >= 4 {
		b := unsafe.Slice((*byte)(trPtr), trLen)
		lang := uint16(b[0]) | uint16(b[1])<<8
		cp := uint16(b[2]) | uint16(b[3])<<8
		base := "\\StringFileInfo\\" + hex4(lang) + hex4(cp) + "\\"
		vi.ProductName = verString(&data[0], base+"ProductName")
		vi.CompanyName = verString(&data[0], base+"CompanyName")
		vi.FileDescription = verString(&data[0], base+"FileDescription")
		vi.OK = true
	}
	return vi
}

func hex4(v uint16) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(v>>12)&0xF], digits[(v>>8)&0xF], digits[(v>>4)&0xF], digits[v&0xF]})
}

func verStringMatch(vi fileVerInfo, needle string) bool {
	n := strings.ToLower(needle)
	return strings.Contains(strings.ToLower(vi.ProductName), n) ||
		strings.Contains(strings.ToLower(vi.CompanyName), n) ||
		strings.Contains(strings.ToLower(vi.FileDescription), n)
}

// 是否为 ReShade DLL；附带版本号
func isReShadeDll(dllPath string) (bool, string, int, int, int) {
	if !fileExists(dllPath) {
		return false, "", 0, 0, 0
	}
	vi := fileVerInfoOf(dllPath)
	if vi.OK && verStringMatch(vi, "reshade") {
		return true, fmt.Sprintf("%d.%d.%d", vi.VerMajor, vi.VerMinor, vi.VerBuild), vi.VerMajor, vi.VerMinor, vi.VerBuild
	}
	return false, "", 0, 0, 0
}

// 是否为 DXVK DLL
func isDxvkDll(dllPath string) bool {
	if !fileExists(dllPath) {
		return false
	}
	vi := fileVerInfoOf(dllPath)
	return vi.OK && verStringMatch(vi, "dxvk")
}

// core_test.go — 安全修复配套单元测试（仅用标准库；需在 Windows 上运行，
// 因同包 ui.go 依赖 lxn/walk。CI 见 .github/workflows/build.yml）
package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initTestDirs(t *testing.T) {
	t.Helper()
	rootDir = t.TempDir()
	cacheDir = filepath.Join(rootDir, "cache")
	compDir = filepath.Join(rootDir, "components")
	logsDir = filepath.Join(rootDir, "logs")
	logFile = filepath.Join(logsDir, "test.log")
	os.MkdirAll(cacheDir, 0o755)
	os.MkdirAll(compDir, 0o755)
	os.MkdirAll(logsDir, 0o755)
}

// F-10：INI 合并多次执行必须产出字节级一致的结果（键序确定化）
func TestIniSetKeysDeterministic(t *testing.T) {
	initTestDirs(t)
	path := filepath.Join(rootDir, "t.ini")
	os.WriteFile(path, []byte("[OTHER]\r\nFoo=1\r\n\r\n[GENERAL]\r\nZed=9\r\n"), 0o644)

	keys := map[string]string{
		"PresetPath":         "ReShadePreset.ini",
		"EffectSearchPaths":  `.\reshade-shaders\Shaders\**`,
		"TextureSearchPaths": `.\reshade-shaders\Textures\**`,
	}
	iniSetKeys(path, "GENERAL", keys)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	// 在首次产物基础上反复重跑，键序不得抖动
	for i := 0; i < 5; i++ {
		iniSetKeys(path, "GENERAL", keys)
		again, _ := os.ReadFile(path)
		if string(again) != string(first) {
			t.Fatalf("第 %d 次重跑产出不一致：\n--- first ---\n%q\n--- again ---\n%q", i+2, first, again)
		}
	}
	for _, want := range []string{"EffectSearchPaths=", "TextureSearchPaths=", "PresetPath="} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("缺少键 %s：\n%s", want, first)
		}
	}
}

// F-11：zip 条目路径穿越必须被拒收
func TestExpandZipTraversalRejected(t *testing.T) {
	initTestDirs(t)
	zp := filepath.Join(cacheDir, "evil.zip")
	buf := newZipBuilder(t)
	buf.add("../evil.txt", "pwned")
	buf.write(zp)

	dest := filepath.Join(rootDir, "out")
	if err := expandZip(zp, dest); err != nil {
		t.Fatalf("含穿越条目的 zip 不应整体报错（条目应被跳过）: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "evil.txt")); err == nil {
		t.Fatal("路径穿越条目被写出，防护失效")
	}
}

// F-11：正常 zip 必须能解出
func TestExpandZipNormal(t *testing.T) {
	initTestDirs(t)
	zp := filepath.Join(cacheDir, "ok.zip")
	buf := newZipBuilder(t)
	buf.add("a/b.txt", "hello")
	buf.add("top.txt", "world")
	buf.write(zp)

	dest := filepath.Join(rootDir, "out2")
	if err := expandZip(zp, dest); err != nil {
		t.Fatalf("正常 zip 解压失败: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "a", "b.txt")); err != nil || string(data) != "hello" {
		t.Fatalf("子目录条目解压异常: %v %q", err, data)
	}
}

// F-01：TOFU 基准——首次记录、二次通过、篡改后拒收
func TestCheckIntegrityTOFU(t *testing.T) {
	initTestDirs(t)
	p := filepath.Join(cacheDir, "sample.bin")
	os.WriteFile(p, []byte("v1-content"), 0o644)

	if !checkIntegrity(p, "sample.bin") {
		t.Fatal("首次获取应记录基准并放行")
	}
	if !checkIntegrity(p, "sample.bin") {
		t.Fatal("与基准一致应放行")
	}
	os.WriteFile(p, []byte("v2-TAMPERED"), 0o644)
	if checkIntegrity(p, "sample.bin") {
		t.Fatal("篡改后必须拒收")
	}
}

// F-01：固定基准优先于 TOFU 基准
func TestCheckIntegrityPinnedPriority(t *testing.T) {
	initTestDirs(t)
	name := "ReShade.fxh" // pinnedSHA256 中已有固定基准
	p := filepath.Join(cacheDir, name)
	os.WriteFile(p, []byte("not-the-real-header"), 0o644)
	if checkIntegrity(p, name) {
		t.Fatal("与固定基准不符必须拒收")
	}
}

// F-12：导入表探测的边界正则应拒绝"内嵌子串"误报、接受真实导入名
func TestImportNameBoundary(t *testing.T) {
	pos := "version resources\x00d3d11.dll\x00other"
	neg := "this game mentions myd3d11.dll inside a string"
	if !importNameRes[3].MatchString(pos) {
		t.Fatal("真实导入名应命中")
	}
	if importNameRes[3].MatchString(neg) {
		t.Fatal("内嵌子串不应命中（边界正则失效）")
	}
}

// 辅助：内存 zip 构造器
type zipBuilder struct {
	t     *testing.T
	files []zipEntry
}

type zipEntry struct {
	name string
	data string
}

func newZipBuilder(t *testing.T) *zipBuilder { return &zipBuilder{t: t} }

func (b *zipBuilder) add(name, data string) { b.files = append(b.files, zipEntry{name, data}) }

func (b *zipBuilder) write(path string) {
	b.t.Helper()
	f, err := os.Create(path)
	if err != nil {
		b.t.Fatalf("创建 zip 失败: %v", err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, e := range b.files {
		fw, err := w.Create(e.name)
		if err != nil {
			b.t.Fatalf("写入条目失败: %v", err)
		}
		fw.Write([]byte(e.data))
	}
	w.Close()
}

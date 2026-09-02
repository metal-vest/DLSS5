// core.go — DLSS5 一键开启工具（Go 原生版）· 基础工具库
// 移植自 pkg/tools/core.ps1（PS1 原型），逻辑保持一致
package main

import (
        "archive/zip"
        "context"
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "io"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "regexp"
        "sort"
        "strings"
        "time"
)

// 2.1.1：安全修复版修订（F-01~F-12 + 构建修正，详见 安全修复说明.md）
const toolVersion = "2.1.1"

// ============ 全局状态 ============
var (
        rootDir  string // 工具根目录（exe 所在目录，不可写时回退 %LOCALAPPDATA%）
        cacheDir string // 下载缓存
        compDir  string // 手动投递组件目录
        logsDir  string // 日志目录
        logFile  string // 当前日志文件

        // UI 钩子（由 ui.go 注入，已做跨线程封送；静默模式为 nil）
        uiLogFn      func(line string)
        uiProgressFn func(pct int, label string) // pct==-1 表示不确定进度
        uiStatusFn   func(text string, kind string)
        uiMsgBoxFn   func(title, text string, warn bool)
        uiConfirmFn  func(title, text string) bool // 是/否确认框（F-03：第三方安装器执行前征询）
)

func initDirs(exeDir string) {
        rootDir = strings.TrimRight(exeDir, `\`)
        cacheDir = filepath.Join(rootDir, "cache")
        compDir = filepath.Join(rootDir, "components")
        logsDir = filepath.Join(rootDir, "logs")
        if err := os.MkdirAll(cacheDir, 0o755); err != nil {
                // exe 目录不可写（如 Program Files）→ 回退用户目录
                rootDir = filepath.Join(os.Getenv("LOCALAPPDATA"), "DLSS5Feeder")
                cacheDir = filepath.Join(rootDir, "cache")
                compDir = filepath.Join(rootDir, "components")
                logsDir = filepath.Join(rootDir, "logs")
                os.MkdirAll(cacheDir, 0o755)
        }
        os.MkdirAll(compDir, 0o755)
        os.MkdirAll(logsDir, 0o755)
        logFile = filepath.Join(logsDir, "oneclick-"+time.Now().Format("20060102-150405")+".log")
        logLine("INFO", fmt.Sprintf("DLSS5 一键开启工具(Go 原生版) v%s · 根目录: %s", toolVersion, rootDir))
}

// ============ 日志 ============
func logLine(level, msg string) string {
        line := fmt.Sprintf("[%s] [%-5s] %s", time.Now().Format("15:04:05"), level, msg)
        f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
        if err == nil {
                f.WriteString(line + "\r\n")
                f.Close()
        }
        if uiLogFn != nil {
                uiLogFn(line)
        }
        return line
}

func logf(level, format string, a ...interface{}) { logLine(level, fmt.Sprintf(format, a...)) }

func setProgress(pct int, label string) {
        if uiProgressFn != nil {
                uiProgressFn(pct, label)
        }
}

func setStatus(text, kind string) {
        logLine("INFO", text)
        if uiStatusFn != nil {
                uiStatusFn(text, kind)
        }
}

// ============ UTF-8 BOM 写入（ReShade 配置习惯） ============
func writeTextBOM(path, text string) error {
        f, err := os.Create(path)
        if err != nil {
                return err
        }
        defer f.Close()
        f.Write([]byte{0xEF, 0xBB, 0xBF})
        _, err = f.WriteString(text)
        return err
}

// ============ INI 读写（保留其他节的原样合并） ============
// sortedKeys 返回排序后的键序（F-10：map 迭代无序，补键阶段必须确定化，
// 保证多次执行产出字节级一致的 INI）
func sortedKeys(m map[string]string) []string {
        ks := make([]string, 0, len(m))
        for k := range m {
                ks = append(ks, k)
        }
        sort.Strings(ks)
        return ks
}

func iniSetKeys(path, section string, keys map[string]string) {
        var lines []string
        if data, err := os.ReadFile(path); err == nil {
                raw := strings.ReplaceAll(string(data), "\r\n", "\n")
                raw = strings.ReplaceAll(raw, "\r", "\n")
                lines = strings.Split(raw, "\n")
                if len(lines) > 0 && lines[len(lines)-1] == "" {
                        lines = lines[:len(lines)-1]
                }
        }

        // 先写"[section] 内已存在的键"（原位替换），再决定追加位置
        written := map[string]bool{}
        inSection := false
        sectionExists := false
        var out []string
        for _, line := range lines {
                t := strings.TrimSpace(line)
                if sectRe.MatchString(t) {
                        m := sectRe.FindStringSubmatch(t)
                        isTarget := strings.EqualFold(strings.TrimSpace(m[1]), section)
                        if isTarget {
                                sectionExists = true
                        }
                        if inSection && !isTarget {
                                // 上一个节结束：补齐未写键（F-10：按排序键序写入）
                                for _, k := range sortedKeys(keys) {
                                        if !written[k] {
                                                out = append(out, k+"="+keys[k])
                                                written[k] = true
                                        }
                                }
                        }
                        inSection = isTarget
                        out = append(out, line)
                        continue
                }
                if inSection && kvRe.MatchString(t) {
                        m := kvRe.FindStringSubmatch(t)
                        k := strings.TrimSpace(m[1])
                        // F-10：按排序键序匹配，同义大小写冲突时结果确定
                        for _, target := range sortedKeys(keys) {
                                if strings.EqualFold(k, target) {
                                        out = append(out, target+"="+keys[target])
                                        written[target] = true
                                        goto next
                                }
                        }
                }
                out = append(out, line)
        next:
        }
        if !sectionExists {
                out = append(out, "["+section+"]")
                for _, k := range sortedKeys(keys) {
                        out = append(out, k+"="+keys[k])
                        written[k] = true
                }
        } else if inSection {
                for _, k := range sortedKeys(keys) {
                        if !written[k] {
                                out = append(out, k+"="+keys[k])
                                written[k] = true
                        }
                }
        }
        writeTextBOM(path, strings.Join(out, "\r\n")+"\r\n")
}

var sectRe = regexp.MustCompile(`^\[(.+)\]$`)
var kvRe = regexp.MustCompile(`^([^=;#]+)=`)

func iniRemoveKeys(path, section string, keyNames []string) {
        if !fileExists(path) {
                return
        }
        data, _ := os.ReadFile(path)
        raw := strings.ReplaceAll(string(data), "\r\n", "\n")
        raw = strings.ReplaceAll(raw, "\r", "\n")
        var out []string
        inSection := false
        for _, line := range strings.Split(raw, "\n") {
                t := strings.TrimSpace(line)
                if sectRe.MatchString(t) {
                        m := sectRe.FindStringSubmatch(t)
                        inSection = strings.EqualFold(strings.TrimSpace(m[1]), section)
                        out = append(out, line)
                        continue
                }
                if inSection && kvRe.MatchString(t) {
                        m := kvRe.FindStringSubmatch(t)
                        k := strings.TrimSpace(m[1])
                        remove := false
                        for _, n := range keyNames {
                                if strings.EqualFold(n, k) {
                                        remove = true
                                        break
                                }
                        }
                        if remove {
                                continue
                        }
                }
                out = append(out, line)
        }
        writeTextBOM(path, strings.Join(out, "\r\n")+"\r\n")
}

// ============ 下载（带超时/重试/UA/缓存/进度/完整性校验） ============
func download(url, outFile string, retries int, label string) bool {
        name := filepath.Base(outFile)
        if fileExists(outFile) {
                // F-05/F-01：缓存命中也必须过完整性校验，防残缺缓存或被篡改缓存被直接使用
                if checkIntegrity(outFile, name) {
                        logf("OK", "缓存命中且校验通过: %s", label)
                        return true
                }
                logf("WARN", "缓存完整性校验未通过，删除后重新下载: %s", name)
                os.Remove(outFile)
        }
        tmp := outFile + ".downloading"
        os.Remove(tmp)
        // F-06：响应头/握手空闲超时，连接或服务端挂起时快速失败
        client := &http.Client{
                Transport: &http.Transport{
                        TLSHandshakeTimeout:   10 * time.Second,
                        ResponseHeaderTimeout: 30 * time.Second,
                },
        }
        // F-06：整体超时兑底（下载链路最大构件 < 100MB，30 分钟余量充足），杜绝永久挂起
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
        defer cancel()
        for i := 1; i <= retries; i++ {
                logf("INFO", "下载 %s（第 %d/%d 次）", label, i, retries)
                setProgress(-1, "下载 "+label)
                req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
                if err != nil {
                        logf("ERROR", "下载失败: %s · %v", label, err)
                        continue
                }
                req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) DLSS5-OneClick-Go")
                resp, err := client.Do(req)
                if err != nil {
                        logf("ERROR", "下载失败: %s · %v", label, err)
                        if i < retries {
                                time.Sleep(time.Duration(2*i) * time.Second)
                        }
                        continue
                }
                if resp.StatusCode != 200 {
                        resp.Body.Close()
                        logf("ERROR", "下载失败: %s · HTTP %d", label, resp.StatusCode)
                        if i < retries {
                                time.Sleep(time.Duration(2*i) * time.Second)
                        }
                        continue
                }
                total := resp.ContentLength
                f, err := os.Create(tmp)
                if err != nil {
                        resp.Body.Close()
                        logf("ERROR", "无法写入 %s: %v", tmp, err)
                        continue
                }
                var done int64
                buf := make([]byte, 256*1024)
                lastReport := time.Now()
                var copyErr error
                for {
                        n, rerr := resp.Body.Read(buf)
                        if n > 0 {
                                if _, werr := f.Write(buf[:n]); werr != nil {
                                        copyErr = werr
                                        break
                                }
                                done += int64(n)
                                if time.Since(lastReport) > 300*time.Millisecond {
                                        lastReport = time.Now()
                                        if total > 0 {
                                                setProgress(int(done*100/total), fmt.Sprintf("下载 %s · %s / %s", label, humanBytes(done), humanBytes(total)))
                                        } else {
                                                setProgress(-1, fmt.Sprintf("下载 %s · %s", label, humanBytes(done)))
                                        }
                                }
                        }
                        if rerr != nil {
                                // F-05：只有 io.EOF 才算正常结束；连接重置/意外 EOF 等一律视为中断，
                                // 不把残缺文件晋升为缓存
                                if rerr != io.EOF {
                                        copyErr = rerr
                                }
                                break
                        }
                }
                f.Close()
                resp.Body.Close()
                if copyErr != nil {
                        os.Remove(tmp)
                        logf("ERROR", "下载中断: %s · %v", label, copyErr)
                        if i < retries {
                                time.Sleep(time.Duration(2*i) * time.Second)
                        }
                        continue
                }
                st, _ := os.Stat(tmp)
                if st == nil || st.Size() < 1 {
                        logf("ERROR", "下载内容为空: %s", label)
                        continue
                }
                // F-05：与 Content-Length 比对，缺字节即判失败
                if total > 0 && st.Size() != total {
                        os.Remove(tmp)
                        logf("ERROR", "下载不完整: %s · %s / %s", label, humanBytes(st.Size()), humanBytes(total))
                        continue
                }
                os.Remove(outFile)
                if err := os.Rename(tmp, outFile); err != nil {
                        copyFile(tmp, outFile)
                        os.Remove(tmp)
                }
                // F-01：落盘后立即做完整性校验（有基准值则强制比对，不符即拒收）
                if !checkIntegrity(outFile, name) {
                        os.Remove(outFile)
                        setProgress(0, "")
                        continue
                }
                setProgress(0, "")
                logf("OK", "完成: %s (%s)", label, humanBytes(st.Size()))
                return true
        }
        os.Remove(tmp)
        return false
}

func humanBytes(n int64) string {
        switch {
        case n >= 1024*1024*1024:
                return fmt.Sprintf("%.2f GB", float64(n)/(1024*1024*1024))
        case n >= 1024*1024:
                return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
        case n >= 1024:
                return fmt.Sprintf("%.0f KB", float64(n)/1024)
        default:
                return fmt.Sprintf("%d B", n)
        }
}

// ============ ZIP 安全解压（F-11：体积/条目限额 + 压缩炸弹防护） ============
const (
        maxZipEntries           = 4096      // 条目数上限
        maxZipEntrySize         = 256 << 20 // 单条目解压上限 256MB
        maxZipTotalUncompressed = 1 << 30   // 解压总量上限 1GB
)

func expandZip(zipPath, destDir string) error {
        r, err := zip.OpenReader(zipPath)
        if err != nil {
                return err
        }
        defer r.Close()
        if len(r.File) > maxZipEntries {
                return fmt.Errorf("zip 条目数 %d 超过上限 %d", len(r.File), maxZipEntries)
        }
        // 打开成功后才清空目标目录，避免把已有的完好解压产物误删
        os.RemoveAll(destDir)
        os.MkdirAll(destDir, 0o755)
        var total int64
        for _, f := range r.File {
                if f.UncompressedSize64 > maxZipEntrySize {
                        return fmt.Errorf("条目 %s 解压后 %s 超过单条目上限", f.Name, humanBytes(int64(f.UncompressedSize64)))
                }
                total += int64(f.UncompressedSize64)
                if total > maxZipTotalUncompressed {
                        return fmt.Errorf("zip 解压总量超过 %s，疑似压缩炸弹", humanBytes(maxZipTotalUncompressed))
                }
                name := filepath.FromSlash(f.Name)
                // 路径穿越防护：拒绝 ..、盘符与绝对路径
                if strings.Contains(name, "..") || filepath.VolumeName(name) != "" || strings.HasPrefix(name, `\`) || strings.HasPrefix(name, "/") {
                        logf("WARN", "zip 条目路径可疑，已跳过: %s", f.Name)
                        continue
                }
                target := filepath.Join(destDir, name)
                // 双重保险：解压产物必须落在目标目录内
                if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
                        logf("WARN", "zip 条目逃逸目标目录，已跳过: %s", f.Name)
                        continue
                }
                if f.FileInfo().IsDir() {
                        os.MkdirAll(target, 0o755)
                        continue
                }
                os.MkdirAll(filepath.Dir(target), 0o755)
                rc, err := f.Open()
                if err != nil {
                        return fmt.Errorf("打开条目 %s 失败: %w", f.Name, err)
                }
                // 限额读取：读到的字节超过声明大小即异常（CRC 由 archive/zip 在 EOF 时校验）
                out, err := os.Create(target)
                if err != nil {
                        rc.Close()
                        return fmt.Errorf("写出条目 %s 失败: %w", f.Name, err)
                }
                _, err = io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
                out.Close()
                rc.Close()
                if err != nil {
                        return fmt.Errorf("解压条目 %s 失败: %w", f.Name, err)
                }
                if st, stErr := os.Stat(target); stErr == nil && st.Size() > int64(f.UncompressedSize64) {
                        return fmt.Errorf("条目 %s 实际解压字节超出声明大小", f.Name)
                }
        }
        return nil
}

// ============ 通用文件操作 ============
func fileExists(p string) bool {
        st, err := os.Stat(p)
        return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
        st, err := os.Stat(p)
        return err == nil && st.IsDir()
}

func ensureDir(p string) { os.MkdirAll(p, 0o755) }

func copyFile(src, dst string) error {
        in, err := os.Open(src)
        if err != nil {
                return err
        }
        defer in.Close()
        os.MkdirAll(filepath.Dir(dst), 0o755)
        out, err := os.Create(dst)
        if err != nil {
                return err
        }
        defer out.Close()
        _, err = io.Copy(out, in)
        return err
}

// Source 允许为空（组件缺失时静默跳过）
func copyIfExists(src, destDir, newName string) bool {
        if src != "" && fileExists(src) {
                name := newName
                if name == "" {
                        name = filepath.Base(src)
                }
                return copyFile(src, filepath.Join(destDir, name)) == nil
        }
        return false
}

func sha256sum(path string) string {
        f, err := os.Open(path)
        if err != nil {
                return ""
        }
        defer f.Close()
        h := sha256.New()
        io.Copy(h, f)
        return hex.EncodeToString(h.Sum(nil))
}

// 在父目录（含子目录，深度限制）中查找同名文件，返回最近修改的第一个
func findFileRecursive(rootDir, fileName string, maxDepth int) string {
        if !dirExists(rootDir) {
                return ""
        }
        baseDepth := strings.Count(strings.TrimRight(rootDir, `\`), string(os.PathSeparator)) + 1
        var hits []string
        filepath.WalkDir(rootDir, func(p string, d os.DirEntry, err error) error {
                if err != nil {
                        return nil
                }
                depth := strings.Count(p, string(os.PathSeparator)) + 1
                if d.IsDir() {
                        if depth > baseDepth+maxDepth {
                                return filepath.SkipDir
                        }
                        return nil
                }
                if strings.EqualFold(filepath.Base(p), fileName) && depth <= baseDepth+maxDepth {
                        hits = append(hits, p)
                }
                return nil
        })
        if len(hits) == 0 {
                return ""
        }
        type dated struct {
                p string
                t time.Time
        }
        var ds []dated
        for _, p := range hits {
                st, err := os.Stat(p)
                t := time.Time{}
                if err == nil {
                        t = st.ModTime()
                }
                ds = append(ds, dated{p, t})
        }
        sort.Slice(ds, func(i, j int) bool { return ds[i].t.After(ds[j].t) })
        return ds[0].p
}

// 从"本机已有 DLSS 游戏 / Steam 库"中提取 nvngx_dlss.dll
func findLocalNvNgx() string {
        var roots []string
        seen := map[string]bool{}
        addRoot := func(p string) {
                if p != "" && dirExists(p) && !seen[p] {
                        seen[p] = true
                        roots = append(roots, p)
                }
        }
        // libraryfolders.vdf 解析
        candidates := []string{
                filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam", "steamapps"),
                filepath.Join(os.Getenv("ProgramFiles"), "Steam", "steamapps"),
        }
        vdfRe := regexp.MustCompile(`"path"\s+"([^"]+)"`)
        for _, lib := range candidates {
                vdf := filepath.Join(lib, "libraryfolders.vdf")
                if data, err := os.ReadFile(vdf); err == nil {
                        for _, m := range vdfRe.FindAllStringSubmatch(string(data), -1) {
                                p := strings.ReplaceAll(m[1], `\\`, `\`)
                                addRoot(filepath.Join(p, "steamapps", "common"))
                        }
                }
        }
        for _, drv := range []string{"C:", "D:", "E:", "F:", "G:"} {
                addRoot(drv + `\SteamLibrary\steamapps\common`)
        }
        for _, r := range roots {
                hit := findFileRecursive(r, "nvngx_dlss.dll", 3)
                if hit != "" {
                        logf("OK", "在本机找到 nvngx_dlss.dll: %s", hit)
                        return hit
                }
        }
        return ""
}

// ============ PowerShell 助手 ============
// F-04：支持以环境变量向脚本传参，调用方不得把路径拼接进脚本字符串
func runPS(script string, env ...string) (string, error) {
        cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
        if len(env) > 0 {
                cmd.Env = append(os.Environ(), env...)
        }
        out, err := cmd.Output()
        return strings.TrimSpace(string(out)), err
}

// 静默运行外部程序，返回退出码
func runExe(path string, args ...string) int {
        cmd := exec.Command(path, args...)
        cmd.Stdout = nil
        cmd.Stderr = nil
        err := cmd.Run()
        if err == nil {
                return 0
        }
        if ee, ok := err.(*exec.ExitError); ok {
                return ee.ExitCode()
        }
        return -1
}

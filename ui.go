//go:build windows

// ui.go — 原生 Win32 图形界面（lxn/walk）
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type gui struct {
	mw        *walk.MainWindow
	lePath    *walk.LineEdit
	teDetect  *walk.TextEdit
	lblHint   *walk.Label
	lblProg   *walk.Label
	lblStatus *walk.Label
	pb        *walk.ProgressBar
	teLog     *walk.TextEdit
	btnEnv    *walk.PushButton
	btnInst   *walk.PushButton
	btnUninst *walk.PushButton
	cbShort   *walk.CheckBox

	busy     int32
	analysis *analysis
}

func runGUI() {
	g := &gui{}

	err := MainWindow{
		AssignTo: &g.mw,
		Title:    fmt.Sprintf("DLSS5 一键开启工具 v%s · Go 原生版", toolVersion),
		MinSize:  Size{Width: 780, Height: 640},
		Size:     Size{Width: 860, Height: 720},
		Font:     Font{Family: "Microsoft YaHei UI", PointSize: 9},
		Layout:   VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 8},

		Children: []Widget{
			GroupBox{
				Title:  "① 选择游戏",
				Layout: VBox{Spacing: 4},
				Children: []Widget{
					Composite{
						Layout: HBox{Spacing: 6, MarginsZero: true},
						Children: []Widget{
							LineEdit{AssignTo: &g.lePath, CueBanner: "游戏主程序的完整路径（…\\YourGame.exe）"},
							PushButton{Text: "浏览…", OnClicked: g.onBrowse},
						},
					},
					Label{AssignTo: &g.lblHint, Text: "提示：也可以直接把游戏 exe 从资源管理器拖进本窗口", TextColor: walk.RGB(100, 100, 100)},
					TextEdit{
						AssignTo: &g.teDetect, ReadOnly: true, VScroll: true,
						MinSize: Size{Height: 64}, MaxSize: Size{Height: 110},
						Text: "（尚未选择游戏）",
					},
				},
			},
			GroupBox{
				Title:  "② 操作",
				Layout: VBox{Spacing: 6},
				Children: []Widget{
					Composite{
						Layout: HBox{Spacing: 8, MarginsZero: true},
						Children: []Widget{
							PushButton{AssignTo: &g.btnEnv, Text: "环境体检", MinSize: Size{Width: 110}, OnClicked: g.onEnvCheck},
							PushButton{AssignTo: &g.btnInst, Text: "一键开启", MinSize: Size{Width: 110}, OnClicked: g.onInstall},
							PushButton{AssignTo: &g.btnUninst, Text: "一键卸载", MinSize: Size{Width: 110}, OnClicked: g.onUninstall},
							CheckBox{AssignTo: &g.cbShort, Text: "创建桌面启动器", Checked: true},
							VSpacer{},
						},
					},
					ProgressBar{AssignTo: &g.pb, MinValue: 0, MaxValue: 100},
					Label{AssignTo: &g.lblProg, Text: "就绪。首次开启需要联网下载组件（约 30MB，之后走缓存）"},
				},
			},
			TextEdit{
				AssignTo: &g.teLog, ReadOnly: true, VScroll: true,
				Font: Font{Family: "Consolas", PointSize: 8},
			},
			Label{
				AssignTo: &g.lblStatus, Text: "状态：待命",
				TextColor: walk.RGB(0, 128, 0),
			},
		},
	}.Create()

	if err != nil {
		walk.MsgBox(nil, "启动失败", "窗口创建失败："+err.Error(), walk.MsgBoxIconError)
		return
	}

	// 拖拽支持
	g.mw.DropFiles().Attach(func(files []string) {
		for _, f := range files {
			if strings.EqualFold(filepath.Ext(f), ".exe") {
				g.pickGame(f)
				return
			}
		}
		// 拖进来的是目录：找里面第一个 exe
		for _, f := range files {
			if st, err := os.Stat(f); err == nil && st.IsDir() {
				entries, _ := os.ReadDir(f)
				for _, e := range entries {
					if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".exe") {
						g.pickGame(filepath.Join(f, e.Name()))
						return
					}
				}
			}
		}
		g.uiMsg("提示", "请拖入游戏主程序（.exe）", true)
	})

	// 注入 UI 钩子（跨线程封送）
	uiLogFn = func(line string) {
		g.mw.Synchronize(func() { g.appendLog(line) })
	}
	uiProgressFn = func(pct int, label string) {
		g.mw.Synchronize(func() { g.setProgress(pct, label) })
	}
	uiStatusFn = func(text, kind string) {
		g.mw.Synchronize(func() { g.setStatus(text, kind) })
	}
	uiMsgBoxFn = func(title, text string, warn bool) {
		g.mw.Synchronize(func() {
			style := walk.MsgBoxIconInformation
			if warn {
				style = walk.MsgBoxIconWarning
			}
			walk.MsgBox(g.mw, title, text, style)
		})
	}
	uiConfirmFn = func(title, text string) bool {
		var yes bool
		g.mw.Synchronize(func() {
			yes = walk.MsgBox(g.mw, title, text, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes
		})
		return yes
	}

	g.appendLog(fmt.Sprintf("DLSS5 一键开启工具(Go 原生版) v%s", toolVersion))
	g.appendLog("组件获取链路：cache 缓存 → components 手动投递 → 本机游戏扫描 → 社区镜像在线下载")
	g.mw.Run()
}

// ============ UI 辅助 ============
func (g *gui) appendLog(line string) {
	const cap = 80 * 1024
	cur := g.teLog.Text()
	if len(cur) > cap {
		// 附加修复：截断点对齐到 UTF-8 字符边界，避免切碎多字节字符产生乱码首行
		cut := len(cur) / 2
		for cut < len(cur) && !utf8.RuneStart(cur[cut]) {
			cut++
		}
		cur = cur[cut:]
		if i := strings.Index(cur, "\r\n"); i > 0 {
			cur = cur[i+2:]
		}
		g.teLog.SetText(cur)
	}
	g.teLog.AppendText(line + "\r\n")
}

func (g *gui) setProgress(pct int, label string) {
	if pct < 0 {
		g.pb.SetRange(0, 100)
		g.pb.SetValue(0)
		g.pb.SetMarqueeMode(true)
	} else {
		g.pb.SetMarqueeMode(false)
		g.pb.SetRange(0, 100)
		g.pb.SetValue(pct)
	}
	if label != "" {
		g.lblProg.SetText(label)
	} else if pct == 0 {
		g.lblProg.SetText("就绪")
	}
}

func (g *gui) setStatus(text, kind string) {
	g.lblStatus.SetText("状态：" + text)
	switch kind {
	case "ok", "STEP", "INFO":
		g.lblStatus.SetTextColor(walk.RGB(0, 128, 0))
	case "warn":
		g.lblStatus.SetTextColor(walk.RGB(200, 120, 0))
	case "error":
		g.lblStatus.SetTextColor(walk.RGB(200, 0, 0))
	default:
		g.lblStatus.SetTextColor(walk.RGB(0, 128, 0))
	}
}

func (g *gui) uiMsg(title, text string, warn bool) {
	if uiMsgBoxFn != nil {
		uiMsgBoxFn(title, text, warn)
	}
}

func (g *gui) setBusy(b bool) {
	g.mw.Synchronize(func() {
		g.btnEnv.SetEnabled(!b)
		g.btnInst.SetEnabled(!b)
		g.btnUninst.SetEnabled(!b)
	})
}

// ============ 事件 ============
func (g *gui) onBrowse() {
	dlg := walk.FileDialog{
		Title:  "选择游戏主程序",
		Filter: "可执行文件 (*.exe)|*.exe|所有文件 (*.*)|*.*",
	}
	if ok, _ := dlg.ShowOpen(g.mw); ok && dlg.FilePath != "" {
		g.pickGame(dlg.FilePath)
	}
}

func (g *gui) pickGame(exePath string) {
	g.teDetect.SetText("分析中…")
	a := analyzeGame(exePath)
	g.analysis = a
	var b strings.Builder
	fmt.Fprintf(&b, "游戏：%s\r\n识别：%s · 路由 [%s]\r\n", a.GameName, routeDescription(a), a.Route)
	if len(a.Notes) > 0 {
		fmt.Fprintf(&b, "说明：%s\r\n", strings.Join(a.Notes, "；"))
	}
	if len(a.Warnings) > 0 {
		fmt.Fprintf(&b, "警告：%s\r\n", strings.Join(a.Warnings, "；"))
	}
	g.teDetect.SetText(b.String())
	if a.PeValid {
		setStatus("已识别目标："+a.GameName+"（路由 "+a.Route+"）", "ok")
	} else {
		setStatus("exe 无法解析，请确认选对主程序", "warn")
	}
}

func (g *gui) onEnvCheck() {
	if !atomic.CompareAndSwapInt32(&g.busy, 0, 1) {
		return
	}
	go func() {
		defer g.setBusy(false)
		defer atomic.StoreInt32(&g.busy, 0)
		g.setBusy(true)
		setStatus("环境体检中…", "warn")
		setProgress(-1, "环境体检中…")
		rep := getEnvironmentReport()
		g.mw.Synchronize(func() {
			g.appendLog("─────────── 环境体检报告 ───────────")
			for _, line := range formatEnvReport(rep) {
				g.appendLog(line)
			}
			g.appendLog("────────────────────────────────────")
		})
		setProgress(0, "")
		verdict := "体检未完全通过，请看报告中的失败/警告项"
		if rep.Pass {
			verdict = "环境就绪，可以一键开启"
		}
		setStatus("体检完成："+verdict, map[bool]string{true: "ok", false: "warn"}[rep.Pass])
		g.uiMsg("环境体检", strings.Join(formatEnvReport(rep), "\n"), !rep.Pass)
	}()
}

func (g *gui) onInstall() {
	if !atomic.CompareAndSwapInt32(&g.busy, 0, 1) {
		return
	}
	g.setBusy(true)
	go func() {
		defer func() {
			atomic.StoreInt32(&g.busy, 0)
			g.setBusy(false)
			setProgress(0, "")
		}()
		if g.analysis == nil || !fileExists(g.analysis.ExePath) {
			g.uiMsg("提示", "请先选择游戏主程序（拖拽或浏览）", true)
			return
		}
		a := g.analysis
		setStatus("正在准备组件…", "warn")
		c := getAllComponents()

		if len(c.Missing) > 0 {
			g.uiMsg("组件缺失",
				"以下组件未能自动获取：\n\n"+strings.Join(c.Missing, "\n")+
					"\n\n请把对应文件手动放进工具目录的 components\\ 子文件夹后重试。\n（各文件的获取途径见《使用说明.txt》）", true)
			setStatus("组件缺失，安装中止", "error")
			return
		}
		setStatus("正在安装（路由 "+a.Route+"）…", "warn")
		m, err := installGameRoute(a, c)
		if err != nil {
			setStatus("安装失败："+err.Error(), "error")
			g.uiMsg("安装失败", err.Error(), true)
			return
		}
		if g.cbShort.Checked() {
			if _, err := newGameShortcut(a); err != nil {
				logf("WARN", "桌面启动器创建失败: %v", err)
			}
		}
		tips := "已完成！请进游戏后：\n\n1. 关闭 MSAA/SSAA\n2. 按 Home 打开 ReShade 菜单，勾选 LUMENITE: Kernel 2.0 与 DLSS 5 Feed（Kernel 在上）\n3. 在 Add-ons 页的 DLSS 5 Neural Rendering 面板打开神经渲染开关\n\n详细步骤已写入游戏目录《DLSS5_游戏内启用步骤.txt》"
		if strings.HasPrefix(m.Route, "vulkan") {
			tips += "\n\n（Vulkan 路由：若日志提示 interop 缺失，请用 run-with-feed-layer.bat 启动游戏）"
		}
		g.uiMsg("安装成功", tips, false)
		setStatus("安装完成！路由 "+m.Route+"，共写入 "+fmt.Sprint(len(m.Files))+" 项", "ok")
	}()
}

func (g *gui) onUninstall() {
	if !atomic.CompareAndSwapInt32(&g.busy, 0, 1) {
		return
	}
	g.setBusy(true)
	go func() {
		defer func() {
			atomic.StoreInt32(&g.busy, 0)
			g.setBusy(false)
		}()
		if g.analysis == nil || !fileExists(g.analysis.ExePath) {
			g.uiMsg("提示", "请先选择游戏主程序（拖拽或浏览）", true)
			return
		}
		exe := g.analysis.ExePath
		n, err := uninstallGame(exe)
		if err != nil {
			setStatus("卸载失败："+err.Error(), "error")
			g.uiMsg("卸载失败", err.Error(), true)
			return
		}
		g.teDetect.SetText("（已卸载）")
		g.analysis = nil
		g.uiMsg("卸载完成", fmt.Sprintf("已从本游戏移除 %d 个文件，配置键已还原，游戏目录零残留。", n), false)
		setStatus("卸载完成", "ok")
	}()
}

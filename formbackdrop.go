package main

import (
	"syscall"
	"unsafe"

	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// msgRepaintBackdrop 首帧兜底重绘消息：Show 后投递，等首次合成完成再整窗擦一次
const msgRepaintBackdrop = w32.WM_APP + 1

// formBackdropColor 窗体统一底色，与主题版分组框内部填充色一致，保证各处无灰白差
const formBackdropColor = 0xFFFFFF

var formBackdropBrush = winc.NewSolidColorBrush(formBackdropColor)

// groupBorderPen 分组框边框画笔，颜色取系统按钮阴影色
var groupBorderPen = winc.NewPen(w32.PS_SOLID, 1, winc.NewSolidColorBrush(winc.RGB(160, 160, 160)))

// backdropWndProc 需在进程内长期持有，避免系统回调地址被回收
var backdropWndProc = syscall.NewCallback(formBackdropWndProc)

// formPrevWndProc 保存 winc 原始窗口过程，未处理的消息仍交回它分发
var formPrevWndProc uintptr

// groupPrevProcs 保存各分组框的原始窗口过程。
// 分组框的擦背景走窗口类画刷(按钮面色)，与窗体底色不同，必须逐个接管
var groupPrevProcs = make(map[w32.HWND]uintptr)

// groupWndProc 需在进程内长期持有
var groupWndProc = syscall.NewCallback(groupBackdropWndProc)

// formDrawItemHook 自绘子控件列表项的回调。
// WM_DRAWITEM 只发给控件的父窗口，窗体子类过程收到后原样转交，
// 回调未注册或判定不属于自己时继续交给默认处理
var formDrawItemHook func(lparam uintptr) bool

// user32DLL 直接取 SetWindowLongPtrW/SetWindowLongW：
// winc 的 w32.SetWindowLongPtr 实际绑定了 SetWindowLongW，64 位下会截断窗口过程地址
var (
	user32DLL              = syscall.NewLazyDLL("user32.dll")
	procSetWindowLongPtr   = user32DLL.NewProc("SetWindowLongPtrW")
	procSetWindowLongValue = user32DLL.NewProc("SetWindowLongW")
	procMapWindowPoints    = user32DLL.NewProc("MapWindowPoints")
	procGetWindow          = user32DLL.NewProc("GetWindow")
	procGetParent          = user32DLL.NewProc("GetParent")
	procGetSysColor        = user32DLL.NewProc("GetSysColor")

	gdi32DLL            = syscall.NewLazyDLL("gdi32.dll")
	procExcludeClipRect = gdi32DLL.NewProc("ExcludeClipRect")
)

// 窗口遍历命令
const (
	gwHwndNext = 2
	gwChild    = 5
)

// setWindowProc 按当前架构选择能完整写入指针的 API
func setWindowProc(hwnd w32.HWND, newProc uintptr) uintptr {
	proc := procSetWindowLongValue
	if unsafe.Sizeof(uintptr(0)) == 8 {
		proc = procSetWindowLongPtr
	}
	index := w32.GWL_WNDPROC
	ret, _, _ := proc.Call(uintptr(hwnd), uintptr(index), newProc)
	return ret
}

// installGroupBackdrop 接管分组框的背景擦除，使其与窗体统一底色一致。
// 分组框自身不接收鼠标，只在绘制时用到这个窗口过程，接管风险很低。
func installGroupBackdrop(group *winc.GroupBox) {
	hwnd := group.Handle()
	// 分组框只作背景容器，压到同级窗口最底层，避免它的底色盖住输入框和按钮
	w32.SetWindowPos(hwnd, w32.HWND_BOTTOM, 0, 0, 0, 0, w32.SWP_NOMOVE|w32.SWP_NOSIZE|w32.SWP_NOACTIVATE)
	groupPrevProcs[hwnd] = setWindowProc(hwnd, groupWndProc)
}

func groupBackdropWndProc(hwnd w32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case w32.WM_ERASEBKGND:
		dc := w32.HDC(wparam)
		excludeSiblingsAbove(hwnd, dc)
		fillClientBackdrop(hwnd, dc)
		return 1
	case w32.WM_PAINT:
		paintGroupBox(hwnd)
		return 0
	}
	if prev := groupPrevProcs[hwnd]; prev != 0 {
		return w32.CallWindowProc(prev, hwnd, msg, wparam, lparam)
	}
	return w32.DefWindowProc(hwnd, msg, wparam, lparam)
}

// paintGroupBox 自绘分组框。
// 系统主题绘制不会为标题挖缺口，边框线会横穿标题文字，只能自己画：
// 先铺统一底色，再画边框，最后用底色擦掉标题位置的那段线并写上标题。
func paintGroupBox(hwnd w32.HWND) {
	var paint w32.PAINTSTRUCT
	dc := w32.BeginPaint(hwnd, &paint)
	if dc == 0 {
		return
	}
	defer w32.EndPaint(hwnd, &paint)

	rect := w32.GetClientRect(hwnd)
	excludeSiblingsAbove(hwnd, dc)
	fillClientBackdrop(hwnd, dc)

	hfont := w32.HGDIOBJ(w32.SendMessage(hwnd, w32.WM_GETFONT, 0, 0))
	if hfont != 0 {
		prevFont := w32.SelectObject(dc, hfont)
		defer w32.SelectObject(dc, prevFont)
	}

	title := w32.GetWindowText(hwnd)
	if title == "" {
		drawGroupBorder(dc, rect, 0)
		return
	}

	titleUTF16 := syscall.StringToUTF16(title)
	var size w32.SIZE
	if !w32.GetTextExtentPoint32(dc, &titleUTF16[0], len(titleUTF16)-1, &size) {
		drawGroupBorder(dc, rect, 0)
		return
	}

	textHeight := int(size.CY)
	drawGroupBorder(dc, rect, textHeight/2)

	titleRect := winc.NewRect(groupTitleIndent, 0, groupTitleIndent+int(size.CX)+groupTitlePadding*2, textHeight)
	canvas := winc.NewCanvasFromHDC(dc)
	canvas.FillRect(titleRect, formBackdropBrush)
	canvas.Dispose()

	w32.SetBkMode(dc, w32.TRANSPARENT)
	w32.SetTextColor(dc, w32.COLORREF(groupTitleColor))
	w32.DrawText(dc, title, len(titleUTF16)-1, titleRect.GetW32Rect(), w32.DT_LEFT|w32.DT_SINGLELINE|w32.DT_VCENTER)
}

// excludeSiblingsAbove 把所有兄弟窗口从 DC 裁剪区中排除。
// 分组框铺底色时会盖住叠在上面的输入框、按钮和文本标签，不排除就会把它们的
// 边框与已绘制的文字一并擦掉。
func excludeSiblingsAbove(hwnd w32.HWND, dc w32.HDC) {
	parent, _, _ := procGetParent.Call(uintptr(hwnd))
	if parent == 0 {
		return
	}
	child, _, _ := procGetWindow.Call(parent, gwChild)
	for child != 0 {
		sibling := w32.HWND(child)
		if sibling != hwnd && w32.IsWindowVisible(sibling) {
			if rect := w32.GetWindowRect(sibling); rect != nil {
				// GetWindowRect 是屏幕坐标，转成 hwnd 的客户区坐标
				procMapWindowPoints.Call(0, uintptr(hwnd), uintptr(unsafe.Pointer(rect)), 2)
				left, top := int(rect.Left), int(rect.Top)
				right, bottom := int(rect.Right), int(rect.Bottom)
				procExcludeClipRect.Call(uintptr(dc), uintptr(left), uintptr(top), uintptr(right), uintptr(bottom))
			}
		}
		child, _, _ = procGetWindow.Call(child, gwHwndNext)
	}
}

// drawGroupBorder 画分组框边框，top 为顶线纵坐标
func drawGroupBorder(dc w32.HDC, rect *w32.RECT, top int) {
	canvas := winc.NewCanvasFromHDC(dc)
	defer canvas.Dispose()
	canvas.DrawRect(winc.NewRect(0, top, int(rect.Right), int(rect.Bottom)), groupBorderPen)
}

const (
	// groupTitleIndent 标题距左边框的偏移
	groupTitleIndent = 8
	// groupTitlePadding 标题缺口两侧留白，避免线紧贴文字
	groupTitlePadding = 3
)

// groupTitleColor 标题文字颜色
var groupTitleColor = winc.RGB(0, 0, 0)

// installFormBackdrop 为窗体挂背景子类过程。
// winc 的 Form 从不自绘客户区（form.go 的 WM_PAINT 只更新未使用的 layoutMng），
// 灰色完全依赖窗口类背景刷在 WM_ERASEBKGND 时擦出；首帧若没走到擦除，露出的是
// DWM 的白色表面，而 STATIC 标签固定用 COLOR_BTNFACE 填自身矩形，于是显出一块块灰。
// 这里把客户区底色画死，并让标签透明，不再依赖擦除时机与主题配色。
func installFormBackdrop(form *winc.Form) {
	hwnd := form.Handle()
	formPrevWndProc = setWindowProc(hwnd, backdropWndProc)
	// 消息循环启动后补一次整窗绘制，覆盖首次合成可能留下的白底
	w32.PostMessage(hwnd, msgRepaintBackdrop, 0, 0)
}

func formBackdropWndProc(hwnd w32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case msgRepaintBackdrop:
		repaintWindow(hwnd)
		return 0

	case w32.WM_ERASEBKGND:
		fillClientBackdrop(hwnd, w32.HDC(wparam))
		return 1

	case w32.WM_PAINT:
		// 自行完成整窗绘制；winc 原有的 WM_PAINT 分支只调用未使用的 layoutMng
		var paint w32.PAINTSTRUCT
		dc := w32.BeginPaint(hwnd, &paint)
		if dc == 0 {
			return 0
		}
		fillClientBackdrop(hwnd, dc)
		w32.EndPaint(hwnd, &paint)
		return 0

	case w32.WM_CTLCOLORSTATIC:
		// 标签自己用统一底色铺满再写字。若保持透明背景，标签下方的底色就得由分组框补，
		// 而分组框重绘在标签之后，会把已经画好的文字一起擦掉，表现为文字残缺、叠影
		dc := w32.HDC(wparam)
		w32.SetBkMode(dc, w32.OPAQUE)
		w32.SetBkColor(dc, w32.COLORREF(formBackdropColor))
		return uintptr(formBackdropBrush.GetHBRUSH())

	case w32.WM_CTLCOLORBTN:
		// 分组框内部与标题缺口用同一底色填充
		return uintptr(formBackdropBrush.GetHBRUSH())

	case w32.WM_DRAWITEM:
		// owner-draw 子控件（IP 下拉框）的列表项交给注册的回调绘制
		if formDrawItemHook != nil && formDrawItemHook(lparam) {
			return 1
		}
	}

	if formPrevWndProc == 0 {
		return w32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	return w32.CallWindowProc(formPrevWndProc, hwnd, msg, wparam, lparam)
}

// fillClientBackdrop 用窗体统一底色填满客户区。
// 窗体带 WS_CLIPCHILDREN，子控件占用的区域会被系统自动扣除。
func fillClientBackdrop(hwnd w32.HWND, dc w32.HDC) {
	w32.FillRect(dc, w32.GetClientRect(hwnd), formBackdropBrush.GetHBRUSH())
}

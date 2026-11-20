package desktop

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"Canyou/internal/assets"

	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// addWindowStyle 为窗口追加一个样式位（如 WS_CLIPCHILDREN、WS_CLIPSIBLINGS）
func addWindowStyle(hwnd w32.HWND, style uint32) {
	current := uint32(w32.GetWindowLong(hwnd, w32.GWL_STYLE))
	if current&style == 0 {
		w32.SetWindowLong(hwnd, w32.GWL_STYLE, current|style)
	}
}

// repaintWindow 强制窗口立即重绘整块客户区
func repaintWindow(hwnd w32.HWND) {
	w32.InvalidateRect(hwnd, nil, true)
	w32.UpdateWindow(hwnd)
}

// setBounds 以“父窗口客户区坐标”设置控件位置与大小。
// 不用 winc 的 SetPos/SetSize：它们会把显示器工作区原点(workRect.Left/Top)错误叠加进坐标，
// 在多显示器或工作区原点非(0,0)时把控件整体平移出窗体，导致客户区空白（整片灰）。
func setBounds(ctrl winc.Controller, x, y, width, height int) {
	w32.SetWindowPos(ctrl.Handle(), 0, x, y, width, height, w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
}

// centerOnCursorMonitor 将窗口居中到“鼠标指针所在显示器”的工作区。
// 不用 winc 的 Form.Center()：它按窗口创建时的默认位置取最近显示器，
// 在多显示器下会时主屏时副屏。改为跟随鼠标，符合用户当下关注的那块屏。
func centerOnCursorMonitor(form *winc.Form) {
	hwnd := form.Handle()
	var monitor w32.HMONITOR
	if x, y, ok := w32.GetCursorPos(); ok {
		monitor = w32.MonitorFromPoint(x, y, w32.MONITOR_DEFAULTTONEAREST)
	} else {
		monitor = w32.MonitorFromWindow(hwnd, w32.MONITOR_DEFAULTTONEAREST)
	}

	var info w32.MONITORINFO
	info.CbSize = uint32(unsafe.Sizeof(info))
	if !w32.GetMonitorInfo(monitor, &info) {
		form.Center()
		return
	}

	rect := w32.GetWindowRect(hwnd)
	winWidth := int(rect.Right - rect.Left)
	winHeight := int(rect.Bottom - rect.Top)
	work := info.RcWork
	x := int(work.Left) + (int(work.Right-work.Left)-winWidth)/2
	y := int(work.Top) + (int(work.Bottom-work.Top)-winHeight)/2
	w32.SetWindowPos(hwnd, 0, x, y, 0, 0, w32.SWP_NOSIZE|w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
}

// copyToClipboard 借助系统自带 clip.exe 写入文本；URL 为纯 ASCII 无编码问题
func copyToClipboard(text string) error {
	cmd := exec.Command("clip")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	_, writeErr := io.WriteString(stdin, text)
	closeErr := stdin.Close()
	waitErr := cmd.Wait()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return waitErr
}

// applyWindowIcon 将内嵌图标设置为窗口标题栏小图标与任务栏大图标
func applyWindowIcon(form *winc.Form) {
	// ExtractIcon 只能从磁盘文件读取，故先把内嵌字节落地到临时文件
	iconPath := filepath.Join(os.TempDir(), "canyou_app.ico")
	if err := os.WriteFile(iconPath, assets.Icon(), 0o600); err != nil {
		return
	}

	icon, err := winc.ExtractIcon(iconPath, 0)
	if err != nil {
		return
	}
	form.SetIcon(0, icon) // ICON_SMALL：标题栏左上角
	form.SetIcon(1, icon) // ICON_BIG：任务栏与 Alt+Tab
}

package desktop

import (
	"image"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"Canyou/internal/netinfo"

	"github.com/tadvi/winc/w32"
)

// TestBandwidthDesktopScreenshots 仅显式开启；只捕获本测试创建的窗口，不截取用户桌面。
func TestBandwidthDesktopScreenshots(t *testing.T) {
	output := os.Getenv("CANYOU_DESKTOP_SCREENSHOTS")
	if output == "" {
		t.Skip("仅用于原生限速窗口及文档截图验收")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	ctrl := bandwidthTestController(t)
	ctrl.mainWindow.SetText("Canyou dev")
	ctrl.mainWindow.EnableSizable(false)
	ctrl.mainWindow.EnableMaxButton(false)
	addWindowStyle(ctrl.mainWindow.Handle(), w32.WS_CLIPCHILDREN)
	installFormBackdrop(ctrl.mainWindow)
	applyWindowIcon(ctrl.mainWindow)
	ctrl.dirEdit.SetText(ctrl.selectedDir)
	ctrl.uploadEdit.SetText(ctrl.uploadDir)
	if ips, err := netinfo.ScanLANIPv4(); err == nil && len(ips) > 0 {
		ctrl.applyIPAddresses(ips)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	ctrl.portEdit.SetText(strconv.Itoa(port))
	ctrl.gatePassEdit.SetText("1234")
	ctrl.onApplyGatePassword(nil)
	centerOnCursorMonitor(ctrl.mainWindow)
	ctrl.mainWindow.Show()
	pumpDesktopMessages()
	captureDesktopWindow(t, ctrl.mainWindow.Handle(), filepath.Join(output, "server-stopped.png"))
	ctrl.startService()
	if !ctrl.fileServer.IsRunning() {
		t.Fatal("隔离服务未启动")
	}
	pumpDesktopMessages()
	captureDesktopWindow(t, ctrl.mainWindow.Handle(), filepath.Join(output, "server.png"))
	dialog := ctrl.newBandwidthDialog()
	defer dialog.form.Close()
	for _, edit := range dialog.edits {
		if edit.Text() != "10" {
			t.Fatal("截图中的四项初始限速应为 10 MB/s")
		}
	}
	dialog.form.Show()
	if err := dialog.apply(); err != nil {
		t.Fatal(err)
	}
	pumpDesktopMessages()
	captureDesktopWindow(t, dialog.form.Handle(), filepath.Join(output, "bandwidth-settings.png"))
	t.Log("主窗启停、限速对话框与实时保存已验收，截图目录:", output)
}

func pumpDesktopMessages() {
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		var message w32.MSG
		for w32.PeekMessage(&message, 0, 0, 0, w32.PM_REMOVE) {
			w32.TranslateMessage(&message)
			w32.DispatchMessage(&message)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func captureDesktopWindow(t *testing.T, hwnd w32.HWND, path string) {
	t.Helper()
	bounds := w32.GetWindowRect(hwnd)
	width, height := int(bounds.Right-bounds.Left), int(bounds.Bottom-bounds.Top)
	dc := w32.GetDC(hwnd)
	if dc == 0 {
		t.Fatal("无法获取测试窗口 DC")
	}
	defer w32.ReleaseDC(hwnd, dc)
	memory := w32.CreateCompatibleDC(dc)
	if memory == 0 {
		t.Fatal("无法创建截图 DC")
	}
	defer w32.DeleteDC(memory)
	info := w32.BITMAPINFO{BmiHeader: w32.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(w32.BITMAPINFOHEADER{})), BiWidth: int32(width), BiHeight: -int32(height), BiPlanes: 1, BiBitCount: 32}}
	var pixels unsafe.Pointer
	bitmap := w32.CreateDIBSection(dc, &info, 0, &pixels, 0, 0)
	if bitmap == 0 || pixels == nil {
		t.Fatal("无法创建截图位图")
	}
	defer w32.DeleteObject(w32.HGDIOBJ(bitmap))
	previous := w32.SelectObject(memory, w32.HGDIOBJ(bitmap))
	defer w32.SelectObject(memory, previous)
	ok, _, err := syscall.NewLazyDLL("user32.dll").NewProc("PrintWindow").Call(uintptr(hwnd), uintptr(memory), 2)
	if ok == 0 {
		t.Fatal("窗口截图失败:", err)
	}
	bgra := unsafe.Slice((*byte)(pixels), width*height*4)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for offset := 0; offset < len(bgra); offset += 4 {
		img.Pix[offset], img.Pix[offset+1], img.Pix[offset+2], img.Pix[offset+3] = bgra[offset+2], bgra[offset+1], bgra[offset], 255
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, img)
	closeErr := file.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

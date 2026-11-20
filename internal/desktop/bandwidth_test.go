package desktop

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"Canyou/internal/config"
	"Canyou/internal/netinfo"

	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

func bandwidthTestController(t *testing.T) *controller {
	t.Helper()
	runtime.LockOSThread()
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	previousFont := winc.DefaultFont
	winc.DefaultFont = winc.NewFont("Microsoft YaHei UI", 9, 0)
	form := winc.NewForm(nil)
	form.SetSize(400, 454)
	ctrl := newController(form, "dev")
	ctrl.qrTempFile = filepath.Join(t.TempDir(), "qr.png")
	t.Cleanup(func() {
		ctrl.fileServer.Stop()
		form.Close()
		ctrl.boldFont.Dispose()
		winc.DefaultFont.Dispose()
		winc.DefaultFont = previousFont
		runtime.UnlockOSThread()
	})
	ctrl.buildLayout()
	ctrl.applyLoadedConfig()
	ctrl.applyIPAddresses([]netinfo.LANIP{{IP: "127.0.0.1", Adapter: "隔离测试"}})
	ctrl.selectedDir = t.TempDir()
	ctrl.uploadDir = t.TempDir()
	return ctrl
}

func TestBandwidthDialogApplyAndPersistence(t *testing.T) {
	ctrl := bandwidthTestController(t)
	if !ctrl.bandwidthButton.Enabled() {
		t.Fatal("停止状态不能打开限速设置")
	}
	button := w32.GetWindowRect(ctrl.bandwidthButton.Handle())
	open := w32.GetWindowRect(ctrl.openButton.Handle())
	status := w32.GetWindowRect(ctrl.statusLabel.Handle())
	if button.Top <= open.Bottom || button.Bottom >= status.Top {
		t.Fatal("限速按钮与既有控件重叠")
	}
	dialog := ctrl.newBandwidthDialog()
	defer dialog.form.Close()
	if labelBandwidthPerIP != "单客户端" || labelBandwidthTotal != "所有客户端" {
		t.Fatal("限速设置的客户端文案错误")
	}
	for _, edit := range dialog.edits {
		if edit.Text() != "10" {
			t.Fatal("限速设置的初始值不是 10 MB/s", edit.Text())
		}
	}
	values := []string{"1.25", "2.000001", "3.5", "0"}
	for i, text := range values {
		dialog.edits[i].SetText(text)
	}
	if err := dialog.apply(); err != nil {
		t.Fatal(err)
	}
	want := config.BandwidthLimits{PerIPUpload: 1250000, PerIPDownload: 2000001, TotalUpload: 3500000}
	if ctrl.config.Bandwidth != want || config.LoadAppConfig().Bandwidth != want {
		t.Fatal("应用或持久化失败")
	}
	ctrl.config.Password = "测试密码"
	ctrl.config.PasswordHash = config.HashAccessPassword("测试密码")
	ctrl.saveConfig()
	ctrl.onIPChanged(nil)
	if got := config.LoadAppConfig(); got.Bandwidth != want || got.PasswordHash != ctrl.config.PasswordHash {
		t.Fatal("其他设置丢失限速字段")
	}
	before := ctrl.config
	dialog.edits[0].SetText("-1")
	if err := dialog.apply(); err == nil || dialog.errorLabel.Text() == "" || ctrl.config != before {
		t.Fatal("非法输入更改配置或缺少错误提示")
	}
	dialog.edits[0].SetText("4")
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", blocked)
	if err := dialog.apply(); err == nil || ctrl.config != before {
		t.Fatal("保存失败仍更新了当前限速")
	}
}

func TestBandwidthDialogPreservesRunningTransfers(t *testing.T) {
	ctrl := bandwidthTestController(t)
	data := bytes.Repeat([]byte("z"), 64*1024)
	if err := os.WriteFile(filepath.Join(ctrl.selectedDir, "data.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	ctrl.portEdit.SetText(strconv.Itoa(port))
	cfg := ctrl.collectConfig()
	cfg.Bandwidth = config.BandwidthLimits{TotalDownload: 1}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// 模拟启动加载：未调用运行时应用接口，第一条传输也必须使用磁盘里的限额。
	ctrl.config = config.LoadAppConfig()
	ctrl.applyLoadedConfig()
	ctrl.startService()
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	connection, _, _ := connectChat(t, endpoint, "限速窗口验收")
	defer connection.Close()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	done := make(chan error, 1)
	go func() {
		response, err := client.Get(endpoint + "/data.bin")
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			err = readErr
			if err == nil && !bytes.Equal(body, data) {
				err = io.ErrUnexpectedEOF
			}
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("UI 限速未作用于服务", err)
	case <-time.After(100 * time.Millisecond):
	}
	appData := os.Getenv("APPDATA")
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", blocked)
	if err := ctrl.applyBandwidth(config.BandwidthLimits{}); err == nil {
		t.Fatal("保存失败没有返回错误")
	}
	select {
	case err := <-done:
		t.Fatal("保存失败改变了正在运行的限速", err)
	case <-time.After(100 * time.Millisecond):
	}
	t.Setenv("APPDATA", appData)
	dialog := ctrl.newBandwidthDialog()
	for _, edit := range dialog.edits {
		edit.SetText("0")
	}
	dialog.saveButton.OnClick().Fire(winc.NewEvent(dialog.saveButton, nil))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("保存后已有传输没有恢复")
	}
	if err := connection.WriteJSON(chatInput{Type: "message", Text: "原连接仍在线"}); err != nil {
		t.Fatal(err)
	}
	if event := readChatEvent(t, connection, "message"); event.Message.Text != "原连接仍在线" {
		t.Fatal("保存限速断开了聊天")
	}
}

func TestBandwidthDialogCancelAndReload(t *testing.T) {
	ctrl := bandwidthTestController(t)
	limits := config.BandwidthLimits{PerIPUpload: 1, TotalDownload: 5000000}
	if err := ctrl.applyBandwidth(limits); err != nil {
		t.Fatal(err)
	}
	for _, closeWith := range []string{"cancel", "escape", "close"} {
		dialog := ctrl.newBandwidthDialog()
		if dialog.edits[0].Text() != "0.000001" || dialog.edits[3].Text() != "5" {
			t.Fatal("重开窗口没有恢复配置")
		}
		dialog.form.Show()
		dialog.form.Hide()
		disabled := !ctrl.mainWindow.Enabled()
		dialog.edits[0].SetText("9")
		switch closeWith {
		case "cancel":
			dialog.cancelButton.OnClick().Fire(winc.NewEvent(dialog.cancelButton, nil))
		case "escape":
			w32.SendMessage(dialog.form.Handle(), w32.WM_COMMAND, w32.IDCANCEL, 0)
		case "close":
			w32.SendMessage(dialog.form.Handle(), w32.WM_CLOSE, 0, 0)
		}
		if !disabled || !ctrl.mainWindow.Enabled() {
			t.Fatal("模态窗口没有正确禁用/恢复父窗")
		}
		if ctrl.config.Bandwidth != limits || config.LoadAppConfig().Bandwidth != limits {
			t.Fatal("取消修改了限速")
		}
	}
}

//go:build windows

package integration

import (
	"bytes"
	"database/sql"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"Canyou/internal/assets"
	"Canyou/internal/config"
)

// TestStandaloneExecutable 仅显式开启：从单独 EXE 启动原生窗口，复用旧配置和旧数据库，验证真实 HTTP/WS。
func TestStandaloneExecutable(t *testing.T) {
	if os.Getenv("CANYOU_EXE_ACCEPTANCE") != "1" {
		t.Skip("仅用于已构建 EXE 的隔离验收")
	}
	source := os.Getenv("CANYOU_TEST_EXE")
	if source == "" {
		source = filepath.Join(repositoryRoot(t), "dist", "bin", "Canyou-v2.0.1.exe")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	header, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || header.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		t.Fatal("实际 EXE 必须使用 64 位 Windows GUI 子系统")
	}
	section := image.Section(".rsrc")
	if section == nil {
		t.Fatal("实际 EXE 缺少 Windows 资源")
	}
	resources, err := section.Data()
	if err != nil {
		t.Fatal(err)
	}
	icon := assets.Icon()
	for i := 0; i < int(binary.LittleEndian.Uint16(icon[4:6])); i++ {
		entry := 6 + i*16
		length := int(binary.LittleEndian.Uint32(icon[entry+8:]))
		offset := int(binary.LittleEndian.Uint32(icon[entry+12:]))
		if !bytes.Contains(resources, icon[offset:offset+length]) {
			t.Fatal("实际 EXE 的 Windows 图标与嵌入图标不同")
		}
	}
	isolated, appData, localData, shared := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	executable := filepath.Join(isolated, "Canyou.exe")
	if err := os.WriteFile(executable, data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "standalone.txt"), []byte("单文件验收"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{Directory: shared, UploadDirectory: shared, Port: strconv.Itoa(port), SelectedIP: "127.0.0.1"}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(appData, "Canyou")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(localData, "Programs", "Canyou", "chat.db")
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE messages(id INTEGER PRIMARY KEY AUTOINCREMENT,client_id TEXT NOT NULL,name TEXT NOT NULL,ip TEXT NOT NULL,sent_at TEXT NOT NULL,body TEXT NOT NULL);
CREATE TABLE settings(id INTEGER PRIMARY KEY CHECK(id=1),retention INTEGER NOT NULL CHECK(retention BETWEEN 1 AND 10000));
INSERT INTO settings VALUES(1,200);
INSERT INTO messages(id,client_id,name,ip,sent_at,body) VALUES(7,'ababababababababababababab','旧设备','127.0.0.1','2026-01-01T00:00:00Z','旧消息仍可读取');
CREATE TABLE allowed_devices(did TEXT PRIMARY KEY,authorized_at TEXT NOT NULL);
INSERT INTO allowed_devices VALUES('ababababababababababababab','2026-01-01T00:00:00Z');
PRAGMA user_version=1;`)
	closeErr := db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	command := exec.Command(executable)
	command.Dir = isolated
	command.Env = append(os.Environ(), "APPDATA="+appData, "LOCALAPPDATA="+localData)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	user32 := syscall.NewLazyDLL("user32.dll")
	post := user32.NewProc("PostMessageW")
	var window uintptr
	t.Cleanup(func() {
		if window != 0 {
			post.Call(window, 0x0010, 0, 0)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("隔离 EXE 退出失败: %v", err)
			}
		case <-time.After(8 * time.Second):
			command.Process.Kill()
			<-done
			t.Error("隔离 EXE 未正常退出，已终止本测试启动的进程")
		}
	})
	getPID := user32.NewProc("GetWindowThreadProcessId")
	getText := user32.NewProc("SendMessageTimeoutW")
	textOf := func(hwnd uintptr) string {
		var text [256]uint16
		var result uintptr
		// 跨进程控件文本必须使用 WM_GETTEXT，并限制无响应窗口的等待时间。
		getText.Call(hwnd, 0x000d, uintptr(len(text)), uintptr(unsafe.Pointer(&text[0])), 0x0002, 1000, uintptr(unsafe.Pointer(&result)))
		return syscall.UTF16ToString(text[:])
	}
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		getPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if int(pid) == command.Process.Pid && strings.HasPrefix(textOf(hwnd), "Canyou ") {
			window = hwnd
			return 0
		}
		return 1
	})
	deadline := time.Now().Add(15 * time.Second)
	for window == 0 && time.Now().Before(deadline) {
		user32.NewProc("EnumWindows").Call(callback, 0)
		time.Sleep(100 * time.Millisecond)
	}
	if window == 0 {
		t.Fatal("未找到隔离 EXE 的原生主窗口")
	}
	version := os.Getenv("CANYOU_TEST_VERSION")
	if version == "" {
		version = "v2.0.1"
	}
	if got := textOf(window); got != "Canyou "+version {
		t.Fatalf("运行时标题版本不符: %q", got)
	}
	var start uintptr
	childCallback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if textOf(hwnd) == "启动服务" {
			start = hwnd
			return 0
		}
		return 1
	})
	deadline = time.Now().Add(15 * time.Second)
	for start == 0 && time.Now().Before(deadline) {
		user32.NewProc("EnumChildWindows").Call(window, childCallback, 0)
		time.Sleep(100 * time.Millisecond)
	}
	if start == 0 {
		t.Fatal("未找到启动服务按钮")
	}
	post.Call(start, 0x00f5, 0, 0) // BM_CLICK：调用真实原生控件事件。
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	deadline = time.Now().Add(15 * time.Second)
	var page []byte
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint + "/")
		if err == nil {
			page, err = io.ReadAll(response.Body)
			response.Body.Close()
			if err == nil && response.StatusCode == 200 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, marker := range []string{"<style>", "<script>", `id="chatToggle"`, `id="uploadToggle"`, `<div class="sub">在同一网络，轻松分享 · ` + version + `</div>`} {
		if !strings.Contains(html.UnescapeString(string(page)), marker) {
			t.Fatalf("独立 EXE 缺少网页资源: %s", marker)
		}
	}
	for target, expected := range map[string][]byte{"/favicon.ico": assets.Icon(), "/standalone.txt": []byte("单文件验收")} {
		response, err := client.Get(endpoint + target)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !bytes.Equal(body, expected) {
			t.Fatalf("独立 EXE 响应错误: %s, %v", target, err)
		}
	}
	connection, _, history := connectChat(t, endpoint, "独立验收")
	if len(history) != 1 || history[0].ID != 7 || history[0].Text != "旧消息仍可读取" {
		t.Fatalf("旧消息未兼容: %+v", history)
	}
	if err := connection.WriteJSON(chatInput{Type: "message", Text: "新消息可保存"}); err != nil {
		t.Fatal(err)
	}
	if event := readChatEvent(t, connection, "message"); event.Message == nil || event.Message.Text != "新消息可保存" {
		t.Fatalf("独立 EXE 消息读写失败: %+v", event)
	}
	connection.Close()
	db, err = sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var schema, devices, messages int
	if err := db.QueryRow("PRAGMA user_version").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM allowed_devices WHERE did=? AND theme='system'", gateTestDid).Scan(&devices); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if schema != 1 || devices != 1 || messages != 2 {
		t.Fatalf("共用数据库或旧表迁移异常: version=%d devices=%d messages=%d", schema, devices, messages)
	}
	entries, err := os.ReadDir(isolated)
	if err != nil || len(entries) != 1 || entries[0].Name() != "Canyou.exe" {
		t.Fatal("隔离运行目录不再是单个 EXE")
	}
	t.Log("单个 EXE 的标题版本、原生启动、嵌入网页/图标、文件、WebSocket、旧配置及旧 SQLite 共库迁移通过")
}

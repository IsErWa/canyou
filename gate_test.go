package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// gateTestDid 模拟前端指纹落盘后的合法设备标识（26 位小写 hex）
const gateTestDid = "ababababababababababababab"

// newGateTestServer 拉起带独立门禁与聊天服务的完整共享监听器，门禁名单所在的 chat.db 固定在 LOCALAPPDATA 临时目录
func newGateTestServer(t *testing.T) (*accessGate, *httptest.Server) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("共享文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newChatManager(filepath.Join(t.TempDir(), "chat.db"))
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Disable)
	gate := newAccessGate()
	server := httptest.NewServer(gate.intercept(newShareHandler(root, uploads, manager)))
	t.Cleanup(server.Close)
	return gate, server
}

// gateGet 以指定身份携带方式发起 GET；header 非空时走 X-Device-ID，否则走 ?did= 查询参数
func gateGet(t *testing.T, server *httptest.Server, path, accept, header, did string) (int, string) {
	t.Helper()
	target := server.URL + path
	if header == "" && did != "" {
		target += "?did=" + did
	}
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	if header != "" {
		request.Header.Set(header, did)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func postGateJSON(t *testing.T, server *httptest.Server, path string, payload map[string]string) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, result
}

func TestGateResponsiveTemplate(t *testing.T) {
	var body bytes.Buffer
	if err := gateTemplate.ExecuteTemplate(&body, "gatePage", struct{ Next string }{Next: jsonString("/")}); err != nil {
		t.Fatal(err)
	}
	html := body.String()
	assertUniquePageIDs(t, html)
	for _, marker := range []string{
		`<main class="box">`, `for="gatePass"`, `role="alert"`, `id="i-lock"`, `100dvh`, `id="themeColor"`, `window.canyouTheme=`, `window.canyouTheme.accept(data.theme)`,
		`id="gateNote" role="status" aria-live="polite" aria-atomic="true"`, `class="gate-spinner" aria-hidden="true"`,
		`@keyframes gate-spin`, `animation:gate-spin .8s linear infinite`,
		`@media(prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}`, `note.hidden=true; form.hidden=false;`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("门禁页缺少标记: %s", marker)
		}
	}
	assertInlineScripts(t, html)
}

func TestGateOpenWithoutPassword(t *testing.T) {
	_, server := newGateTestServer(t)
	if code, body := gateGet(t, server, "/sample.txt", "", "", ""); code != 200 || body != "共享文件" {
		t.Fatalf("未设密码应全部直通: 状态=%d 正文=%q", code, body)
	}
	if code, body := gateGet(t, server, "/", "text/html", "", ""); code != 200 || !strings.Contains(body, `id="chatToggle"`) {
		t.Fatalf("未设密码导航应直达列表页: 状态=%d", code)
	}
}

func TestGatePasswordUnlockAndPersistence(t *testing.T) {
	gate, server := newGateTestServer(t)
	gate.SetPasswordHash(hashAccessPassword("1234"))

	// 未授权整页导航 → 密码页；fetch/WebSocket 类请求 → 403
	if code, body := gateGet(t, server, "/", "text/html", "", ""); code != 200 || !strings.Contains(body, `id="gateForm"`) || strings.Contains(body, "共享文件") {
		t.Fatalf("未授权导航应返回密码页: 状态=%d 正文=%q", code, body)
	}
	if code, _ := gateGet(t, server, "/chat/status", "", "", ""); code != 403 {
		t.Fatalf("未授权接口请求应 403: %d", code)
	}

	// 密码错误拒绝且不登记
	if code, result := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "password": "错的"}); code != 403 || result["ok"] != false {
		t.Fatalf("错误密码应被拒绝: %d %v", code, result)
	}
	if code, result := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid}); result["allowed"] != false {
		t.Fatalf("未授权设备 check 应为 false: %d %v", code, result)
	}
	if _, found := gateStoredName(t, gateTestDid); found {
		t.Fatal("已设密码时未授权设备的 check 不得登记入库")
	}
	// 非法 did 即使密码正确也拒绝登记
	if code, _ := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": "../../etc", "password": "1234"}); code != 403 {
		t.Fatalf("非法设备标识应被拒绝: %d", code)
	}

	// 密码正确 → 登记设备名并持久化，同时回传 {id, did, name}
	code, result := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "password": "1234", "name": "小明的手机"})
	if code != 200 || result["ok"] != true {
		t.Fatalf("验证密码应授权成功: %d %v", code, result)
	}
	if result["did"] != gateTestDid || result["name"] != "小明的手机-1" {
		t.Fatalf("解锁应回传设备 did 与 name: %+v", result)
	}
	if idVal, isNum := result["id"].(float64); !isNum || idVal <= 0 {
		t.Fatalf("解锁应回传正整数主键 ID: %+v", result)
	}
	if got := gateNameStoredInDB(t, gateTestDid); got != "小明的手机-1" { // httptest 的 RemoteAddr 为 127.0.0.1，尾号 1
		t.Fatalf("解锁应落库设备名与 IP 尾号: %q", got)
	}
	if code, _ := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "password": "1234", "name": "客厅电视"}); code != 200 {
		t.Fatalf("重复解锁应成功: %d", code)
	}
	if got := gateNameStoredInDB(t, gateTestDid); got != "小明的手机-1" {
		t.Fatalf("已登记设备应保留首次名称，不得刷新: %q", got)
	}
	code, result = postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid})
	if result["allowed"] != true || result["did"] != gateTestDid || result["name"] != "小明的手机-1" {
		t.Fatalf("已授权设备 check 应回传设备信息: %d %v", code, result)
	}
	if reloaded := newAccessGate(); !reloaded.approved(gateTestDid) {
		t.Fatal("允许名单应持久化在 SQLite 并在重载后仍生效")
	}

	// header 与 URL 参数两种携带方式都放行；非法 did 仍被拦
	if code, body := gateGet(t, server, "/sample.txt", "", "X-Device-ID", gateTestDid); code != 200 || body != "共享文件" {
		t.Fatalf("header 携带身份应放行: %d %q", code, body)
	}
	if code, body := gateGet(t, server, "/sample.txt", "", "", gateTestDid); code != 200 || body != "共享文件" {
		t.Fatalf("URL 参数携带身份应放行: %d %q", code, body)
	}
	if code, _ := gateGet(t, server, "/sample.txt", "", "", "deadbeef"); code != 403 {
		t.Fatalf("未授权 did 应被拦截: %d", code)
	}
	// 回归：旧链接残留的失效 did 卡在参数首位时，应取末位当前有效身份放行，不得死循环拦截
	if code, _ := gateGet(t, server, "/sample.txt?did=ffffffffffffffffffffffff&did="+gateTestDid, "", "", ""); code != 200 {
		t.Fatalf("URL 多个 did 应取最后一个有效值: %d", code)
	}

	// 清空密码恢复全开放，名单保留待再次启用
	gate.SetPasswordHash("")
	if code, _ := gateGet(t, server, "/sample.txt", "", "", ""); code != 200 {
		t.Fatalf("取消密码后应全部放行: %d", code)
	}
}

// gateNameStoredInDB 查询允许名单里登记的设备名
func gateNameStoredInDB(t *testing.T, did string) string {
	t.Helper()
	var name string
	if err := gateWithDB(func(db *sql.DB) error {
		return db.QueryRow(`SELECT name FROM allowed_devices WHERE did=?`, did).Scan(&name)
	}); err != nil {
		t.Fatalf("查询设备名失败: %v", err)
	}
	return name
}

// gateStoredName 查询设备是否已登记及其名称，行不存在时 found=false
func gateStoredName(t *testing.T, did string) (string, bool) {
	t.Helper()
	var name string
	err := gateWithDB(func(db *sql.DB) error {
		return db.QueryRow(`SELECT name FROM allowed_devices WHERE did=?`, did).Scan(&name)
	})
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		t.Fatalf("查询设备登记失败: %v", err)
	}
	return name, true
}

// TestGateCheckRegistersWithoutPassword 未设密码时任何设备都经 /gate/check 自动登记，并获得带 IP 尾号的权威名
func TestGateCheckRegistersWithoutPassword(t *testing.T) {
	_, server := newGateTestServer(t)
	code, result := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid, "name": "会议室平板"})
	if code != 200 || result["allowed"] != true || result["name"] != "会议室平板-1" || result["did"] != gateTestDid {
		t.Fatalf("未设密码 check 应自动登记并回传设备名: %d %v", code, result)
	}
	firstID, isNum := result["id"].(float64)
	if !isNum || firstID <= 0 {
		t.Fatalf("check 应回传主键 ID: %v", result)
	}
	if _, found := gateStoredName(t, gateTestDid); !found {
		t.Fatal("未设密码 check 应把设备写入 allowed_devices")
	}
	// 二次 check 命中同一记录：ID 不变、名称不被新上报值刷新
	_, again := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid, "name": "换个名"})
	if again["id"] != firstID || again["name"] != "会议室平板-1" {
		t.Fatalf("重复 check 应返回同一登记信息: %+v", again)
	}
}

// TestGateRenameSyncsAllowedDevices 连接不传 name 时后台应下发 allowed_devices 登记名；聊天改名回写该表，但历史消息里的旧名不回刷
func TestGateRenameSyncsAllowedDevices(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dbPath, err := chatDatabasePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := gateWithDB(func(db *sql.DB) error { return nil }); err != nil { // 预建 allowed_devices，与生产启动顺序一致
		t.Fatal(err)
	}
	manager := newChatManager(dbPath) // 与门禁同库
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	defer manager.Disable()
	gate := newAccessGate()
	gate.SetPasswordHash(hashAccessPassword("1234"))
	server := httptest.NewServer(gate.intercept(newShareHandler(t.TempDir(), t.TempDir(), manager)))
	defer server.Close()

	if code, _ := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "password": "1234", "name": "手机"}); code != 200 {
		t.Fatalf("解锁登记失败: %d", code)
	}
	header := http.Header{"Origin": []string{server.URL}}
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/chat?id="+gateTestDid, header)
	if err != nil {
		t.Fatalf("已授权设备应能连接: %v %v", err, resp)
	}
	defer conn.Close()

	drain := func(want string) chatEvent {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(8 * time.Second))
		for {
			var event chatEvent
			if err := conn.ReadJSON(&event); err != nil {
				t.Fatal(err)
			}
			if event.Type == want {
				return event
			}
		}
	}
	if event := drain("welcome"); event.Name != "手机-1" {
		t.Fatalf("连接不传 name 时后台应下发门禁登记名: %+v", event)
	}
	if err := conn.WriteJSON(map[string]any{"type": "message", "text": "hello", "requestId": "m1"}); err != nil {
		t.Fatal(err)
	}
	drain("message")
	if err := conn.WriteJSON(map[string]any{"type": "rename", "name": "新名字", "requestId": "r1"}); err != nil {
		t.Fatal(err)
	}
	if event := drain("renamed"); event.ClientID != gateTestDid || event.Name != "新名字" {
		t.Fatalf("改名事件错误: %+v", event)
	}
	if got := gateNameStoredInDB(t, gateTestDid); got != "新名字" {
		t.Fatalf("改名应回写 allowed_devices: %q", got)
	}
	var msgName string
	if err := gateWithDB(func(db *sql.DB) error {
		return db.QueryRow(`SELECT name FROM messages WHERE body='hello'`).Scan(&msgName)
	}); err != nil {
		t.Fatal(err)
	}
	if msgName != "手机-1" {
		t.Fatalf("历史消息设备名不应被改写: %q", msgName)
	}
}

func TestGateThemePersistence(t *testing.T) {
	_, server := newGateTestServer(t)
	_, first := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid, "name": "主题设备"})
	if first["theme"] != "system" {
		t.Fatalf("新设备应默认跟随系统: %v", first)
	}
	type metadata struct {
		id               int64
		name, authorized string
		color            int
	}
	readMetadata := func() metadata {
		t.Helper()
		var value metadata
		if err := gateWithDB(func(db *sql.DB) error {
			return db.QueryRow(`SELECT id,name,authorized_at,color FROM allowed_devices WHERE did=?`, gateTestDid).Scan(&value.id, &value.name, &value.authorized, &value.color)
		}); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := readMetadata()
	for _, theme := range []string{"dark", "light", "system", "dark"} {
		code, result := postGateJSON(t, server, "/gate/theme", map[string]string{"did": gateTestDid, "theme": theme})
		if code != 200 || result["ok"] != true || result["theme"] != theme {
			t.Fatalf("主题保存失败: %d %v", code, result)
		}
		_, result = postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid})
		if result["theme"] != theme {
			t.Fatalf("主题未落库: %v", result)
		}
		if readMetadata() != before {
			t.Fatal("保存主题修改了其他设备字段")
		}
	}
	other := "cdcdcdcdcdcdcdcdcdcdcdcdcd"
	_, result := postGateJSON(t, server, "/gate/check", map[string]string{"did": other})
	if result["theme"] != "system" {
		t.Fatal("主题污染其他设备")
	}
	// 未设密码时，直接保存也沿用设备自动登记规则。
	if code, result := postGateJSON(t, server, "/gate/theme", map[string]string{"did": "eeeeeeeeeeeeeeee", "theme": "light"}); code != 200 || result["ok"] != true {
		t.Fatalf("开放服务登记失败: %d %v", code, result)
	}
	if err := gateWithDB(func(db *sql.DB) error {
		store := &chatStore{db: db}
		if err := store.updateDeviceName(gateTestDid, "改名后"); err != nil {
			return err
		}
		return store.updateDeviceColor(gateTestDid, "改名后", 3)
	}); err != nil {
		t.Fatal(err)
	}
	reloaded := newAccessGate()
	reloaded.SetPasswordHash(hashAccessPassword("1234"))
	restarted := httptest.NewServer(reloaded.intercept(http.NotFoundHandler()))
	defer restarted.Close()
	for _, path := range []string{"/gate/check", "/gate/unlock"} {
		code, result := postGateJSON(t, restarted, path, map[string]string{"did": gateTestDid, "password": "1234"})
		if code != 200 || result["theme"] != "dark" || result["name"] != "改名后" || result["id"] != first["id"] {
			t.Fatalf("重启或改名改色丢失主题: %d %v", code, result)
		}
	}
}

func TestGateThemeValidationAndWriteFailure(t *testing.T) {
	gate, server := newGateTestServer(t)
	gate.SetPasswordHash(hashAccessPassword("1234"))
	for _, payload := range []map[string]string{
		{"did": "../bad", "theme": "dark"}, {"did": gateTestDid, "theme": "night"},
		{"did": gateTestDid}, {"theme": "light"}, {"did": gateTestDid, "theme": "Dark"},
	} {
		if code, result := postGateJSON(t, server, "/gate/theme", payload); code != 400 || result["ok"] != false {
			t.Fatalf("非法主题请求未拒绝: %d %v", code, result)
		}
	}
	if code, result := postGateJSON(t, server, "/gate/theme", map[string]string{"did": gateTestDid, "theme": "dark"}); code != 403 || result["ok"] != false {
		t.Fatalf("未授权保存未拒绝: %d %v", code, result)
	}
	if _, found := gateStoredName(t, gateTestDid); found || gate.approved(gateTestDid) {
		t.Fatal("主题保存绕过了门禁登记")
	}
	_, denied := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid})
	if _, exists := denied["theme"]; exists {
		t.Fatal("未授权响应泄露主题")
	}
	if code, _ := gateGet(t, server, "/gate/theme", "", "", ""); code != 405 {
		t.Fatalf("错误方法应拒绝: %d", code)
	}
	recorder := httptest.NewRecorder()
	gate.serveAPI(recorder, httptest.NewRequest(http.MethodPost, "/gate/theme", strings.NewReader("{")))
	if recorder.Code != 400 {
		t.Fatalf("错误 JSON 应拒绝: %d", recorder.Code)
	}
	_, unlocked := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "password": "1234"})
	if unlocked["theme"] != "system" {
		t.Fatalf("解锁未返回默认主题: %v", unlocked)
	}
	if err := gateWithDB(func(db *sql.DB) error {
		_, err := db.Exec(`CREATE TRIGGER reject_theme BEFORE UPDATE OF theme ON allowed_devices BEGIN SELECT RAISE(ABORT,'模拟主题写入失败'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if code, result := postGateJSON(t, server, "/gate/theme", map[string]string{"did": gateTestDid, "theme": "dark"}); code != 500 || result["ok"] != false {
		t.Fatalf("写入失败伪报成功: %d %v", code, result)
	}
	_, result := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid})
	if result["theme"] != "system" {
		t.Fatal("失败写入改变了主题")
	}
	if err := gateWithDB(func(db *sql.DB) error {
		_, err := db.Exec(`DELETE FROM allowed_devices WHERE did=?`, gateTestDid)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if code, _ := postGateJSON(t, server, "/gate/theme", map[string]string{"did": gateTestDid, "theme": "dark"}); code != 500 {
		t.Fatalf("更新不存在的设备伪报成功: %d", code)
	}
}

func TestGateThemeColumnMigration(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	filename, err := chatDatabasePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE allowed_devices(id INTEGER PRIMARY KEY AUTOINCREMENT,did TEXT NOT NULL UNIQUE,name TEXT NOT NULL DEFAULT '',authorized_at TEXT NOT NULL,color INTEGER NOT NULL DEFAULT -1);
INSERT INTO allowed_devices(id,did,name,authorized_at,color) VALUES(7,?,'旧设备','旧授权时间',2); PRAGMA user_version=7;`, gateTestDid); err != nil {
		t.Fatal(err)
	}
	gate := newAccessGate()
	id, name, theme, found := gate.deviceInfo(gateTestDid)
	if !found || id != 7 || name != "旧设备" || theme != "system" {
		t.Fatalf("旧设备主题迁移失败: %d %s %s %v", id, name, theme, found)
	}
	var version, color int
	var authorized string
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT color,authorized_at FROM allowed_devices WHERE did=?`, gateTestDid).Scan(&color, &authorized); err != nil {
		t.Fatal(err)
	}
	if version != 7 || color != 2 || authorized != "旧授权时间" {
		t.Fatal("主题迁移破坏旧字段或聊天版本")
	}
	// 模拟已有数据库中出现异常值，读取时仍安全回退。
	if _, err := db.Exec(`PRAGMA ignore_check_constraints=ON; UPDATE allowed_devices SET theme='unknown' WHERE did=?`, gateTestDid); err != nil {
		t.Fatal(err)
	}
	_, _, theme, found = gate.deviceInfo(gateTestDid)
	if !found || theme != "system" {
		t.Fatal("异常主题没有回退到跟随系统")
	}
}

// TestWithIPTail IP 尾号拼接：IPv4 取末段 1–3 位，非 IPv4 或非法尾段保持原名
func TestWithIPTail(t *testing.T) {
	cases := []struct{ name, remote, want string }{
		{"Windows 电脑", "10.173.25.70:52134", "Windows 电脑-70"},
		{"Android 设备", "192.168.1.108:1", "Android 设备-108"},
		{"", "192.168.1.1:1", "设备-1"},
		{"Mac", "[::1]:1234", "Mac"},
		{"Mac", "hostname.local:80", "Mac"},
		{"Mac", "1.2.3.4567:80", "Mac"},
	}
	for _, c := range cases {
		if got := withIPTail(c.name, c.remote); got != c.want {
			t.Fatalf("withIPTail(%q, %q) = %q，应为 %q", c.name, c.remote, got, c.want)
		}
	}
}

// TestGateAllowedDevicesLegacyMigration 旧版表结构（did 主键、无 id/name 列）应自动迁移且存量名单不丢
func TestGateAllowedDevicesLegacyMigration(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	filename, err := chatDatabasePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE allowed_devices (
 did TEXT PRIMARY KEY, authorized_at TEXT NOT NULL
); INSERT INTO allowed_devices(did, authorized_at) VALUES(?, '2026-01-01T00:00:00Z')`, gateTestDid); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	gate := newAccessGate() // 首次访问库时触发迁移
	if _, _, theme, found := gate.deviceInfo(gateTestDid); !found || theme != "system" {
		t.Fatal("最旧版本设备未迁移为跟随系统")
	}
	if !gate.approved(gateTestDid) {
		t.Fatal("旧表存量名单迁移后应仍生效")
	}
	gate.authorize("cdcdcdcdcdcdcdcdcdcdcdcdcd", "客厅电视", "10.0.0.99:5")
	if got := gateNameStoredInDB(t, "cdcdcdcdcdcdcdcdcdcdcdcdcd"); got != "客厅电视-99" {
		t.Fatalf("新登记设备应拼接 IP 尾号: %q", got)
	}
	if got := gateNameStoredInDB(t, gateTestDid); got != "" {
		t.Fatalf("迁移行设备名应为空: %q", got)
	}
}

func TestGateWebSocketHandshake(t *testing.T) {
	gate, server := newGateTestServer(t)
	gate.SetPasswordHash(hashAccessPassword("会话密码"))
	dial := func(query string) (*websocket.Conn, *http.Response) {
		t.Helper()
		header := http.Header{"Origin": []string{server.URL}}
		conn, resp, _ := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/chat?"+query, header)
		return conn, resp
	}
	conn, resp := dial("name=测试&id=" + gateTestDid)
	if conn != nil {
		conn.Close()
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("未授权设备 WebSocket 握手应 403: %v", resp)
	}
	resp.Body.Close()

	if code, result := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "password": "会话密码"}); code != 200 || result["ok"] != true {
		t.Fatalf("授权失败: %d %v", code, result)
	}
	conn, resp = dial("name=测试&id=" + gateTestDid)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if conn == nil {
		t.Fatalf("已授权设备握手应成功: %v", resp)
	}
	t.Cleanup(func() { conn.Close() })
	if event := readChatEvent(t, conn, "welcome"); event.ClientID != gateTestDid {
		t.Fatalf("指纹应作为连接标识: %+v", event)
	}
}

func TestSafeInternalTarget(t *testing.T) {
	good := "/a/b?q=%E4%B8%AD"
	if got := safeInternalTarget(good); got != good {
		t.Fatalf("内部路径被误改: %s", got)
	}
	for _, bad := range []string{"", "//evil.com", "http://evil.com", `/a\<b>`, "/a b", "/a\nb"} {
		if got := safeInternalTarget(bad); got != "/" {
			t.Fatalf("危险目标应回落首页: %q -> %q", bad, got)
		}
	}
}

// TestGateBrowserPreview 仅显式开启时提供隔离的门禁浏览器验收服务，临时 chat.db 目录、密码固定 1234。
func TestGateBrowserPreview(t *testing.T) {
	if os.Getenv("CANYOU_GATE_PREVIEW") != "1" {
		t.Skip("仅用于手动浏览器验收")
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "示例文件.txt"), []byte("门禁验收样例"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "子目录"), 0o755); err != nil { // 供验收单页内目录导航
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "子目录", "内层文件.txt"), []byte("子目录验收样例"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 多条目、长文件名及深路径只写入隔离目录，用于响应式布局验收。
	for i := 1; i <= 30; i++ {
		name := fmt.Sprintf("测试文档-%02d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("布局验收文档"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, strings.Repeat("很长的文件名", 12)+".txt"), []byte("长名称验收"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "子目录", "项目资料", "交付文件", "最终版本"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 两张生成图供验收图片预览与左右翻页
	for _, spec := range []struct {
		name       string
		red, green uint8
	}{
		{"预览图A.png", 79, 110},
		{"预览图B.png", 220, 90},
	} {
		file, err := os.Create(filepath.Join(root, spec.name))
		if err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 160, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 160; x++ {
				img.Set(x, y, color.RGBA{R: spec.red, G: spec.green, B: uint8(x), A: 255})
			}
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
	}
	// 预览验收样例：可播放 WAV / 最小合法 PDF / 文本 / HTML / 占位 mp4 与 zip
	writePreviewWav(t, filepath.Join(root, "预览音A.wav"))
	for name, content := range map[string]string{
		"预览文本.txt": "文本预览验收样例：浏览器应按 text/plain 在新标签页直接显示。",
		"预览页.html": "<h1>HTML 预览验收</h1>\n<p>Canyou 新标签页渲染测试。</p>\n",
		"预览视频.mp4": "非真实 mp4，仅验证点击后浮层出现 video 元素的结构",
		"压缩样本.zip": "PK\x03\x04 非完整压缩包，应保持点击下载",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "预览页.pdf"), minimalPDFBytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newChatManager("") // 与生产一致：聊天库回落到 chatDatabasePath，与门禁 allowed_devices 同库，改名才能回写
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	defer manager.Disable()
	gate := newAccessGate()
	gate.SetPasswordHash(hashAccessPassword("1234"))
	share := gate.intercept(newShareHandler(root, uploads, manager))
	done := make(chan struct{})
	mux := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" && request.URL.Query().Get("__test_files_only") == "1" && request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := listingTemplate.Execute(writer, listingData{ClientIP: "127.0.0.1"}); err != nil {
				t.Error(err)
			}
			return
		}
		// iframe 的视口由元素尺寸决定，供无窗口缩放能力的浏览器工具验收断点。
		if request.URL.Path == "/__test/layout" && request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(writer, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>Canyou 响应式验收</title></head><body style="margin:0;background:#e2e7ef"><iframe id="previewFrame" title="响应式工作台预览" src="/" style="display:block;border:0;width:1440px;height:900px;color-scheme:light"></iframe></body></html>`)
			return
		}
		if request.URL.Path == "/__test/stop" && request.Method == http.MethodPost {
			close(done)
			return
		}
		share.ServeHTTP(writer, request)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:18082")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	defer server.Close()
	go server.Serve(listener)
	t.Log("门禁验收地址：http://127.0.0.1:18082（密码 1234，仅回环地址）")
	select {
	case <-done:
	case <-time.After(20 * time.Minute):
	}
}

// writePreviewWav 生成 1 秒 440Hz 正弦波的 16bit 单声道 PCM WAV，供验收浮层内音频真播放
func writePreviewWav(t *testing.T, target string) {
	t.Helper()
	const rate = 8000
	data := make([]byte, rate*2)
	for i := 0; i < rate; i++ {
		v := int16(12000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	buf := &bytes.Buffer{}
	u32 := func(v uint32) { binary.Write(buf, binary.LittleEndian, v) }
	u16 := func(v uint16) { binary.Write(buf, binary.LittleEndian, v) }
	buf.WriteString("RIFF")
	u32(uint32(36 + len(data)))
	buf.WriteString("WAVEfmt ")
	u32(16)
	u16(1)
	u16(1)
	u32(rate)
	u32(rate * 2)
	u16(2)
	u16(16)
	buf.WriteString("data")
	u32(uint32(len(data)))
	buf.Write(data)
	if err := os.WriteFile(target, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// minimalPDFBytes 拼装带正确 xref 偏移的单页文本 PDF（纯 ASCII，避免 xref 长度问题），供验收 Chrome 内置查看器
func minimalPDFBytes() []byte {
	stream := "BT /F1 18 Tf 20 60Td (Canyou PDF) Tj ET"
	objects := []string{
		"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj",
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj",
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 240 120] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >> endobj",
		fmt.Sprintf("4 0 obj << /Length %d >> stream\n%s\nendstream endobj", len(stream), stream),
		"5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj",
	}
	buf := &bytes.Buffer{}
	offsets := make([]int, len(objects)+1)
	buf.WriteString("%PDF-1.4\n")
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		buf.WriteString(obj)
		buf.WriteString("\n")
	}
	xref := buf.Len()
	fmt.Fprintf(buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(buf, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"Canyou/internal/chat"
	"Canyou/internal/config"
	"Canyou/internal/gate"
	"Canyou/internal/server"
	"Canyou/internal/storage"

	"github.com/gorilla/websocket"
)

type chatInput struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Name      string `json:"name"`
	Color     int    `json:"color"`
	Limit     int    `json:"limit"`
	RequestID string `json:"requestId"`
}

type chatDevice struct {
	ClientID string `json:"clientId"`
	No       int64  `json:"no"`
	Color    int    `json:"color"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
}

type chatEvent struct {
	Type      string            `json:"type"`
	Action    string            `json:"action,omitempty"`
	Time      string            `json:"time,omitempty"`
	Devices   []chatDevice      `json:"devices,omitempty"`
	ClientID  string            `json:"clientId,omitempty"`
	No        int64             `json:"no,omitempty"`
	Color     int               `json:"color"`
	Name      string            `json:"name,omitempty"`
	IP        string            `json:"ip,omitempty"`
	Message   *storage.Message  `json:"message,omitempty"`
	Messages  []storage.Message `json:"messages,omitempty"`
	Limit     int               `json:"limit,omitempty"`
	OldestID  int64             `json:"oldestId,omitempty"`
	Error     string            `json:"error,omitempty"`
	RequestID string            `json:"requestId,omitempty"`
}

func readChatEvent(t *testing.T, c *websocket.Conn, kind string) chatEvent {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(8 * time.Second))
	var event chatEvent
	if err := c.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if event.Type != kind {
		t.Fatalf("预期 %s，收到 %+v", kind, event)
	}
	return event
}

func readChatPresence(t *testing.T, c *websocket.Conn, action, id string, count int) chatEvent {
	t.Helper()
	event := readChatEvent(t, c, "presence")
	if event.Action != action || event.ClientID != id || len(event.Devices) != count || event.Name == "" || event.IP != "127.0.0.1" {
		t.Fatalf("在线状态错误: %+v", event)
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
		t.Fatal("系统提示缺少有效时间")
	}
	seen := make(map[string]bool)
	for _, device := range event.Devices {
		if device.ClientID == "" || seen[device.ClientID] || device.Name == "" || device.IP != "127.0.0.1" {
			t.Fatalf("在线列表身份错误: %+v", event.Devices)
		}
		seen[device.ClientID] = true
	}
	if seen[id] != (action == "joined") {
		t.Fatalf("加入或离开未同步到在线列表: %+v", event)
	}
	return event
}

func connectChat(t *testing.T, endpoint, name string) (*websocket.Conn, chatEvent, []storage.Message) {
	t.Helper()
	header := http.Header{"Origin": []string{endpoint}, "X-Forwarded-For": []string{"8.8.8.8"}}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(endpoint, "http")+"/ws/chat?name="+url.QueryEscape(name), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	welcome := readChatEvent(t, c, "welcome")
	var history []storage.Message
	for {
		var event chatEvent
		if err := c.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "ready" {
			break
		}
		if event.Type != "history" {
			t.Fatalf("历史加载乱序: %+v", event)
		}
		history = append(history, event.Messages...)
	}
	joined := readChatPresence(t, c, "joined", welcome.ClientID, len(welcome.Devices))
	if !reflect.DeepEqual(joined.Devices, welcome.Devices) {
		t.Fatal("初始在线快照与加入事件不一致")
	}
	return c, welcome, history
}

const gateTestDid = "ababababababababababababab"

// newGateTestServer 拉起带独立门禁与聊天服务的完整共享监听器，门禁名单所在的 chat.db 固定在 LOCALAPPDATA 临时目录
func newGateTestServer(t *testing.T) (*gate.Gate, *httptest.Server) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("共享文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := chat.NewManager(filepath.Join(t.TempDir(), "chat.db"))
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Disable)
	gate := gate.New("dev")
	server := httptest.NewServer(gate.Wrap(server.NewHandler(root, uploads, manager, "dev")))
	t.Cleanup(server.Close)
	return gate, server
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

// gateNameStoredInDB 查询允许名单里登记的设备名
// 测试以独立 SQLite 连接检查状态和注入故障，不暴露存储包的数据库句柄。
func gateWithDB(fn func(*sql.DB) error) error {
	if _, err := storage.LoadAllowedDeviceIDs(); err != nil {
		return err
	}
	filename, err := config.DatabasePath()
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return fn(db)
}

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

// TestGateRenameSyncsAllowedDevices 连接不传 name 时后台应下发 allowed_devices 登记名；聊天改名回写该表，但历史消息里的旧名不回刷
func TestGateRenameSyncsAllowedDevices(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dbPath, err := config.DatabasePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := gateWithDB(func(db *sql.DB) error { return nil }); err != nil { // 预建 allowed_devices，与生产启动顺序一致
		t.Fatal(err)
	}
	manager := chat.NewManager(dbPath) // 与门禁同库
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	defer manager.Disable()
	gate := gate.New("dev")
	gate.SetPasswordHash(config.HashAccessPassword("1234"))
	server := httptest.NewServer(gate.Wrap(server.NewHandler(t.TempDir(), t.TempDir(), manager, "dev")))
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

func TestGateWebSocketHandshake(t *testing.T) {
	gate, server := newGateTestServer(t)
	gate.SetPasswordHash(config.HashAccessPassword("会话密码"))
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

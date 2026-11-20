package server

import (
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"Canyou/internal/config"
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
func TestChatFileServerLifecycleAndRendering(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	server := NewFileServer("dev")
	if server.ChatEnabled() {
		t.Fatal("服务未启动不应开启消息")
	}
	root := t.TempDir()
	if err := server.Start(root, "", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	if address, ok := server.listener.Addr().(*net.TCPAddr); !ok || !address.IP.IsUnspecified() {
		t.Fatalf("共享服务应监听所有本机地址: %v", server.listener.Addr())
	}
	_, port, err := net.SplitHostPort(server.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://127.0.0.1:" + port
	assertButton := func(want bool) {
		t.Helper()
		response, err := http.Get(endpoint + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 || strings.Contains(string(body), `id="chatToggle"`) != want || strings.Contains(string(body), "ZgotmplZ") {
			t.Fatalf("聊天入口或模板渲染错误: 状态=%d 有按钮=%v", response.StatusCode, strings.Contains(string(body), `id="chatToggle"`))
		}
		if want {
			for _, marker := range []string{`id="settingsPanel" hidden`, `id="devicesPanel" hidden`, `id="chatPageChat"`, `id="chatManualConnect"`, `id="chatSaveColor"`, `id="chatSwatches"`, `id="chatSettingsToggle"`, `id="chatDevicesToggle"`, `id="chatLimit"`, `window.canyouUI=function`, `currentView:currentView`, `navigationVersion:2`, `function messagesVisible(){`, `!touchInput() && !e.shiftKey`, `id="chatTab"`, `function boot(){`, `theme.request('/gate/check'`, `window.canyouDeviceReady=`, `id="themeOptions"`} {
				if strings.Count(string(body), marker) != 1 {
					t.Fatalf("聊天分页或设备入口渲染错误: %s", marker)
				}
			}
		}
	}
	assertButton(true) // 服务启动即默认开启消息
	c, _, _ := connectChat(t, endpoint, "测试")
	if !server.IsRunning() {
		t.Fatal("服务应处于运行状态")
	}
	server.Stop()
	c.Close()
	if server.ChatEnabled() || server.IsRunning() {
		t.Fatal("停止未清理服务状态")
	}
	if err := server.Start(root, "", 0); err != nil {
		t.Fatal(err)
	}
	if !server.ChatEnabled() {
		t.Fatal("启动服务应自动开启消息")
	}
}

func TestChatUsesDeviceFingerprintAsClientID(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	server := NewFileServer("dev")
	root := t.TempDir()
	if err := server.Start(root, "", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	_, port, err := net.SplitHostPort(server.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://127.0.0.1:" + port
	welcomeWith := func(query string) chatEvent {
		t.Helper()
		header := http.Header{"Origin": []string{endpoint}}
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(endpoint, "http")+"/ws/chat?"+query, header)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return readChatEvent(t, c, "welcome")
	}
	fingerprint := strings.Repeat("ab", 13)
	if welcome := welcomeWith("name=" + url.QueryEscape("指纹设备") + "&id=" + fingerprint); welcome.ClientID != fingerprint {
		t.Fatalf("合法指纹应直接作为连接标识: %s", welcome.ClientID)
	}
	if welcome := welcomeWith("name=" + url.QueryEscape("伪造设备") + "&id=not%20hex"); len(welcome.ClientID) != 24 {
		t.Fatalf("非法指纹应回退为随机标识: %s", welcome.ClientID)
	}
	if welcome := welcomeWith("id=" + strings.Repeat("cd", 13)); welcome.Name != "设备-1" {
		t.Fatalf("无登记且不传 name 时后台应按 IP 尾号兜底下发设备名: %+v", welcome)
	}
}

func TestServerVersionOnGateAndFiles(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	for _, version := range []string{"v2.0.0", "v3.4.5-beta.1+build.7", "dev"} {
		t.Run(version, func(t *testing.T) {
			server := NewFileServer(version)
			t.Cleanup(server.Stop)
			root := t.TempDir()
			for cycle := 0; cycle < 2; cycle++ {
				if err := server.Start(root, "", 0); err != nil {
					t.Fatal(err)
				}
				_, port, err := net.SplitHostPort(server.listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				for _, locked := range []bool{true, false} {
					marker := `<div class="sub">在同一网络，轻松分享 · ` + version + `</div>`
					hash := ""
					if locked {
						hash = config.HashAccessPassword("version-test")
						marker = `<p class="brand">Canyou 共享 · ` + version + `</p>`
					}
					server.SetAccessPasswordHash(hash)
					request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/?version=forged", nil)
					if err != nil {
						t.Fatal(err)
					}
					request.Header.Set("Accept", "text/html")
					response, err := client.Do(request)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(response.Body)
					response.Body.Close()
					if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(html.UnescapeString(string(body)), marker) {
						t.Fatalf("启动轮次 %d，门禁=%v，页面未显示服务器版本 %s：状态=%d，错误=%v", cycle, locked, version, response.StatusCode, err)
					}
				}
				server.Stop()
			}
		})
	}
}

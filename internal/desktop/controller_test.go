package desktop

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"Canyou/internal/config"
	"Canyou/internal/netinfo"
	"Canyou/internal/server"
	"Canyou/internal/storage"

	"github.com/gorilla/websocket"
	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
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

// lanIPList 把纯 IP 列表包装成带网卡名的地址列表，供界面刷新测试使用
func lanIPList(ips ...string) []netinfo.LANIP {
	entries := make([]netinfo.LANIP, 0, len(ips))
	for index, ip := range ips {
		entries = append(entries, netinfo.LANIP{IP: ip, Adapter: fmt.Sprintf("Wi-Fi %d", index+1)})
	}
	return entries
}

// TestChatSurvivesIPRefresh 使用隐藏的原生窗体及临时数据库，验证刷新不会重启聊天室。
func TestChatSurvivesIPRefresh(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	server := server.NewFileServer("dev")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	portNumber := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(portNumber)
	if err := server.Start(t.TempDir(), "", portNumber); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	oldFont := winc.DefaultFont
	winc.DefaultFont = winc.NewFont("Microsoft YaHei UI", 9, 0)
	form := winc.NewForm(nil)
	form.SetSize(400, 430)
	controller := &controller{
		mainWindow: form, fileServer: server,
		config:     config.AppConfig{Port: port, SelectedIP: "192.0.2.20"},
		boldFont:   winc.NewFont("Microsoft YaHei UI", 9, winc.FontBold),
		qrTempFile: filepath.Join(t.TempDir(), "refresh-qr.png"),
	}
	defer func() {
		w32.DestroyWindow(form.Handle())
		controller.boldFont.Dispose()
		winc.DefaultFont.Dispose()
		winc.DefaultFont = oldFont
	}()
	controller.buildLayout()
	ipBounds := w32.GetWindowRect(controller.ipComboBox.Handle())
	refreshBounds := w32.GetWindowRect(controller.refreshIPButton.Handle())
	portBounds := w32.GetWindowRect(controller.portEdit.Handle())
	if ipBounds.Right >= refreshBounds.Left || refreshBounds.Right >= portBounds.Left || portBounds.Right-portBounds.Left != 48 {
		t.Fatalf("IP/刷新/端口控件布局重叠或输入宽度不符: %+v %+v %+v", ipBounds, refreshBounds, portBounds)
	}
	connection, _, _ := connectChat(t, "http://127.0.0.1:"+port, "刷新验证")
	for _, test := range []struct {
		name string
		ips  []string
		want string
	}{
		{"恢复保存的选择", []string{"192.0.2.10", "192.0.2.20"}, "192.0.2.20"},
		{"列表顺序变化", []string{"192.0.2.20", "192.0.2.10"}, "192.0.2.20"},
		{"旧IP已消失", []string{"192.0.2.30", "192.0.2.40"}, "192.0.2.30"},
		{"当前选择优先于启动配置", []string{"192.0.2.20", "192.0.2.30"}, "192.0.2.30"},
		{"网络断开", nil, ""},
		{"网络重新连接", []string{"198.51.100.10"}, "198.51.100.10"},
	} {
		controller.applyIPAddresses(lanIPList(test.ips...))
		controller.applyRunningState(true)
		if controller.currentIP() != test.want || controller.portEdit.Text() != port {
			t.Fatalf("%s: 地址选择或端口发生错误: %s %s", test.name, controller.currentIP(), controller.portEdit.Text())
		}
		if test.want == "" {
			if controller.currentURL != "" || controller.urlLabel.Text() != hintNoIP || controller.copyButton.Enabled() || controller.openButton.Enabled() {
				t.Fatal("无网络时仍保留旧访问地址或可用按钮")
			}
			if uint32(w32.GetWindowLong(controller.qrImageView.Handle(), w32.GWL_STYLE))&w32.WS_VISIBLE != 0 {
				t.Fatal("无网络时未隐藏旧二维码")
			}
		} else if controller.currentURL != buildAccessURL(test.want, port) || !controller.copyButton.Enabled() || !controller.openButton.Enabled() {
			t.Fatalf("%s: 访问地址或按钮未更新", test.name)
		}
		if !server.ChatEnabled() {
			t.Fatal("刷新重启了共享或关闭了聊天室")
		}
		if err := connection.WriteJSON(chatInput{Type: "message", Text: test.name}); err != nil {
			t.Fatal(err)
		}
		if event := readChatEvent(t, connection, "message"); event.Message.Text != test.name {
			t.Fatal("刷新中聊天室连接不可用")
		}
	}
	// 触发真实按钮回调，重新扫描网卡，并检查成功后恢复按钮和保存配置。
	controller.refreshIPButton.OnClick().Fire(winc.NewEvent(controller.refreshIPButton, nil))
	if !controller.refreshIPButton.Enabled() || !server.ChatEnabled() {
		t.Fatal("真实刷新按钮改变了服务状态")
	}
	if saved := config.LoadAppConfig(); saved.SelectedIP != controller.currentIP() || saved.Port != port {
		t.Fatalf("刷新后的地址或端口未保存: %+v", saved)
	}
	if err := connection.WriteJSON(chatInput{Type: "message", Text: "实际扫描后仍在线"}); err != nil {
		t.Fatal(err)
	}
	readChatEvent(t, connection, "message")
	// 回环与本机网卡分别访问同一个监听器；测试请求禁用代理，避免改变路由。
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	listenIPs := []string{"127.0.0.1"}
	for _, entry := range controller.lanIPs {
		listenIPs = append(listenIPs, entry.IP)
	}
	for _, ip := range listenIPs {
		response, err := client.Get(buildAccessURL(ip, port) + "/")
		if err != nil {
			t.Fatalf("本机地址 %s 无法访问共享监听器: %v", ip, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `id="chatToggle"`) {
			t.Fatalf("本机地址 %s 的共享监听器响应错误: 状态=%d", ip, response.StatusCode)
		}
	}
}

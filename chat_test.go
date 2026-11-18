package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// lanIPList 把纯 IP 列表包装成带网卡名的地址列表，供界面刷新测试使用
func lanIPList(ips ...string) []LANIP {
	entries := make([]LANIP, 0, len(ips))
	for index, ip := range ips {
		entries = append(entries, LANIP{IP: ip, Adapter: fmt.Sprintf("Wi-Fi %d", index+1)})
	}
	return entries
}

func newChatTestServer(t *testing.T) (*chatManager, *httptest.Server) {
	t.Helper()
	manager := newChatManager(filepath.Join(t.TempDir(), "chat.db"))
	server := httptest.NewServer(newShareHandler(t.TempDir(), "", manager))
	t.Cleanup(func() { manager.Disable(); server.Close() })
	return manager, server
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

func connectChat(t *testing.T, endpoint, name string) (*websocket.Conn, chatEvent, []chatMessage) {
	t.Helper()
	header := http.Header{"Origin": []string{endpoint}, "X-Forwarded-For": []string{"8.8.8.8"}}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(endpoint, "http")+"/ws/chat?name="+url.QueryEscape(name), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	welcome := readChatEvent(t, c, "welcome")
	var history []chatMessage
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

func TestChatBroadcastRenameRetentionAndRestart(t *testing.T) {
	manager, server := newChatTestServer(t)
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	a, wa, _ := connectChat(t, server.URL, "电脑")
	b, wb, _ := connectChat(t, server.URL, "电脑")
	readChatPresence(t, a, "joined", wb.ClientID, 2)
	if wa.ClientID == wb.ClientID || wa.IP != "127.0.0.1" || wb.IP != wa.IP {
		t.Fatalf("身份或 IP 识别错误: %+v %+v", wa, wb)
	}
	for i := 0; i < 3; i++ {
		if err := a.WriteJSON(chatInput{Type: "message", Text: "你好 <script>alert(1)</script>"}); err != nil {
			t.Fatal(err)
		}
		ea, eb := readChatEvent(t, a, "message"), readChatEvent(t, b, "message")
		if ea.Message.ID != eb.Message.ID || ea.Message.Name != "电脑" || ea.Message.ClientID != wa.ClientID {
			t.Fatal("广播或本人回显错误")
		}
	}
	a.WriteJSON(chatInput{Type: "rename", Name: "新名称"})
	readChatEvent(t, a, "renamed")
	readChatEvent(t, b, "renamed")
	a.WriteJSON(chatInput{Type: "message", Text: "改名后的消息"})
	if event := readChatEvent(t, a, "message"); event.Message.Name != "新名称" {
		t.Fatal("改名未生效")
	}
	readChatEvent(t, b, "message")
	b.WriteJSON(chatInput{Type: "retention", Limit: 2})
	ea, eb := readChatEvent(t, a, "settings"), readChatEvent(t, b, "settings")
	if ea.Limit != 2 || ea.OldestID != 3 || ea.OldestID != eb.OldestID {
		t.Fatal("全局保留边界未同步")
	}
	manager.Disable()
	var event chatEvent
	err := a.ReadJSON(&event)
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != chatDisabledCode {
		t.Fatalf("关闭未发送专用关闭码: %v", err)
	}
	if manager.Enabled() {
		t.Fatal("消息未关闭")
	}
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	_, welcome, history := connectChat(t, server.URL, "恢复设备")
	if welcome.Limit != 2 || len(history) != 2 || history[0].Name != "电脑" || history[1].Name != "新名称" {
		t.Fatalf("历史或设置未恢复: %+v %+v", welcome, history)
	}
}

func TestChatPresenceLifecycle(t *testing.T) {
	manager, server := newChatTestServer(t)
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	a, wa, history := connectChat(t, server.URL, "同名设备")
	if len(wa.Devices) != 1 || wa.Devices[0].ClientID != wa.ClientID || len(history) != 0 {
		t.Fatal("首个连接的在线快照或历史错误")
	}
	b, wb, history := connectChat(t, server.URL, "同名设备")
	joined := readChatPresence(t, a, "joined", wb.ClientID, 2)
	if len(history) != 0 || !reflect.DeepEqual(joined.Devices, wb.Devices) || wa.ClientID == wb.ClientID {
		t.Fatal("同名同 IP 连接被合并，或系统提示被保存为历史")
	}
	if err := b.WriteJSON(chatInput{Type: "rename", Name: "改名后的手机", RequestID: "rename"}); err != nil {
		t.Fatal(err)
	}
	for _, client := range []*websocket.Conn{a, b} {
		event := readChatEvent(t, client, "renamed")
		if event.RequestID != "rename" || len(event.Devices) != 2 {
			t.Fatalf("改名后未同步在线列表: %+v", event)
		}
		for _, device := range event.Devices {
			if device.ClientID == wb.ClientID && device.Name != "改名后的手机" {
				t.Fatal("在线列表保留了旧名称")
			}
		}
	}
	if err := b.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "离开"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	left := readChatPresence(t, a, "left", wb.ClientID, 1)
	if left.Name != "改名后的手机" || left.Devices[0].ClientID != wa.ClientID {
		t.Fatal("离开提醒未使用最新设备身份")
	}
	b.Close()
	c, wc, history := connectChat(t, server.URL, "异常断开设备")
	if len(history) != 0 {
		t.Fatal("连接、改名、离开提示不应写入历史")
	}
	readChatPresence(t, a, "joined", wc.ClientID, 2)
	c.Close()
	readChatPresence(t, a, "left", wc.ClientID, 1)
	// 用户消息作为事件屏障，严格检查没有重复的加入或离开提醒。
	if err := a.WriteJSON(chatInput{Type: "message", Text: "唯一需要持久化的消息"}); err != nil {
		t.Fatal(err)
	}
	if event := readChatEvent(t, a, "message"); event.Message.ID != 1 {
		t.Fatal("系统提醒占用了消息序号")
	}
	manager.Disable()
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	_, welcome, history := connectChat(t, server.URL, "重连")
	if len(welcome.Devices) != 1 || len(history) != 1 || history[0].Text != "唯一需要持久化的消息" {
		t.Fatal("重启后存在陈旧在线设备或系统提示历史")
	}
}

func TestChatSlowClientsPublishLeaveOnce(t *testing.T) {
	client := func(id string, capacity int) *chatClient {
		c := &chatClient{id: id, name: id, ip: "127.0.0.1", send: make(chan chatEvent, capacity)}
		// 仅验证事件循环队列；此处没有网络连接，跳过网络关闭。
		c.stopOnce.Do(func() {})
		return c
	}
	observer, slow, cascading := client("observer", 8), client("slow", 1), client("cascading", 1)
	room := &chatRoom{clients: map[*chatClient]bool{observer: true, slow: true, cascading: true}}
	slow.send <- chatEvent{Type: "occupied"}
	room.broadcast(chatEvent{Type: "message"})
	room.flushDepartures()
	if len(room.clients) != 1 || !room.clients[observer] || len(room.departures) != 0 {
		t.Fatal("慢连接级联移除未完成")
	}
	if event := <-observer.send; event.Type != "message" {
		t.Fatal("慢连接离开事件超越了原广播事件")
	}
	for _, id := range []string{"slow", "cascading"} {
		event := <-observer.send
		if event.Type != "presence" || event.Action != "left" || event.ClientID != id {
			t.Fatalf("慢连接离开事件错误: %+v", event)
		}
		if id == "cascading" && (len(event.Devices) != 1 || event.Devices[0].ClientID != observer.id) {
			t.Fatal("级联断开后在线快照未收敛")
		}
	}
	room.remove(slow)
	room.remove(cascading)
	room.flushDepartures()
	if len(observer.send) != 0 {
		t.Fatal("重复移除产生了重复离开提醒")
	}
}

func TestChatValidationAndAvailability(t *testing.T) {
	manager, server := newChatTestServer(t)
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/chat?name=PC"
	_, response, err := websocket.DefaultDialer.Dial(endpoint, http.Header{"Origin": []string{server.URL}})
	if err == nil || response.StatusCode != http.StatusForbidden {
		t.Fatal("关闭状态允许握手")
	}
	response.Body.Close()
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://evil.example", "", server.URL + "/spoof", strings.Replace(server.URL, "http:", "https:", 1)} {
		_, response, err = websocket.DefaultDialer.Dial(endpoint, http.Header{"Origin": []string{origin}})
		if err == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("接受了不合法 Origin: %q", origin)
		}
		response.Body.Close()
	}
	c, _, _ := connectChat(t, server.URL, "测试")
	for _, input := range []chatInput{
		{Type: "message", Text: "  \n "}, {Type: "message", Text: strings.Repeat("字", 2001)},
		{Type: "rename", Name: "\n"}, {Type: "rename", Name: strings.Repeat("字", 33)},
		{Type: "retention", Limit: 0}, {Type: "retention", Limit: 10001}, {Type: "unknown"},
	} {
		c.WriteJSON(input)
		readChatEvent(t, c, "error")
	}
	c.WriteMessage(websocket.TextMessage, []byte(`{"type":"retention","limit":1.5}`))
	readChatEvent(t, c, "error")
	c.WriteMessage(websocket.BinaryMessage, []byte("不支持二进制"))
	readChatEvent(t, c, "error")
	c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 17000)))
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("超大帧未被拒绝")
	}
}

func TestChatHistoryThenLiveOrdering(t *testing.T) {
	manager, server := newChatTestServer(t)
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	a, _, _ := connectChat(t, server.URL, "发送端")
	var send sync.WaitGroup
	send.Add(1)
	go func() {
		defer send.Done()
		for i := 0; i < 30; i++ {
			if err := a.WriteJSON(chatInput{Type: "message", Text: "顺序测试"}); err != nil {
				return
			}
		}
	}()
	b, _, history := connectChat(t, server.URL, "接收端")
	messages := append([]chatMessage(nil), history...)
	for len(messages) < 30 {
		event := readChatEvent(t, b, "message")
		messages = append(messages, *event.Message)
	}
	send.Wait()
	for i, message := range messages {
		if message.ID != int64(i+1) {
			t.Fatalf("历史和实时消息重复或遗漏: %d %+v", i, message)
		}
	}
}

func TestChatHistoryBatchesAndWriteErrors(t *testing.T) {
	manager, server := newChatTestServer(t)
	store, err := openChatStore(manager.path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// 直接在临时库准备跨三批历史，避免多次落盘使测试变慢。
	if _, err := store.db.Exec(`UPDATE settings SET retention=300;
WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<250)
INSERT INTO messages(client_id,name,ip,sent_at,body) SELECT 'old','历史设备','127.0.0.1','2026-01-01T00:00:00Z','历史 '||i FROM n;`); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/chat?name=PC", http.Header{"Origin": []string{server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if welcome := readChatEvent(t, c, "welcome"); welcome.Limit != 300 {
		t.Fatal("历史快照上限错误")
	}
	var previous int64
	for _, count := range []int{100, 100, 50} {
		event := readChatEvent(t, c, "history")
		if len(event.Messages) != count {
			t.Fatalf("历史分批错误: %d", len(event.Messages))
		}
		for _, m := range event.Messages {
			if m.ID != previous+1 {
				t.Fatal("历史排序或去重错误")
			}
			previous = m.ID
		}
	}
	readChatEvent(t, c, "ready")
	joined := readChatEvent(t, c, "presence")
	if joined.Action != "joined" || len(joined.Devices) != 1 {
		t.Fatal("历史加载完成后缺少在线状态")
	}
	peer, welcome, _ := connectChat(t, server.URL, "旁观设备")
	readChatPresence(t, c, "joined", welcome.ClientID, 2)
	if _, err := store.db.Exec(`CREATE TRIGGER reject_message BEFORE INSERT ON messages BEGIN SELECT RAISE(FAIL, '模拟不可写'); END`); err != nil {
		t.Fatal(err)
	}
	c.WriteJSON(chatInput{Type: "message", Text: "这条不能广播", RequestID: "failed"})
	if failure := readChatEvent(t, c, "error"); failure.RequestID != "failed" {
		t.Fatal("失败未关联请求")
	}
	// 下一条有效消息作为屏障：接收端的第一条事件必须是此消息，不能是失败消息。
	if _, err := store.db.Exec("DROP TRIGGER reject_message"); err != nil {
		t.Fatal(err)
	}
	c.WriteJSON(chatInput{Type: "message", Text: "成功消息", RequestID: "ok"})
	readChatEvent(t, c, "message")
	if message := readChatEvent(t, peer, "message"); message.Message.Text != "成功消息" || message.Message.ID != 251 {
		t.Fatal("失败消息被错误广播或提交")
	}
}

func TestChatFileServerLifecycleAndRendering(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	server := NewFileServer()
	if server.ChatEnabled() {
		t.Fatal("服务未启动不应开启消息")
	}
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

// TestChatSurvivesIPRefresh 使用隐藏的原生窗体及临时数据库，验证刷新不会重启聊天室。
func TestChatSurvivesIPRefresh(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	server := NewFileServer()
	if err := server.Start(t.TempDir(), "", 0); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	if address, ok := server.listener.Addr().(*net.TCPAddr); !ok || !address.IP.IsUnspecified() {
		t.Fatalf("共享服务应监听所有本机地址: %v", server.listener.Addr())
	}
	_, port, err := net.SplitHostPort(server.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	oldFont := winc.DefaultFont
	winc.DefaultFont = winc.NewFont("Microsoft YaHei UI", 9, 0)
	form := winc.NewForm(nil)
	form.SetSize(400, 430)
	controller := &UIController{
		mainWindow: form, fileServer: server,
		config:     AppConfig{Port: port, SelectedIP: "192.0.2.20"},
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
	listener, manager := server.listener, server.chat
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
		if server.listener != listener || server.chat != manager || !server.ChatEnabled() {
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
	if !controller.refreshIPButton.Enabled() || server.listener != listener || server.chat != manager || !server.ChatEnabled() {
		t.Fatal("真实刷新按钮改变了服务状态")
	}
	if saved := LoadAppConfig(); saved.SelectedIP != controller.currentIP() || saved.Port != port {
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

func TestFileRoutesRemainAvailableWithChat(t *testing.T) {
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("共享文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newChatManager(filepath.Join(t.TempDir(), "chat.db"))
	defer manager.Disable()
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	handler := newShareHandler(root, uploads, manager)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sample.txt", nil))
	if response.Code != 200 || response.Body.String() != "共享文件" {
		t.Fatal("消息功能影响了文件下载")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "uploaded.txt")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(part, "上传内容")
	form.Close()
	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result uploadResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.OK {
		t.Fatalf("上传失败: %s %v", response.Body.String(), err)
	}
	if data, err := os.ReadFile(filepath.Join(uploads, "uploaded.txt")); err != nil || string(data) != "上传内容" {
		t.Fatal("上传内容未正确保存")
	}
	values := url.Values{"base": {"/"}, "paths": {"/sample.txt"}}
	request = httptest.NewRequest(http.MethodPost, "/zip", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil || len(archive.File) != 1 {
		t.Fatalf("打包下载失败: %v", err)
	}
	reader, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "共享文件" {
		t.Fatal("打包下载内容不一致")
	}
}

func TestChatConcurrentToggle(t *testing.T) {
	manager, server := newChatTestServer(t)
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/chat?name=PC"
	for round := 0; round < 3; round++ {
		if err := manager.Enable(); err != nil {
			t.Fatal(err)
		}
		var attempts sync.WaitGroup
		for i := 0; i < 8; i++ {
			attempts.Add(1)
			go func() {
				defer attempts.Done()
				dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
				c, resp, err := dialer.Dial(endpoint, http.Header{"Origin": []string{server.URL}})
				if err == nil {
					c.SetReadDeadline(time.Now().Add(5 * time.Second))
					for {
						if _, _, err := c.ReadMessage(); err != nil {
							break
						}
					}
					c.Close()
				} else if resp != nil {
					resp.Body.Close()
				}
			}()
		}
		manager.Disable()
		attempts.Wait()
		if manager.Enabled() {
			t.Fatal("并发握手后关闭状态错误")
		}
	}
}

// TestChatBrowserPreview 仅显式开启时提供隔离的浏览器验收服务，不使用真实聊天数据库。
func TestChatBrowserPreview(t *testing.T) {
	if os.Getenv("CANYOU_CHAT_PREVIEW") != "1" {
		t.Skip("仅用于手动浏览器验收")
	}
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "示例文件.txt"), []byte("浏览与下载回归样例"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newChatManager(filepath.Join(t.TempDir(), "chat.db"))
	if err := manager.Enable(); err != nil { // 与真实服务语义一致：启动即开启消息
		t.Fatal(err)
	}
	defer manager.Disable()
	share := newShareHandler(root, uploads, manager)
	done := make(chan struct{})
	var once sync.Once
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__test/") {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			switch r.URL.Path {
			case "/__test/enable":
				if err := manager.Enable(); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			case "/__test/disable":
				manager.Disable()
			case "/__test/stop":
				once.Do(func() { close(done) })
			default:
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"enabled": manager.Enabled()})
			return
		}
		share.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:18081")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	defer srv.Close()
	go srv.Serve(listener)
	t.Log("浏览器验收地址：http://127.0.0.1:18081（测试数据库，仅回环地址）")
	select {
	case <-done:
	case <-time.After(20 * time.Minute):
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func TestValidChatDeviceID(t *testing.T) {
	valid := []string{strings.Repeat("ab", 13), "0123456789abcdef", strings.Repeat("0", 8), strings.Repeat("f", 64)}
	invalid := []string{"", strings.Repeat("a", 7), strings.Repeat("a", 65), strings.Repeat("AB", 13), "z" + strings.Repeat("a", 25), strings.Repeat("a", 25) + "g", "../../etc"}
	for _, id := range valid {
		if !validChatDeviceID(id) {
			t.Fatalf("合法指纹被拒绝: %s", id)
		}
	}
	for _, id := range invalid {
		if validChatDeviceID(id) {
			t.Fatalf("非法指纹被接受: %s", id)
		}
	}
}

func TestChatUsesDeviceFingerprintAsClientID(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	server := NewFileServer()
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

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const chatDisabledCode = 4001

// chatManager 的锁保护开启、关闭、握手，避免关闭时遗留新连接。
type chatManager struct {
	mu   sync.Mutex
	room *chatRoom
	path string
}

func newChatManager(path string) *chatManager { return &chatManager{path: path} }

func (m *chatManager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.room != nil
}

func (m *chatManager) Enable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.room != nil {
		return nil
	}
	filename := m.path
	if filename == "" {
		var err error
		filename, err = chatDatabasePath()
		if err != nil {
			return err
		}
	}
	store, err := openChatStore(filename)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	room := &chatRoom{store: store, ctx: ctx, cancel: cancel, commands: make(chan chatCommand), done: make(chan struct{}), clients: make(map[*chatClient]bool)}
	m.room = room
	go room.run()
	return nil
}

func (m *chatManager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.room != nil {
		m.room.cancel()
		<-m.room.done
		m.room = nil
	}
}

func chatPeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func validChatName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 32 {
		return false
	}
	return strings.IndexFunc(name, unicode.IsControl) == -1
}

// validChatDeviceID 校验前端上报的设备指纹标识：8–64 位小写十六进制（当前前端为双种子 cyrb53 拼成的 26 位）。
func validChatDeviceID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func chatSameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && origin.Scheme == scheme && origin.User == nil && strings.EqualFold(origin.Host, r.Host) && origin.Path == "" && origin.RawQuery == "" && origin.Fragment == ""
}

func (m *chatManager) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		http.Error(w, "消息连接仅支持 GET", http.StatusMethodNotAllowed)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.room == nil {
		http.Error(w, "主程序尚未打开消息", http.StatusForbidden)
		return
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		http.Error(w, "无法创建连接标识", http.StatusInternalServerError)
		return
	}
	clientID := hex.EncodeToString(id[:])
	// 客户端带来合法的设备指纹时优先沿用，同一设备重连后身份不再变化；缺失或非法时保持原来的随机行为。
	provided := r.URL.Query().Get("id")
	if validChatDeviceID(provided) {
		clientID = provided
	}
	// 设备名由后台下发：优先取门禁 allowed_devices 登记的权威名，其次兼容 URL 的 name 参数，
	// 都没有时按 IP 尾号兜底；前端连接不再需要传 name
	name := ""
	if validChatDeviceID(provided) {
		name = m.room.store.deviceName(provided)
	}
	if !validChatName(name) {
		name = strings.TrimSpace(r.URL.Query().Get("name"))
	}
	if !validChatName(name) {
		name = withIPTail("设备", r.RemoteAddr)
	}
	// 设备序号与预设底色取门禁 allowed_devices；未登记时序号 0、底色 -1（前端回退按编号散列取色）
	var deviceNo int64
	deviceColor := -1
	if validChatDeviceID(clientID) {
		deviceNo, deviceColor = m.room.store.deviceMeta(clientID)
	}
	upgrader := websocket.Upgrader{CheckOrigin: chatSameOrigin, HandshakeTimeout: 3 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 1024}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &chatClient{room: m.room, conn: conn, id: clientID, deviceNo: deviceNo, colorIndex: deviceColor, name: name, ip: chatPeerIP(r), send: make(chan chatEvent, 64), done: make(chan struct{})}
	accepted := make(chan bool, 1)
	if !m.room.submit(chatCommand{client: client, join: accepted}) || !<-accepted {
		client.stop(websocket.CloseTryAgainLater, "聊天室暂不可用")
		return
	}
	m.room.pumps.Add(2)
	go client.writePump()
	go client.readPump()
}

// chatAvatarColors 预设头像底色数量，必须与前端 chatScript 里的 AVATAR_COLORS 长度一致。
const chatAvatarColors = 8

// randAvatarColor 设备登记时随机分配一个预设底色索引（0..chatAvatarColors-1）。
func randAvatarColor() int {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return int(b[0]) % chatAvatarColors
}

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
	Type      string        `json:"type"`
	Action    string        `json:"action,omitempty"`
	Time      string        `json:"time,omitempty"`
	Devices   []chatDevice  `json:"devices,omitempty"`
	ClientID  string        `json:"clientId,omitempty"`
	No        int64         `json:"no,omitempty"`
	Color     int           `json:"color"`
	Name      string        `json:"name,omitempty"`
	IP        string        `json:"ip,omitempty"`
	Message   *chatMessage  `json:"message,omitempty"`
	Messages  []chatMessage `json:"messages,omitempty"`
	Limit     int           `json:"limit,omitempty"`
	OldestID  int64         `json:"oldestId,omitempty"`
	Error     string        `json:"error,omitempty"`
	RequestID string        `json:"requestId,omitempty"`
}

type chatCommand struct {
	client *chatClient
	join   chan bool
	leave  bool
	input  chatInput
}

type chatRoom struct {
	store      *chatStore
	ctx        context.Context
	cancel     context.CancelFunc
	commands   chan chatCommand
	done       chan struct{}
	clients    map[*chatClient]bool
	departures []chatDevice
	pumps      sync.WaitGroup
}

func (r *chatRoom) submit(command chatCommand) bool {
	select {
	case r.commands <- command:
		return true
	case <-r.ctx.Done():
		return false
	}
}

// run 串行协调历史快照、持久化及广播，保持所有客户端事件顺序一致。
func (r *chatRoom) run() {
	defer close(r.done)
	defer r.store.Close()
	defer func() {
		var closing sync.WaitGroup
		for c := range r.clients {
			closing.Add(1)
			go func(c *chatClient) {
				defer closing.Done()
				c.stop(chatDisabledCode, "主程序已关闭消息")
			}(c)
		}
		closing.Wait()
		r.pumps.Wait()
	}()
	for {
		if r.ctx.Err() != nil {
			return
		}
		select {
		case <-r.ctx.Done():
			return
		case command := <-r.commands:
			c := command.client
			switch {
			case command.join != nil:
				if len(r.clients) >= 64 {
					command.join <- false
					continue
				}
				history, limit, err := r.store.snapshot()
				if err != nil {
					logServiceFailure(err)
					command.join <- false
					continue
				}
				c.history, c.initialLimit = history, limit
				c.initialName = c.name
				c.initialColor = c.colorIndex
				r.clients[c] = true
				c.initialDevices = r.devices()
				r.broadcast(r.presence("joined", c.device()))
				command.join <- true
			case command.leave:
				r.remove(c)
			case r.clients[c]:
				r.handle(c, command.input)
			}
			r.flushDepartures()
		}
	}
}

func (c *chatClient) device() chatDevice {
	return chatDevice{ClientID: c.id, No: c.deviceNo, Color: c.colorIndex, Name: c.name, IP: c.ip}
}

func (r *chatRoom) devices() []chatDevice {
	devices := make([]chatDevice, 0, len(r.clients))
	for c := range r.clients {
		devices = append(devices, c.device())
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ClientID < devices[j].ClientID })
	return devices
}

func (r *chatRoom) presence(action string, device chatDevice) chatEvent {
	return chatEvent{Type: "presence", Action: action, ClientID: device.ClientID, No: device.No, Color: device.Color, Name: device.Name, IP: device.IP, Time: time.Now().UTC().Format(time.RFC3339Nano), Devices: r.devices()}
}

func (r *chatRoom) remove(c *chatClient) {
	if r.clients[c] {
		delete(r.clients, c)
		r.departures = append(r.departures, c.device())
	}
}

// 慢连接移除也可能发生在广播中；排队发布离开，避免递归广播及重复通知。
func (r *chatRoom) flushDepartures() {
	for len(r.departures) > 0 {
		device := r.departures[0]
		r.departures = r.departures[1:]
		r.broadcast(r.presence("left", device))
	}
	r.departures = nil
}

func (r *chatRoom) send(c *chatClient, event chatEvent) {
	if !r.clients[c] {
		return
	}
	select {
	case c.send <- event:
	default:
		r.remove(c)
		go c.stop(websocket.CloseTryAgainLater, "接收消息过慢，请重新连接")
	}
}

func (r *chatRoom) broadcast(event chatEvent) {
	for c := range r.clients {
		r.send(c, event)
	}
}

func (r *chatRoom) handle(c *chatClient, input chatInput) {
	failure := func(message string) { r.send(c, chatEvent{Type: "error", Error: message, RequestID: input.RequestID}) }
	if len(input.RequestID) > 64 {
		failure("请求标识过长")
		return
	}
	switch input.Type {
	case "message":
		text := strings.TrimSpace(input.Text)
		if !utf8.ValidString(text) || text == "" || utf8.RuneCountInString(text) > 2000 {
			failure("消息必须为 1–2000 个字符，且不能全为空白")
			return
		}
		message, limit, err := r.store.appendMessage(chatMessage{ClientID: c.id, No: c.deviceNo, Color: c.colorIndex, Name: c.name, IP: c.ip, Time: time.Now().UTC().Format(time.RFC3339Nano), Text: text})
		if err != nil {
			logServiceFailure(err)
			failure("消息保存失败，请检查主程序数据库后重试")
			return
		}
		r.broadcast(chatEvent{Type: "message", Message: &message, Limit: limit, RequestID: input.RequestID})
	case "rename":
		name := strings.TrimSpace(input.Name)
		if !validChatName(name) {
			failure("设备名称必须为 1–32 个字符，且不能含控制字符")
			return
		}
		c.name = name
		r.broadcast(chatEvent{Type: "renamed", ClientID: c.id, No: c.deviceNo, Color: c.colorIndex, Name: name, Devices: r.devices(), RequestID: input.RequestID})
		// 改名同步写回门禁 allowed_devices 的权威设备名（历史消息名字不回刷）；仅当连接身份为合法设备指纹时
		if validChatDeviceID(c.id) {
			if err := r.store.updateDeviceName(c.id, name); err != nil {
				logServiceFailure(err)
			}
		}
	case "color":
		if input.Color < 0 || input.Color >= chatAvatarColors {
			failure("头像底色取值无效")
			return
		}
		c.colorIndex = input.Color
		r.broadcast(chatEvent{Type: "recolored", ClientID: c.id, No: c.deviceNo, Color: c.colorIndex, Devices: r.devices(), RequestID: input.RequestID})
		// 底色写回门禁 allowed_devices（设备级属性，历史消息颜色读取时按最新值回填）；仅合法设备指纹时
		if validChatDeviceID(c.id) {
			if err := r.store.updateDeviceColor(c.id, c.name, input.Color); err != nil {
				logServiceFailure(err)
			}
		}
	case "retention":
		if input.Limit < 1 || input.Limit > maxChatLimit {
			failure("保留条数必须为 1–10000 的整数")
			return
		}
		oldest, err := r.store.setLimit(input.Limit)
		if err != nil {
			logServiceFailure(err)
			failure("保留数量保存失败，请检查主程序数据库后重试")
			return
		}
		r.broadcast(chatEvent{Type: "settings", Limit: input.Limit, OldestID: oldest, RequestID: input.RequestID})
	default:
		failure("无法识别的消息格式")
	}
}

type chatClient struct {
	room           *chatRoom
	conn           *websocket.Conn
	id, name, ip   string
	deviceNo       int64
	colorIndex     int
	initialName    string
	initialColor   int
	initialDevices []chatDevice
	history        []chatMessage
	initialLimit   int
	send           chan chatEvent
	done           chan struct{}
	stopOnce       sync.Once
}

func (c *chatClient) stop(code int, reason string) {
	c.stopOnce.Do(func() {
		c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(200*time.Millisecond))
		close(c.done)
		c.conn.Close()
	})
}

func (c *chatClient) readPump() {
	defer c.room.pumps.Done()
	defer c.room.submit(chatCommand{client: c, leave: true})
	defer c.stop(websocket.CloseNormalClosure, "连接已断开")
	c.conn.SetReadLimit(16 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(65 * time.Second))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(65 * time.Second)) })
	for {
		kind, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var input chatInput
		if kind != websocket.TextMessage || !utf8.Valid(data) || json.Unmarshal(data, &input) != nil {
			input = chatInput{Type: "invalid"}
		}
		if !c.room.submit(chatCommand{client: c, input: input}) {
			return
		}
	}
}

func (c *chatClient) write(event chatEvent) error {
	select {
	case <-c.done:
		return errors.New("连接已关闭")
	default:
	}
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteJSON(event)
}

func (c *chatClient) writePump() {
	defer c.room.pumps.Done()
	defer c.stop(websocket.CloseNormalClosure, "连接已断开")
	if c.write(chatEvent{Type: "welcome", ClientID: c.id, No: c.deviceNo, Color: c.initialColor, Name: c.initialName, IP: c.ip, Limit: c.initialLimit, Devices: c.initialDevices}) != nil {
		return
	}
	c.initialDevices = nil
	for start := 0; start < len(c.history); start += 100 {
		end := start + 100
		if end > len(c.history) {
			end = len(c.history)
		}
		if c.write(chatEvent{Type: "history", Messages: c.history[start:end]}) != nil {
			return
		}
	}
	c.history = nil
	if c.write(chatEvent{Type: "ready"}) != nil {
		return
	}
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case event := <-c.send:
			if c.write(event) != nil {
				return
			}
		case <-ticker.C:
			if c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		}
	}
}

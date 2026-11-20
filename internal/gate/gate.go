package gate

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"

	"Canyou/internal/device"
	"Canyou/internal/storage"
	"Canyou/internal/webui"
)

// Gate 设备门禁：设置密码后，只放行携带已授权设备 UUID 的请求。
// UUID 由浏览器端计算并自报（能放 header 就放 X-Device-ID，导航/下载类放 ?did=，WebSocket 放 ?id=），
// 首次验证通过的设备 UUID 记入允许名单、持久化在聊天记录同一个 SQLite 库的 allowed_devices 表；
// 身份可被刻意伪造，属"防无意访问"的已知取舍。
type Gate struct {
	mu           sync.Mutex
	version      string
	passwordHash string          // 访问密码的 SHA-256 hex；空表示门禁关闭
	allowed      map[string]bool // 已授权的设备 UUID 集合（启动时从库加载）
}

// New 创建门禁并从 SQLite 加载已授权名单；密码哈希随后由主程序从配置推送
func New(version string) *Gate {
	gate := &Gate{allowed: make(map[string]bool), version: version}
	if err := gate.load(); err != nil {
		log.Println("加载门禁设备名单失败:", err)
	}
	return gate
}

// load 从 allowed_devices 表重建内存名单；格式损坏的存量行直接忽略
func (gate *Gate) load() error {
	ids, err := storage.LoadAllowedDeviceIDs()
	for _, did := range ids {
		if device.ValidID(did) {
			gate.allowed[did] = true
		}
	}
	return err
}

// SetPasswordHash 热更新密码哈希，立即生效且无需重启服务；已授权名单保持不变
func (gate *Gate) SetPasswordHash(hash string) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.passwordHash = hash
}

func (gate *Gate) enabled() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.passwordHash != ""
}

func (gate *Gate) approved(did string) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.allowed[did]
}

func (gate *Gate) checkPassword(password string) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.passwordHash == "" {
		return true
	}
	sum := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare([]byte(gate.passwordHash), []byte(hex.EncodeToString(sum[:]))) == 1
}

// authorize 先查数据库，仅在记录缺失时登记；读取成功后才同步缓存并返回设备资料。
func (gate *Gate) authorize(did, name, remoteAddr string) (int64, string, string, error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	id, storedName, theme, err := gate.deviceInfo(did)
	if errors.Is(err, sql.ErrNoRows) {
		if err = storage.RegisterDevice(did, device.WithIPTail(name, remoteAddr), device.RandomAvatarColor()); err == nil {
			id, storedName, theme, err = gate.deviceInfo(did)
		}
	}
	gate.syncAllowed(did, err)
	return id, storedName, theme, err
}

// checkDevice 只读取设备记录并同步缓存，不创建记录；与授权操作共用锁，避免缓存更新乱序。
func (gate *Gate) checkDevice(did string) (int64, string, string, error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	id, name, theme, err := gate.deviceInfo(did)
	gate.syncAllowed(did, err)
	return id, name, theme, err
}

// syncAllowed 调用方持有 mu；缺失或读取失败时清除旧授权，避免继续使用过期缓存。
func (gate *Gate) syncAllowed(did string, err error) {
	if err != nil {
		delete(gate.allowed, did)
		return
	}
	gate.allowed[did] = true
}

// deviceInfo 保留数据库错误，区分记录缺失与读取失败；调用方负责同步缓存。
func (gate *Gate) deviceInfo(did string) (int64, string, string, error) {
	id, name, theme, err := storage.DeviceInfo(did)
	if !device.ValidTheme(theme) {
		theme = "system"
	}
	return id, name, theme, err
}

// deviceID 按 header → ?did= → ?id= 顺序提取设备标识；格式非法一律视为缺失。
// URL 里出现多个 did 时从后往前取：旧链接残留的失效 did 排在前位，不应阻断当前设备身份
func deviceID(request *http.Request) string {
	if candidate := request.Header.Get("X-Device-ID"); device.ValidID(candidate) {
		return candidate
	}
	query := request.URL.Query()
	if values := query["did"]; len(values) > 0 {
		for index := len(values) - 1; index >= 0; index-- {
			if device.ValidID(values[index]) {
				return values[index]
			}
		}
	}
	if candidate := query.Get("id"); device.ValidID(candidate) {
		return candidate
	}
	return ""
}

// Wrap 包在共享处理器外层：门禁接口始终可达；未设密码时全部直通（保持现状行为）；
// 已设密码时仅放行名单内设备，未授权整页导航改出密码页，其余请求直接 403
func (gate *Gate) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/gate/") {
			gate.serveAPI(writer, request)
			return
		}
		if !gate.enabled() || request.URL.Path == "/favicon.ico" {
			next.ServeHTTP(writer, request)
			return
		}
		if did := deviceID(request); did != "" && gate.approved(did) {
			next.ServeHTTP(writer, request)
			return
		}
		if isNavigation(request) {
			gate.servePage(writer, request)
			return
		}
		http.Error(writer, "该设备尚未通过访问验证", http.StatusForbidden)
	})
}

// isNavigation 仅整页 GET 请求进入密码页；WebSocket 握手与 fetch/XHR 一律 403
func isNavigation(request *http.Request) bool {
	if request.Method != http.MethodGet || request.Header.Get("Upgrade") != "" {
		return false
	}
	return strings.Contains(request.Header.Get("Accept"), "text/html")
}

func writeGateJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		log.Println("共享服务异常:", err)
	}
}

// serveAPI 提供授权检查、统一授权登记和主题保存接口。
func (gate *Gate) serveAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	var body struct {
		Did      string `json:"did"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Theme    string `json:"theme"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<16)).Decode(&body); err != nil {
		writeGateJSON(writer, http.StatusBadRequest, map[string]any{"ok": false, "allowed": false, "message": "请求格式错误"})
		return
	}
	switch request.URL.Path {
	case "/gate/check":
		if !device.ValidID(body.Did) {
			writeGateJSON(writer, http.StatusBadRequest, map[string]any{"ok": false, "allowed": false, "message": "设备标识无效，请刷新页面重试"})
			return
		}
		id, name, theme, err := gate.checkDevice(body.Did)
		if errors.Is(err, sql.ErrNoRows) {
			writeGateJSON(writer, http.StatusOK, map[string]any{"ok": false, "allowed": false, "requiresPassword": gate.enabled()})
			return
		}
		if err != nil {
			log.Println("读取门禁设备资料失败:", err)
			writeGateJSON(writer, http.StatusInternalServerError, map[string]any{"ok": false, "allowed": false, "message": "读取设备资料失败，请重试"})
			return
		}
		writeGateJSON(writer, http.StatusOK, map[string]any{"ok": true, "allowed": true, "id": id, "did": body.Did, "name": name, "theme": theme})
	case "/gate/unlock":
		if !device.ValidID(body.Did) {
			writeGateJSON(writer, http.StatusForbidden, map[string]any{"ok": false, "message": "设备标识无效，请刷新页面重试"})
			return
		}
		if !gate.checkPassword(body.Password) {
			writeGateJSON(writer, http.StatusForbidden, map[string]any{"ok": false, "message": "密码错误，请重试"})
			return
		}
		id, name, theme, err := gate.authorize(body.Did, body.Name, request.RemoteAddr)
		if err != nil {
			log.Println("登记门禁设备失败:", err)
			writeGateJSON(writer, http.StatusInternalServerError, map[string]any{"ok": false, "allowed": false, "message": "设备授权失败，请重试"})
			return
		}
		writeGateJSON(writer, http.StatusOK, map[string]any{"ok": true, "allowed": true, "id": id, "did": body.Did, "name": name, "theme": theme})
	case "/gate/theme":
		if !device.ValidID(body.Did) || !device.ValidTheme(body.Theme) {
			writeGateJSON(writer, http.StatusBadRequest, map[string]any{"ok": false, "message": "设备标识或主题选项无效"})
			return
		}
		// /gate/* 不经过普通资源的授权检查，这里必须单独核验，不能用保存偏好绕过门禁。
		if gate.enabled() {
			if !gate.approved(body.Did) {
				writeGateJSON(writer, http.StatusForbidden, map[string]any{"ok": false, "message": "该设备尚未通过访问验证"})
				return
			}
		}
		err := storage.SetDeviceTheme(body.Did, body.Theme)
		if errors.Is(err, sql.ErrNoRows) {
			writeGateJSON(writer, http.StatusForbidden, map[string]any{"ok": false, "message": "设备未登记，请刷新页面重新授权"})
			return
		}
		if err != nil {
			log.Println("共享服务异常:", err)
			writeGateJSON(writer, http.StatusInternalServerError, map[string]any{"ok": false, "message": "主题保存失败，请重试"})
			return
		}
		writeGateJSON(writer, http.StatusOK, map[string]any{"ok": true, "theme": body.Theme})
	default:
		http.Error(writer, "未找到", http.StatusNotFound)
	}
}

func (gate *Gate) servePage(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	if err := webui.RenderGate(writer, request.URL.RequestURI(), gate.version); err != nil {
		log.Println("共享服务异常:", err) // 响应已部分写出，仅记录
	}
}

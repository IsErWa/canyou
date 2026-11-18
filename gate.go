package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"
)

// accessGate 设备门禁：设置密码后，只放行携带已授权设备 UUID 的请求。
// UUID 由浏览器端计算并自报（能放 header 就放 X-Device-ID，导航/下载类放 ?did=，WebSocket 放 ?id=），
// 首次验证通过的设备 UUID 记入允许名单、持久化在聊天记录同一个 SQLite 库的 allowed_devices 表；
// 身份可被刻意伪造，属"防无意访问"的已知取舍。
type accessGate struct {
	mu           sync.Mutex
	passwordHash string          // 访问密码的 SHA-256 hex；空表示门禁关闭
	allowed      map[string]bool // 已授权的设备 UUID 集合（启动时从库加载）
}

// gateWithDB 在聊天库（chatDatabasePath）上执行操作：每次短时开关连接，避免与聊天室长期互持句柄；
// allowed_devices 表不占用 user_version，不会触发聊天存储的"版本较新"拦截
func gateWithDB(fn func(db *sql.DB) error) error {
	filename, err := chatDatabasePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=3000"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS allowed_devices (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 did TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL DEFAULT '',
 authorized_at TEXT NOT NULL
)`); err != nil {
		return err
	}
	// 旧版表（did 作主键、无 id 列）就地迁移到带主键 ID 与设备名的新结构，保留存量允许名单
	var idColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('allowed_devices') WHERE name='id'`).Scan(&idColumns); err != nil {
		return err
	}
	if idColumns == 0 {
		if _, err := db.ExecContext(ctx, `
ALTER TABLE allowed_devices RENAME TO allowed_devices_legacy;
CREATE TABLE allowed_devices (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 did TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL DEFAULT '',
 authorized_at TEXT NOT NULL
);
INSERT INTO allowed_devices(did, name, authorized_at) SELECT did, '', authorized_at FROM allowed_devices_legacy;
DROP TABLE allowed_devices_legacy;`); err != nil {
			return err
		}
	}
	// 迁移：为存量表补 color 列（预设头像底色索引，-1 表示未设置，前端按编号散列兜底）
	var colorColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('allowed_devices') WHERE name='color'`).Scan(&colorColumns); err != nil {
		return err
	}
	if colorColumns == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE allowed_devices ADD COLUMN color INTEGER NOT NULL DEFAULT -1`); err != nil {
			return err
		}
	}
	// 主题偏好独立于系统当前深浅色，旧设备默认跟随系统。
	var themeColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('allowed_devices') WHERE name='theme'`).Scan(&themeColumns); err != nil {
		return err
	}
	if themeColumns == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE allowed_devices ADD COLUMN theme TEXT NOT NULL DEFAULT 'system' CHECK(theme IN ('dark','light','system'))`); err != nil {
			return err
		}
	}
	return fn(db)
}

// newAccessGate 创建门禁并从 SQLite 加载已授权名单；密码哈希随后由主程序从配置推送
func newAccessGate() *accessGate {
	gate := &accessGate{allowed: make(map[string]bool)}
	if err := gate.load(); err != nil {
		log.Println("加载门禁设备名单失败:", err)
	}
	return gate
}

// load 从 allowed_devices 表重建内存名单；格式损坏的存量行直接忽略
func (gate *accessGate) load() error {
	return gateWithDB(func(db *sql.DB) error {
		rows, err := db.Query("SELECT did FROM allowed_devices")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var did string
			if err := rows.Scan(&did); err != nil {
				return err
			}
			if validChatDeviceID(did) {
				gate.allowed[did] = true
			}
		}
		return rows.Err()
	})
}

// hashAccessPassword 明文转 SHA-256 hex；空串保持为空，代表不设密码
func hashAccessPassword(plain string) string {
	if plain == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// SetPasswordHash 热更新密码哈希，立即生效且无需重启服务；已授权名单保持不变
func (gate *accessGate) SetPasswordHash(hash string) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.passwordHash = hash
}

func (gate *accessGate) enabled() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.passwordHash != ""
}

func (gate *accessGate) approved(did string) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.allowed[did]
}

func (gate *accessGate) checkPassword(password string) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.passwordHash == "" {
		return false
	}
	sum := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare([]byte(gate.passwordHash), []byte(hex.EncodeToString(sum[:]))) == 1
}

// sanitizeGateDeviceName 清洗上报设备名：去控制字符、截 32 字符并去首尾空白
func sanitizeGateDeviceName(name string) string {
	var runes []rune
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		runes = append(runes, r)
		if len(runes) >= 32 {
			break
		}
	}
	return strings.TrimSpace(string(runes))
}

// withIPTail 把客户端 IPv4 最后一段（1–3 位数字）拼到设备名后，区分同网段同类设备；
// 非 IPv4（如 IPv6）或尾段异常时保持原名
func withIPTail(name, remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	index := strings.LastIndex(host, ".")
	if index < 0 {
		return name
	}
	tail := host[index+1:]
	if tail == "" || len(tail) > 3 {
		return name
	}
	for _, r := range tail {
		if !unicode.IsDigit(r) {
			return name
		}
	}
	name = sanitizeGateDeviceName(name)
	if name == "" {
		name = "设备"
	}
	return name + "-" + tail
}

// authorize 登记设备：检查到允许名单里还没有这条数据时，才把设备名拼上 IP 尾号写入 SQLite；
// 已授权设备保留首次登记的名称不再刷新；写库失败仅记日志，本次运行内仍保持授权
func (gate *accessGate) authorize(did, name, remoteAddr string) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.allowed[did] {
		return
	}
	gate.allowed[did] = true
	if err := gateWithDB(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT OR IGNORE INTO allowed_devices(did, name, authorized_at, color) VALUES(?, ?, ?, ?)`, did, withIPTail(name, remoteAddr), time.Now().Format(time.RFC3339), randAvatarColor())
		return err
	}); err != nil {
		log.Println("保存门禁设备名单失败:", err)
	}
}

// ensureRegistered 未设密码时的幂等登记：语义与 authorize 相同（内存名单没有才写库），
// 用于让任何设备都进入 allowed_devices，从而获得统一的权威设备名
func (gate *accessGate) ensureRegistered(did, name, remoteAddr string) {
	gate.authorize(did, name, remoteAddr)
}

// deviceInfo 按 did 从 allowed_devices 读取主键 ID 与设备名，供门禁接口回传设备信息
func (gate *accessGate) deviceInfo(did string) (int64, string, string, bool) {
	var id int64
	var name, theme string
	err := gateWithDB(func(db *sql.DB) error {
		return db.QueryRow(`SELECT id, name, theme FROM allowed_devices WHERE did=?`, did).Scan(&id, &name, &theme)
	})
	if err != nil {
		return 0, "", "system", false
	}
	if !validDeviceTheme(theme) {
		theme = "system"
	}
	return id, name, theme, true
}

func validDeviceTheme(theme string) bool {
	return theme == "dark" || theme == "light" || theme == "system"
}

// deviceID 按 header → ?did= → ?id= 顺序提取设备标识；格式非法一律视为缺失。
// URL 里出现多个 did 时从后往前取：旧链接残留的失效 did 排在前位，不应阻断当前设备身份
func deviceID(request *http.Request) string {
	if candidate := request.Header.Get("X-Device-ID"); validChatDeviceID(candidate) {
		return candidate
	}
	query := request.URL.Query()
	if values := query["did"]; len(values) > 0 {
		for index := len(values) - 1; index >= 0; index-- {
			if validChatDeviceID(values[index]) {
				return values[index]
			}
		}
	}
	if candidate := query.Get("id"); validChatDeviceID(candidate) {
		return candidate
	}
	return ""
}

// intercept 包在共享处理器外层：门禁接口始终可达；未设密码时全部直通（保持现状行为）；
// 已设密码时仅放行名单内设备，未授权整页导航改出密码页，其余请求直接 403
func (gate *accessGate) intercept(next http.Handler) http.Handler {
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
		logServiceFailure(err)
	}
}

// serveAPI 门禁自身的两个 JSON 接口：check 供已授权设备静默直通，unlock 验证密码并登记设备
func (gate *accessGate) serveAPI(writer http.ResponseWriter, request *http.Request) {
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
		if !validChatDeviceID(body.Did) {
			writeGateJSON(writer, http.StatusOK, map[string]any{"ok": false, "allowed": false})
			return
		}
		// 未设密码：任何设备都登记以拿到权威设备名；已设密码：仅名单内设备回传信息，check 绝不绕过门禁登记
		if !gate.enabled() {
			gate.ensureRegistered(body.Did, body.Name, request.RemoteAddr)
		} else if !gate.approved(body.Did) {
			writeGateJSON(writer, http.StatusOK, map[string]any{"ok": false, "allowed": false})
			return
		}
		id, name, theme, found := gate.deviceInfo(body.Did)
		if !found {
			writeGateJSON(writer, http.StatusOK, map[string]any{"ok": false, "allowed": false})
			return
		}
		writeGateJSON(writer, http.StatusOK, map[string]any{"ok": true, "allowed": true, "id": id, "did": body.Did, "name": name, "theme": theme})
	case "/gate/unlock":
		if !validChatDeviceID(body.Did) {
			writeGateJSON(writer, http.StatusForbidden, map[string]any{"ok": false, "message": "设备标识无效，请刷新页面重试"})
			return
		}
		if !gate.checkPassword(body.Password) {
			writeGateJSON(writer, http.StatusForbidden, map[string]any{"ok": false, "message": "密码错误，请重试"})
			return
		}
		gate.authorize(body.Did, body.Name, request.RemoteAddr)
		id, name, theme, _ := gate.deviceInfo(body.Did)
		writeGateJSON(writer, http.StatusOK, map[string]any{"ok": true, "id": id, "did": body.Did, "name": name, "theme": theme})
	case "/gate/theme":
		if !validChatDeviceID(body.Did) || !validDeviceTheme(body.Theme) {
			writeGateJSON(writer, http.StatusBadRequest, map[string]any{"ok": false, "message": "设备标识或主题选项无效"})
			return
		}
		// /gate/* 不经过普通资源的授权检查，这里必须单独核验，不能用保存偏好绕过门禁。
		if gate.enabled() {
			if !gate.approved(body.Did) {
				writeGateJSON(writer, http.StatusForbidden, map[string]any{"ok": false, "message": "该设备尚未通过访问验证"})
				return
			}
		} else {
			gate.ensureRegistered(body.Did, body.Name, request.RemoteAddr)
		}
		err := gateWithDB(func(db *sql.DB) error {
			result, err := db.Exec(`UPDATE allowed_devices SET theme=? WHERE did=?`, body.Theme, body.Did)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err == nil && count != 1 {
				return sql.ErrNoRows
			}
			return err
		})
		if err != nil {
			logServiceFailure(err)
			writeGateJSON(writer, http.StatusInternalServerError, map[string]any{"ok": false, "message": "主题保存失败，请重试"})
			return
		}
		writeGateJSON(writer, http.StatusOK, map[string]any{"ok": true, "theme": body.Theme})
	default:
		http.Error(writer, "未找到", http.StatusNotFound)
	}
}

// safeInternalTarget 只允许跳回本站内部路径，杜绝 //evil.com 之类的开放重定向
func safeInternalTarget(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "/"
	}
	if strings.ContainsAny(target, "\\\"'<>` ") || strings.Contains(target, "\n") || strings.Contains(target, "\r") {
		return "/"
	}
	return target
}

// jsonString 把值编码成 JS 字面量并转义 < ，防止路径内容截断 <script>
func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return strings.ReplaceAll(string(data), "<", "\\u003c")
}

func (gate *accessGate) servePage(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	data := struct{ Next string }{jsonString(safeInternalTarget(request.URL.RequestURI()))}
	if err := gateTemplate.ExecuteTemplate(writer, "gatePage", data); err != nil {
		logServiceFailure(err) // 响应已部分写出，仅记录
	}
}

// gateTemplate 复用聊天模板里的 identityScript（UUID 计算与落盘），门禁页自身单独定义
var gateTemplate = template.Must(template.New("gate").Parse(chatTemplates + gateHTML))

const gateHTML = `{{define "gatePage"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" id="themeColor" content="#f4f6fa">
<title>Canyou · 访问验证</title>
{{template "themeBootstrap"}}
<style>
{{template "themeStyles"}}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;min-height:100dvh;display:grid;place-items:center;background:var(--bg);color:var(--text);
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI",Roboto,Helvetica,Arial,sans-serif;
  -webkit-font-smoothing:antialiased;padding:max(24px,env(safe-area-inset-top)) max(16px,env(safe-area-inset-right)) max(24px,env(safe-area-inset-bottom)) max(16px,env(safe-area-inset-left))}
.box{width:min(420px,100%);background:var(--card);border:1px solid var(--border);border-radius:12px;
  box-shadow:var(--shadow);padding:32px;text-align:center}
.ic{display:grid;place-items:center;width:56px;height:56px;margin:0 auto 20px;border-radius:16px;background:var(--accent-soft);color:var(--accent)}
.icon{width:28px;height:28px;fill:none;stroke:currentColor;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.brand{font-size:13px;color:var(--muted);margin:0 0 8px}
h1{font-size:21px;letter-spacing:-.5px;margin:0 0 8px;font-weight:700}
.sub{font-size:12.5px;color:var(--muted);margin:0 0 18px;overflow-wrap:anywhere}
.note{display:flex;align-items:center;justify-content:center;gap:10px;font-size:13px;color:var(--muted);padding:10px 12px;border:1px dashed var(--border);border-radius:12px}
.note[hidden]{display:none}
.gate-spinner{width:18px;height:18px;flex:none;border:2px solid var(--border);border-top-color:var(--accent);border-radius:50%;animation:gate-spin .8s linear infinite}
@keyframes gate-spin{to{transform:rotate(360deg)}}
form{display:flex;flex-direction:column;gap:10px}
form[hidden]{display:none}
input{font:inherit;color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:12px 14px;font-size:16px;width:100%;min-height:48px}
label{font-size:13px;text-align:left;color:var(--muted)}
:focus-visible{outline:3px solid var(--accent);outline-offset:3px}
input:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-soft)}
button{appearance:none;border:0;border-radius:8px;min-height:48px;padding:12px 22px;font-size:14px;font-weight:650;cursor:pointer;
  background:var(--accent);color:var(--accent-ink);transition:.15s;font-family:inherit}
button:hover{filter:brightness(1.06)}
button:disabled{opacity:.45;cursor:not-allowed}
.err{font-size:12.5px;color:var(--bad)}
.err[hidden]{display:none}
@media(max-width:767px){.box{padding:28px 20px}body{padding:max(16px,env(safe-area-inset-top)) max(12px,env(safe-area-inset-right)) max(16px,env(safe-area-inset-bottom)) max(12px,env(safe-area-inset-left))}}
@media(prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}
</style>
</head>
<body>
{{template "identityScript"}}
{{template "uiIcons"}}
<main class="box">
  <div class="ic"><svg class="icon" aria-hidden="true"><use href="#i-lock"/></svg></div>
  <p class="brand">Canyou 共享</p>
  <h1>访问此共享需要密码</h1>
  <p class="sub">本设备尚未被局域网内的 Canyou 授权</p>
  <div class="note" id="gateNote" role="status" aria-live="polite" aria-atomic="true"><span class="gate-spinner" aria-hidden="true"></span><span>正在核验设备身份…</span></div>
  <form id="gateForm" hidden>
    <label for="gatePass">访问密码</label>
    <input id="gatePass" type="password" placeholder="请输入访问密码" autocomplete="current-password">
    <button type="submit" id="gateBtn">验证并进入</button>
    <div class="err" id="gateError" role="alert" hidden></div>
  </form>
</main>
<script>
(function(){
  var did=window.CANYOU_DID||'';
  var next={{.Next}};
  var note=document.getElementById('gateNote'), form=document.getElementById('gateForm');
  var pass=document.getElementById('gatePass'), btn=document.getElementById('gateBtn'), err=document.getElementById('gateError');
  function showError(message){ err.textContent=message; err.hidden=false; btn.disabled=false; }
  function showForm(tip){ if(form.hidden){ note.hidden=true; form.hidden=false; pass.focus(); } err.textContent=tip; err.hidden=!tip; }
  // 带着当前 did 跳回原目标：canyouWithDid 会先清掉旧链接残留的 did，避免失效身份卡在参数首位死循环
  function into(){ location.replace(window.canyouWithDid(next)); }
  if(!did){ showForm('浏览器安全存储不可用，验证可能无法保持，仍可尝试密码'); return; }
  fetch('/gate/check',{method:'post',headers:{'Content-Type':'application/json'},body:JSON.stringify({did:did})})
    .then(function(r){ return r.json(); })
    .then(function(data){ if(data.allowed){ window.canyouTheme.accept(data.theme); into(); } else showForm(''); })
    .catch(function(){ showForm(''); });
  form.addEventListener('submit',function(e){
    e.preventDefault();
    btn.disabled=true; err.hidden=true;
    fetch('/gate/unlock',{method:'post',headers:{'Content-Type':'application/json'},body:JSON.stringify({did:did,password:pass.value,name:window.canyouDeviceName?canyouDeviceName():''})})
      .then(function(r){ return r.json().then(function(data){ return {ok:r.ok,data:data}; }); })
      .then(function(x){ if(x.ok && x.data.ok){ window.canyouTheme.accept(x.data.theme); into(); } else showError(x.data.message||'验证失败，请重试'); })
      .catch(function(){ showError('网络错误，请重试'); });
  });
})();
</script>
</body>
</html>{{end}}`

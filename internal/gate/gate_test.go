package gate

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Canyou/internal/config"
	"Canyou/internal/share"
	"Canyou/internal/storage"
)

// gateTestDid 模拟前端指纹落盘后的合法设备标识（26 位小写 hex）
const gateTestDid = "ababababababababababababab"

// newGateTestServer 拉起带独立门禁与聊天服务的完整共享监听器，门禁名单所在的 chat.db 固定在 LOCALAPPDATA 临时目录
func newGateTestServer(t *testing.T) (*Gate, *httptest.Server) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("共享文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	gate := New("dev")
	server := httptest.NewServer(gate.Wrap(share.NewHandler(root, uploads, func() bool { return true }, "dev", nil)))
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
	gate.SetPasswordHash(config.HashAccessPassword("1234"))

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
	if reloaded := New("dev"); !reloaded.approved(gateTestDid) {
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

// TestGateUnlockRegistersWithoutPassword 无密码时 check 不登记，unlock 才创建并返回权威资料。
func TestGateUnlockRegistersWithoutPassword(t *testing.T) {
	gate, server := newGateTestServer(t)
	code, result := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid, "name": "会议室平板"})
	if code != 200 || result["allowed"] != false || result["requiresPassword"] != false {
		t.Fatalf("未登记设备 check 应返回 false: %d %v", code, result)
	}
	if _, found := gateStoredName(t, gateTestDid); found || gate.approved(gateTestDid) {
		t.Fatal("check 不得新增设备或授权缓存")
	}
	code, result = postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "name": "会议室平板"})
	if code != 200 || result["ok"] != true || result["allowed"] != true || result["name"] != "会议室平板-1" || result["did"] != gateTestDid {
		t.Fatalf("无密码 unlock 应登记并回传设备名: %d %v", code, result)
	}
	firstID, isNum := result["id"].(float64)
	if !isNum || firstID <= 0 || !gate.approved(gateTestDid) {
		t.Fatalf("unlock 应回传主键并更新缓存: %v", result)
	}
	if _, found := gateStoredName(t, gateTestDid); !found {
		t.Fatal("无密码 unlock 未写入 allowed_devices")
	}
	// 二次 check 命中同一记录：ID 不变、名称不被新上报值刷新
	_, again := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid, "name": "换个名"})
	if again["id"] != firstID || again["name"] != "会议室平板-1" {
		t.Fatalf("重复 check 应返回同一登记信息: %+v", again)
	}
}

func TestGateThemePersistence(t *testing.T) {
	_, server := newGateTestServer(t)
	_, first := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "name": "主题设备"})
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
	_, result := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": other})
	if result["theme"] != "system" {
		t.Fatal("主题污染其他设备")
	}
	// 主题保存不再兼任无密码登记入口。
	if code, result := postGateJSON(t, server, "/gate/theme", map[string]string{"did": "eeeeeeeeeeeeeeee", "theme": "light"}); code != 403 || result["ok"] != false {
		t.Fatalf("未登记设备保存主题应失败: %d %v", code, result)
	}
	if _, found := gateStoredName(t, "eeeeeeeeeeeeeeee"); found {
		t.Fatal("主题保存不应补建设备")
	}
	if err := func() error {
		filename, err := config.DatabasePath()
		if err != nil {
			return err
		}
		store, err := storage.Open(filename)
		if err != nil {
			return err
		}
		defer store.Close()
		if err := store.UpdateDeviceName(gateTestDid, "改名后"); err != nil {
			return err
		}
		return store.UpdateDeviceColor(gateTestDid, "改名后", 3)
	}(); err != nil {
		t.Fatal(err)
	}
	reloaded := New("dev")
	reloaded.SetPasswordHash(config.HashAccessPassword("1234"))
	restarted := httptest.NewServer(reloaded.Wrap(http.NotFoundHandler()))
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
	gate.SetPasswordHash(config.HashAccessPassword("1234"))
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
	if code, _ := postGateJSON(t, server, "/gate/theme", map[string]string{"did": gateTestDid, "theme": "dark"}); code != 403 {
		t.Fatalf("更新不存在的设备应要求重新授权: %d", code)
	}
}

func TestGateDatabaseAuthoritativeCheck(t *testing.T) {
	for _, password := range []string{"", "1234"} {
		t.Run("password="+password, func(t *testing.T) {
			gate, server := newGateTestServer(t)
			gate.SetPasswordHash(config.HashAccessPassword(password))
			if err := storage.RegisterDevice(gateTestDid, "库内设备", 2); err != nil {
				t.Fatal(err)
			}
			if err := storage.SetDeviceTheme(gateTestDid, "dark"); err != nil {
				t.Fatal(err)
			}
			if gate.approved(gateTestDid) {
				t.Fatal("测试前缓存应为空")
			}
			code, result := postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid, "name": "不得覆盖"})
			if code != 200 || result["ok"] != true || result["allowed"] != true || result["name"] != "库内设备" || result["theme"] != "dark" || !gate.approved(gateTestDid) {
				t.Fatalf("check 未按数据库恢复缓存及资料: %d %v", code, result)
			}
			gateDeleteDevice(t)
			code, result = postGateJSON(t, server, "/gate/check", map[string]string{"did": gateTestDid})
			if code != 200 || result["allowed"] != false || result["requiresPassword"] != (password != "") || gate.approved(gateTestDid) {
				t.Fatalf("check 未清理过期缓存: %d %v", code, result)
			}
			if _, found := gateStoredName(t, gateTestDid); found {
				t.Fatal("check 不得补建删除的设备")
			}
			for _, field := range []string{"id", "did", "name", "theme"} {
				if _, exists := result[field]; exists {
					t.Fatalf("未授权响应不应包含设备资料: %s", field)
				}
			}
			if password != "" {
				if code, _ := gateGet(t, server, "/sample.txt", "", "X-Device-ID", gateTestDid); code != 403 {
					t.Fatalf("清除缓存后资源仍被放行: %d", code)
				}
			}
		})
	}
}

func gateDeleteDevice(t *testing.T) {
	t.Helper()
	if err := gateWithDB(func(db *sql.DB) error {
		_, err := db.Exec(`DELETE FROM allowed_devices WHERE did=?`, gateTestDid)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGateUnlockRestoresDeletedDevice(t *testing.T) {
	for _, password := range []string{"", "1234"} {
		t.Run("password="+password, func(t *testing.T) {
			gate, server := newGateTestServer(t)
			gate.SetPasswordHash(config.HashAccessPassword(password))
			payload := map[string]string{"did": gateTestDid, "password": password, "name": "重新登记"}
			_, first := postGateJSON(t, server, "/gate/unlock", payload)
			gateDeleteDevice(t)
			if !gate.approved(gateTestDid) {
				t.Fatal("测试需要保留已删除设备的旧缓存")
			}
			if password != "" {
				code, result := postGateJSON(t, server, "/gate/unlock", map[string]string{"did": gateTestDid, "password": "错误密码"})
				if code != 403 || result["ok"] != false || result["message"] == "" {
					t.Fatalf("旧缓存不得绕过密码验证: %d %v", code, result)
				}
				if _, found := gateStoredName(t, gateTestDid); found {
					t.Fatal("错误密码补建了设备")
				}
			}
			code, restored := postGateJSON(t, server, "/gate/unlock", payload)
			if code != 200 || restored["ok"] != true || restored["allowed"] != true || restored["name"] != "重新登记-1" || restored["id"] == first["id"] || !gate.approved(gateTestDid) {
				t.Fatalf("旧缓存阻止重新登记: %d %v", code, restored)
			}
			_, checked := postGateJSON(t, server, "/gate/check", payload)
			if checked["allowed"] != true || checked["id"] != restored["id"] {
				t.Fatalf("重新登记后刷新仍未授权: %v", checked)
			}
			if err := storage.SetDeviceTheme(gateTestDid, "dark"); err != nil {
				t.Fatal(err)
			}
			if err := gateWithDB(func(db *sql.DB) error {
				_, err := db.Exec(`UPDATE allowed_devices SET color=3, authorized_at='原授权时间' WHERE did=?;
CREATE TRIGGER reject_insert BEFORE INSERT ON allowed_devices BEGIN SELECT RAISE(ABORT,'已有记录不得再次插入'); END`, gateTestDid)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			payload["name"] = "不得覆盖"
			code, again := postGateJSON(t, server, "/gate/unlock", payload)
			if code != 200 || again["id"] != restored["id"] || again["name"] != restored["name"] || again["theme"] != "dark" {
				t.Fatalf("重复授权不应写入或覆盖资料: %d %v", code, again)
			}
			if err := gateWithDB(func(db *sql.DB) error {
				var color int
				var authorized string
				err := db.QueryRow(`SELECT color, authorized_at FROM allowed_devices WHERE did=?`, gateTestDid).Scan(&color, &authorized)
				if color != 3 || authorized != "原授权时间" {
					t.Error("重复授权覆盖了底色或授权时间")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGateDatabaseFailureDoesNotAuthorize(t *testing.T) {
	for _, password := range []string{"", "1234"} {
		for _, fault := range []string{"read", "insert", "readback"} {
			t.Run("password="+password+"/"+fault, func(t *testing.T) {
				gate, server := newGateTestServer(t)
				gate.SetPasswordHash(config.HashAccessPassword(password))
				payload := map[string]string{"did": gateTestDid, "password": password}
				postGateJSON(t, server, "/gate/unlock", payload)
				gateDeleteDevice(t)
				if err := gateWithDB(func(db *sql.DB) error {
					statements := map[string]string{
						"read":     `ALTER TABLE allowed_devices RENAME COLUMN name TO broken_name`,
						"insert":   `CREATE TRIGGER reject_insert BEFORE INSERT ON allowed_devices BEGIN SELECT RAISE(ABORT,'模拟登记失败'); END`,
						"readback": `CREATE TRIGGER remove_insert AFTER INSERT ON allowed_devices BEGIN DELETE FROM allowed_devices WHERE did=NEW.did; END`,
					}
					_, err := db.Exec(statements[fault])
					return err
				}); err != nil {
					t.Fatal(err)
				}
				code, result := postGateJSON(t, server, "/gate/unlock", payload)
				if code != 500 || result["ok"] != false || result["allowed"] != false || result["message"] == "" || gate.approved(gateTestDid) {
					t.Fatalf("故障时授权伪报成功或残留缓存: %d %v", code, result)
				}
				if result["id"] != nil || result["name"] != nil {
					t.Fatal("授权失败不应返回虚假的设备资料")
				}
				ids, err := storage.LoadAllowedDeviceIDs()
				if err != nil || len(ids) != 0 {
					t.Fatalf("故障后不应产生设备记录: %v %v", ids, err)
				}
				gate.mu.Lock()
				gate.allowed[gateTestDid] = true
				gate.mu.Unlock()
				code, result = postGateJSON(t, server, "/gate/check", payload)
				want := 200
				if fault == "read" {
					want = 500
				}
				if code != want || result["allowed"] != false || gate.approved(gateTestDid) {
					t.Fatalf("检查失败未清除缓存或混淆缺失与读取故障: %d %v", code, result)
				}
			})
		}
	}
}

func TestGateThemeColumnMigration(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	filename, err := config.DatabasePath()
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
	gate := New("dev")
	id, name, theme, err := gate.deviceInfo(gateTestDid)
	if err != nil || id != 7 || name != "旧设备" || theme != "system" {
		t.Fatalf("旧设备主题迁移失败: %d %s %s %v", id, name, theme, err)
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
	_, _, theme, err = gate.deviceInfo(gateTestDid)
	if err != nil || theme != "system" {
		t.Fatal("异常主题没有回退到跟随系统")
	}
}

// TestGateAllowedDevicesLegacyMigration 旧版表结构（did 主键、无 id/name 列）应自动迁移且存量名单不丢
func TestGateAllowedDevicesLegacyMigration(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	filename, err := config.DatabasePath()
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

	gate := New("dev") // 首次访问库时触发迁移
	if _, _, theme, err := gate.deviceInfo(gateTestDid); err != nil || theme != "system" {
		t.Fatal("最旧版本设备未迁移为跟随系统")
	}
	if !gate.approved(gateTestDid) {
		t.Fatal("旧表存量名单迁移后应仍生效")
	}
	if _, _, _, err := gate.authorize("cdcdcdcdcdcdcdcdcdcdcdcdcd", "客厅电视", "10.0.0.99:5"); err != nil {
		t.Fatal(err)
	}
	if got := gateNameStoredInDB(t, "cdcdcdcdcdcdcdcdcdcdcdcdcd"); got != "客厅电视-99" {
		t.Fatalf("新登记设备应拼接 IP 尾号: %q", got)
	}
	if got := gateNameStoredInDB(t, gateTestDid); got != "" {
		t.Fatalf("迁移行设备名应为空: %q", got)
	}
}

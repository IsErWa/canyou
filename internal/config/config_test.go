package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestChatDatabaseLocalPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	got, err := DatabasePath()
	if err != nil || got != filepath.Join(root, "Programs", "Canyou", "chat.db") {
		t.Fatalf("数据库路径错误: %s %v", got, err)
	}
}

func TestExistingConfigurationRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("验证 Windows 旧配置目录")
	}
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	if got := LoadAppConfig(); got.Port != DefaultPort {
		t.Fatalf("默认端口变化：%+v", got)
	}
	filename, err := configFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if filename != filepath.Join(root, "Canyou", "config.json") {
		t.Fatalf("配置目录变化：%s", filename)
	}
	legacy := `{"directory":"C:\\共享目录","upload_directory":"C:\\接收文件","port":"9090","selected_ip":"192.168.1.2","password_hash":"legacy-hash","password":"旧密码"}`
	if err := os.WriteFile(filename, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	want := AppConfig{Directory: `C:\共享目录`, UploadDirectory: `C:\接收文件`, Port: "9090", SelectedIP: "192.168.1.2", PasswordHash: "legacy-hash", Password: "旧密码", Bandwidth: DefaultBandwidthLimits()}
	if got := LoadAppConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("旧配置读取失败：%+v", got)
	}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	if got := LoadAppConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("旧配置保存后丢失字段：%+v", got)
	}
	if err := os.WriteFile(filename, []byte(`{"port":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := LoadAppConfig(); got.Port != DefaultPort {
		t.Fatal("空端口没有回退")
	}
	if err := os.WriteFile(filename, []byte(`{`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := LoadAppConfig(); got.Port != DefaultPort {
		t.Fatal("损坏配置没有回退")
	}
}

func TestPasswordHashCompatibility(t *testing.T) {
	if HashAccessPassword("") != "" {
		t.Fatal("空密码必须关闭门禁")
	}
	if got := HashAccessPassword("1234"); got != "03ac674216f3e15c761ee1a5e255f067953623c8b388b4459e13f978d7c846f4" {
		t.Fatalf("密码哈希算法变化：%s", got)
	}
}

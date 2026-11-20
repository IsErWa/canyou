package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// AppConfig 跨启动保留的用户配置：查看目录、上传目录、端口与上次选中的网卡 IP
type AppConfig struct {
	Bandwidth       BandwidthLimits `json:"bandwidth"`
	Directory       string          `json:"directory"`
	UploadDirectory string          `json:"upload_directory"`
	Port            string          `json:"port"`
	SelectedIP      string          `json:"selected_ip"`
	PasswordHash    string          `json:"password_hash"` // 访问密码的 SHA-256 hex；空表示未设密码
	Password        string          `json:"password"`      // 访问密码明文，仅供界面回显与复制；门禁校验仍走 PasswordHash
}

// appConfigFile 返回 %AppData%/Canyou 目录下指定文件的完整路径，目录不存在时创建
func appConfigFile(name string) (string, error) {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(baseDir, "Canyou")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(appDir, name), nil
}

// configFilePath 返回配置文件路径：%AppData%/Canyou/config.json
func configFilePath() (string, error) {
	return appConfigFile("config.json")
}

// LoadAppConfig 读取配置；文件缺失或损坏时回退到默认端口
func LoadAppConfig() AppConfig {
	config := AppConfig{Port: DefaultPort, Bandwidth: DefaultBandwidthLimits()}

	path, err := configFilePath()
	if err != nil {
		return config
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config
	}

	_ = json.Unmarshal(data, &config)
	if config.Port == "" {
		config.Port = DefaultPort
	}
	return config
}

// Save 将配置写回磁盘
func (config AppConfig) Save() error {
	if err := config.Bandwidth.Validate(); err != nil {
		return err
	}
	path, err := configFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// DefaultPort 是新配置和桌面输入框共用的默认端口。
const DefaultPort = "8080"

// hashAccessPassword 明文转 SHA-256 hex；空串保持为空，代表不设密码
func HashAccessPassword(plain string) string {
	if plain == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// chatDatabasePath 与原有 Roaming 配置分开，聊天记录放在用户指定的 Local 目录。
func DatabasePath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "Programs", "Canyou", "chat.db"), nil
}

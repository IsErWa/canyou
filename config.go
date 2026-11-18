package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AppConfig 跨启动保留的用户配置：查看目录、上传目录、端口与上次选中的网卡 IP
type AppConfig struct {
	Directory       string `json:"directory"`
	UploadDirectory string `json:"upload_directory"`
	Port            string `json:"port"`
	SelectedIP      string `json:"selected_ip"`
	PasswordHash    string `json:"password_hash"` // 访问密码的 SHA-256 hex；空表示未设密码
	Password        string `json:"password"`      // 访问密码明文，仅供界面回显与复制；门禁校验仍走 PasswordHash
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
	config := AppConfig{Port: defaultPort}

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
		config.Port = defaultPort
	}
	return config
}

// Save 将配置写回磁盘
func (config AppConfig) Save() error {
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

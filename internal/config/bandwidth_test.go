package config

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBandwidthRateConversion(t *testing.T) {
	for text, want := range map[string]int64{"0": 0, "1": 1000000, "1.25": 1250000, "0.000001": 1, "9999.999999": 9999999999, "10000": MaxBandwidthRate, " 2.5000 ": 2500000} {
		got, err := ParseBandwidthRate(text)
		if err != nil || got != want {
			t.Fatalf("%q: %d %v", text, got, err)
		}
		if again, err := ParseBandwidthRate(FormatBandwidthRate(got)); err != nil || again != want {
			t.Fatal("回显丢失精度", again, err)
		}
	}
	for _, text := range []string{"", " ", "-1", "+1", "NaN", "Inf", "1e3", ".1", "1.", "1.0000001", "10000.000001", "10001", "1,5", "1.2.3", "9999999999999999999999999"} {
		if _, err := ParseBandwidthRate(text); err == nil {
			t.Fatal("接受非法数值", text)
		}
	}
}

func TestBandwidthDefaults(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	want := BandwidthLimits{PerIPUpload: 10000000, PerIPDownload: 10000000, TotalUpload: 10000000, TotalDownload: 10000000}
	if got := LoadAppConfig(); got.Bandwidth != want {
		t.Fatalf("首次启动的四项限速不是 10 MB/s：%+v", got.Bandwidth)
	}
	filename, err := configFilePath()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		raw  string
		want BandwidthLimits
	}{
		{"旧配置缺少限速", `{"port":"9090"}`, want},
		{"空限速对象", `{"bandwidth":{}}`, want},
		{"未配置限速对象", `{"bandwidth":null}`, want},
		{"部分配置保留显式零", `{"bandwidth":{"per_ip_upload_bytes_per_second":0}}`, BandwidthLimits{PerIPDownload: 10000000, TotalUpload: 10000000, TotalDownload: 10000000}},
		{"保留全部不限速", `{"bandwidth":{"per_ip_upload_bytes_per_second":0,"per_ip_download_bytes_per_second":0,"total_upload_bytes_per_second":0,"total_download_bytes_per_second":0}}`, BandwidthLimits{}},
		{"保留自定义限额", `{"bandwidth":{"per_ip_upload_bytes_per_second":1250000,"per_ip_download_bytes_per_second":2000000,"total_upload_bytes_per_second":0,"total_download_bytes_per_second":3000000}}`, BandwidthLimits{PerIPUpload: 1250000, PerIPDownload: 2000000, TotalDownload: 3000000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filename, []byte(test.raw), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := LoadAppConfig()
			if cfg.Bandwidth != test.want {
				t.Fatalf("默认值或已保存设置错误：%+v", cfg.Bandwidth)
			}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			if got := LoadAppConfig(); got.Bandwidth != test.want {
				t.Fatalf("保存再加载改变了限速：%+v", got.Bandwidth)
			}
		})
	}
}

func TestBandwidthConfigurationCompatibility(t *testing.T) {
	for _, raw := range []string{`{"per_ip_upload_bytes_per_second":-1}`, `{"total_download_bytes_per_second":10000000001}`, `{"per_ip_download_bytes_per_second":"fast"}`, `{"total_upload_bytes_per_second":9999999999999999999999999}`, `[]`} {
		var cfg AppConfig
		if err := json.Unmarshal([]byte(`{"directory":"test","bandwidth":`+raw+`,"port":"9090","password_hash":"old"}`), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Directory != "test" || cfg.Port != "9090" || cfg.PasswordHash != "old" || cfg.Bandwidth != (BandwidthLimits{}) {
			t.Fatalf("限速损坏影响了其他设置: %+v", cfg)
		}
	}
	t.Setenv("APPDATA", t.TempDir())
	cfg := AppConfig{Port: "9090", Bandwidth: BandwidthLimits{1, 2000000, 3000000, MaxBandwidthRate}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if got := LoadAppConfig(); got != cfg {
		t.Fatalf("限速未持久化: %+v", got)
	}
	filename, err := configFilePath()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Bandwidth.TotalUpload = -1
	if err := cfg.Save(); err == nil {
		t.Fatal("非法限速写入配置")
	}
	after, _ := os.ReadFile(filename)
	if string(before) != string(after) {
		t.Fatal("校验失败修改了原文件")
	}
}

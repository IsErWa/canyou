package config

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
)

const (
	DefaultBandwidthRate int64 = 10 * 1000000
	MaxBandwidthRate     int64 = 10000 * 1000000
)

// BandwidthLimits 使用字节/秒保存；0 表示对应层和方向不限速。
type BandwidthLimits struct {
	PerIPUpload   int64 `json:"per_ip_upload_bytes_per_second"`
	PerIPDownload int64 `json:"per_ip_download_bytes_per_second"`
	TotalUpload   int64 `json:"total_upload_bytes_per_second"`
	TotalDownload int64 `json:"total_download_bytes_per_second"`
}

// DefaultBandwidthLimits 为未配置的四项限速提供 10 MB/s 默认值；零值仍表示不限速。
func DefaultBandwidthLimits() BandwidthLimits {
	return BandwidthLimits{
		PerIPUpload: DefaultBandwidthRate, PerIPDownload: DefaultBandwidthRate,
		TotalUpload: DefaultBandwidthRate, TotalDownload: DefaultBandwidthRate,
	}
}

func (limits BandwidthLimits) Validate() error {
	for _, rate := range []int64{limits.PerIPUpload, limits.PerIPDownload, limits.TotalUpload, limits.TotalDownload} {
		if rate < 0 || rate > MaxBandwidthRate {
			return fmt.Errorf("限速必须在 0–10000 MB/s 之间")
		}
	}
	return nil
}

// UnmarshalJSON 隔离损坏的限速字段，避免影响旧配置中的其他设置。
func (limits *BandwidthLimits) UnmarshalJSON(data []byte) error {
	type plain BandwidthLimits
	decoded := plain(DefaultBandwidthLimits())
	err := json.Unmarshal(data, &decoded)
	if err == nil {
		err = BandwidthLimits(decoded).Validate()
	}
	if err != nil {
		log.Println("限速配置无效，已回退为不限速:", err)
		*limits = BandwidthLimits{}
		return nil
	}
	*limits = BandwidthLimits(decoded)
	return nil
}

// ParseBandwidthRate 精确解析十进制 MB/s，不通过浮点数舍入字节数。
func ParseBandwidthRate(text string) (int64, error) {
	invalid := fmt.Errorf("请输入 0–10000 之间的 MB/s 数值，最多六位小数；0 表示不限速")
	text = strings.TrimSpace(text)
	parts := strings.Split(text, ".")
	if len(parts) > 2 || len(parts[0]) == 0 {
		return 0, invalid
	}
	for _, part := range parts {
		if part == "" {
			return 0, invalid
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return 0, invalid
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > 10000 {
		return 0, invalid
	}
	var fraction int64
	if len(parts) == 2 {
		if len(parts[1]) > 6 {
			return 0, invalid
		}
		fraction, _ = strconv.ParseInt(parts[1]+strings.Repeat("0", 6-len(parts[1])), 10, 64)
	}
	rate := whole*1000000 + fraction
	if rate > MaxBandwidthRate {
		return 0, invalid
	}
	return rate, nil
}

func FormatBandwidthRate(rate int64) string {
	whole := strconv.FormatInt(rate/1000000, 10)
	if fraction := rate % 1000000; fraction != 0 {
		return whole + "." + strings.TrimRight(fmt.Sprintf("%06d", fraction), "0")
	}
	return whole
}

package device

import (
	"crypto/rand"
	"net"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const AvatarColors = 8

func PeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func ValidName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 32 {
		return false
	}
	return strings.IndexFunc(name, unicode.IsControl) == -1
}

// validChatDeviceID 校验前端上报的设备指纹标识：8–64 位小写十六进制（当前前端为双种子 cyrb53 拼成的 26 位）。
func ValidID(id string) bool {
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

// randAvatarColor 设备登记时随机分配一个预设底色索引（0..chatAvatarColors-1）。
func RandomAvatarColor() int {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return int(b[0]) % AvatarColors
}

// sanitizeGateDeviceName 清洗上报设备名：去控制字符、截 32 字符并去首尾空白
func SanitizeName(name string) string {
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
func WithIPTail(name, remoteAddr string) string {
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
	name = SanitizeName(name)
	if name == "" {
		name = "设备"
	}
	return name + "-" + tail
}

func ValidTheme(theme string) bool {
	return theme == "dark" || theme == "light" || theme == "system"
}

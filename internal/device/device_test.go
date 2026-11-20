package device

import (
	"strings"
	"testing"
)

func TestValidChatDeviceID(t *testing.T) {
	valid := []string{strings.Repeat("ab", 13), "0123456789abcdef", strings.Repeat("0", 8), strings.Repeat("f", 64)}
	invalid := []string{"", strings.Repeat("a", 7), strings.Repeat("a", 65), strings.Repeat("AB", 13), "z" + strings.Repeat("a", 25), strings.Repeat("a", 25) + "g", "../../etc"}
	for _, id := range valid {
		if !ValidID(id) {
			t.Fatalf("合法指纹被拒绝: %s", id)
		}
	}
	for _, id := range invalid {
		if ValidID(id) {
			t.Fatalf("非法指纹被接受: %s", id)
		}
	}
}

// TestWithIPTail IP 尾号拼接：IPv4 取末段 1–3 位，非 IPv4 或非法尾段保持原名
func TestWithIPTail(t *testing.T) {
	cases := []struct{ name, remote, want string }{
		{"Windows 电脑", "10.173.25.70:52134", "Windows 电脑-70"},
		{"Android 设备", "192.168.1.108:1", "Android 设备-108"},
		{"", "192.168.1.1:1", "设备-1"},
		{"Mac", "[::1]:1234", "Mac"},
		{"Mac", "hostname.local:80", "Mac"},
		{"Mac", "1.2.3.4567:80", "Mac"},
	}
	for _, c := range cases {
		if got := WithIPTail(c.name, c.remote); got != c.want {
			t.Fatalf("withIPTail(%q, %q) = %q，应为 %q", c.name, c.remote, got, c.want)
		}
	}
}

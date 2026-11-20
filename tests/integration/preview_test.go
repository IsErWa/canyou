package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"Canyou/internal/bandwidth"
	"Canyou/internal/chat"
	"Canyou/internal/config"
	"Canyou/internal/gate"
	"Canyou/internal/server"
	"Canyou/internal/webui"
)

// TestGateBrowserPreview 仅显式开启时提供隔离的门禁浏览器验收服务，临时 chat.db 目录、密码固定 1234。
func TestGateBrowserPreview(t *testing.T) {
	if os.Getenv("CANYOU_GATE_PREVIEW") != "1" {
		t.Skip("仅用于手动浏览器验收")
	}
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root, uploads := t.TempDir(), t.TempDir()
	limits := bandwidth.New()
	bandwidthPreview := os.Getenv("CANYOU_BANDWIDTH_PREVIEW") == "1"
	if bandwidthPreview {
		if err := limits.Update(config.BandwidthLimits{
			PerIPUpload: 256 * 1024, PerIPDownload: 256 * 1024,
			TotalUpload: 512 * 1024, TotalDownload: 512 * 1024,
		}); err != nil {
			t.Fatal(err)
		}
		data := make([]byte, 1024*1024)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "bandwidth-sample.bin")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("限速验收：每 IP 上下行各 256 KiB/s，总上下行各 512 KiB/s；样本 %s；SHA256 %x；上传目录 %s", path, sha256.Sum256(data), uploads)
	}
	if err := os.WriteFile(filepath.Join(root, "示例文件.txt"), []byte("门禁验收样例"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "子目录"), 0o755); err != nil { // 供验收单页内目录导航
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "子目录", "内层文件.txt"), []byte("子目录验收样例"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 多条目、长文件名及深路径只写入隔离目录，用于响应式布局验收。
	for i := 1; i <= 30; i++ {
		name := fmt.Sprintf("测试文档-%02d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("布局验收文档"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, strings.Repeat("很长的文件名", 12)+".txt"), []byte("长名称验收"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "子目录", "项目资料", "交付文件", "最终版本"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 两张生成图供验收图片预览与左右翻页
	for _, spec := range []struct {
		name       string
		red, green uint8
	}{
		{"预览图A.png", 79, 110},
		{"预览图B.png", 220, 90},
	} {
		file, err := os.Create(filepath.Join(root, spec.name))
		if err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 160, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 160; x++ {
				img.Set(x, y, color.RGBA{R: spec.red, G: spec.green, B: uint8(x), A: 255})
			}
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
	}
	// 预览验收样例：可播放 WAV / 最小合法 PDF / 文本 / HTML / 占位 mp4 与 zip
	writePreviewWav(t, filepath.Join(root, "预览音A.wav"))
	for name, content := range map[string]string{
		"&lt;svg onload=window.X=1&gt;'&.txt": "文件名转义验收",
		"预览文本.txt":                            "文本预览验收样例：浏览器应按 text/plain 在新标签页直接显示。",
		"预览页.html":                            "<h1>HTML 预览验收</h1>\n<p>Canyou 新标签页渲染测试。</p>\n",
		"预览视频.mp4":                            "非真实 mp4，仅验证点击后浮层出现 video 元素的结构",
		"压缩样本.zip":                            "PK\x03\x04 非完整压缩包，应保持点击下载",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "预览页.pdf"), minimalPDFBytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := chat.NewManager("") // 与生产一致：聊天库回落到 chatDatabasePath，与门禁 allowed_devices 同库，改名才能回写
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	defer manager.Disable()
	version := os.Getenv("CANYOU_PREVIEW_VERSION")
	gate := gate.New(version)
	gate.SetPasswordHash(config.HashAccessPassword("1234"))
	share := gate.Wrap(server.NewHandler(root, uploads, manager, version, limits))
	done := make(chan struct{})
	var stopOnce sync.Once
	mux := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" && request.URL.Query().Get("__test_files_only") == "1" && request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := webui.RenderFiles(writer, webui.FilesData{ClientIP: "127.0.0.1", Version: version}); err != nil {
				t.Error(err)
			}
			return
		}
		// iframe 的视口由元素尺寸决定，供无窗口缩放能力的浏览器工具验收断点。
		if request.URL.Path == "/__test/layout" && request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(writer, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>Canyou 响应式验收</title></head><body style="margin:0;background:#e2e7ef"><iframe id="previewFrame" title="响应式工作台预览" src="/" style="display:block;border:0;width:1440px;height:900px;color-scheme:light"></iframe></body></html>`)
			return
		}
		if request.URL.Path == "/__test/stop" && request.Method == http.MethodPost {
			stopOnce.Do(func() { close(done) })
			return
		}
		share.ServeHTTP(writer, request)
	})
	address := "127.0.0.1:18082"
	if bandwidthPreview {
		address = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	defer server.Close()
	go server.Serve(listener)
	t.Logf("门禁验收地址：http://%s（密码 1234，仅回环地址）", listener.Addr())
	select {
	case <-done:
	case <-time.After(20 * time.Minute):
	}
}

// writePreviewWav 生成 1 秒 440Hz 正弦波的 16bit 单声道 PCM WAV，供验收浮层内音频真播放
func writePreviewWav(t *testing.T, target string) {
	t.Helper()
	const rate = 8000
	data := make([]byte, rate*2)
	for i := 0; i < rate; i++ {
		v := int16(12000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	buf := &bytes.Buffer{}
	u32 := func(v uint32) { binary.Write(buf, binary.LittleEndian, v) }
	u16 := func(v uint16) { binary.Write(buf, binary.LittleEndian, v) }
	buf.WriteString("RIFF")
	u32(uint32(36 + len(data)))
	buf.WriteString("WAVEfmt ")
	u32(16)
	u16(1)
	u16(1)
	u32(rate)
	u32(rate * 2)
	u16(2)
	u16(16)
	buf.WriteString("data")
	u32(uint32(len(data)))
	buf.Write(data)
	if err := os.WriteFile(target, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// minimalPDFBytes 拼装带正确 xref 偏移的单页文本 PDF（纯 ASCII，避免 xref 长度问题），供验收 Chrome 内置查看器
func minimalPDFBytes() []byte {
	stream := "BT /F1 18 Tf 20 60Td (Canyou PDF) Tj ET"
	objects := []string{
		"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj",
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj",
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 240 120] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >> endobj",
		fmt.Sprintf("4 0 obj << /Length %d >> stream\n%s\nendstream endobj", len(stream), stream),
		"5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj",
	}
	buf := &bytes.Buffer{}
	offsets := make([]int, len(objects)+1)
	buf.WriteString("%PDF-1.4\n")
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		buf.WriteString(obj)
		buf.WriteString("\n")
	}
	xref := buf.Len()
	fmt.Fprintf(buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(buf, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// TestChatBrowserPreview 仅显式开启时提供隔离的浏览器验收服务，不使用真实聊天数据库。
func TestChatBrowserPreview(t *testing.T) {
	if os.Getenv("CANYOU_CHAT_PREVIEW") != "1" {
		t.Skip("仅用于手动浏览器验收")
	}
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "示例文件.txt"), []byte("浏览与下载回归样例"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := chat.NewManager(filepath.Join(t.TempDir(), "chat.db"))
	if err := manager.Enable(); err != nil { // 与真实服务语义一致：启动即开启消息
		t.Fatal(err)
	}
	defer manager.Disable()
	share := server.NewHandler(root, uploads, manager, os.Getenv("CANYOU_PREVIEW_VERSION"), nil)
	done := make(chan struct{})
	var once sync.Once
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__test/") {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			switch r.URL.Path {
			case "/__test/enable":
				if err := manager.Enable(); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			case "/__test/disable":
				manager.Disable()
			case "/__test/stop":
				once.Do(func() { close(done) })
			default:
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"enabled": manager.Enabled()})
			return
		}
		share.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:18081")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	defer srv.Close()
	go srv.Serve(listener)
	t.Log("浏览器验收地址：http://127.0.0.1:18081（测试数据库，仅回环地址）")
	select {
	case <-done:
	case <-time.After(20 * time.Minute):
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

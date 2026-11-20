package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Canyou/internal/config"
)

func TestBandwidthDoesNotBlockControlRoutesOrChat(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root := t.TempDir()
	data := bytes.Repeat([]byte("a"), 64*1024)
	if err := os.WriteFile(filepath.Join(root, "file.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	server := NewFileServer("dev")
	defer server.Stop()
	if err := server.SetBandwidthLimits(config.BandwidthLimits{PerIPDownload: 1, TotalDownload: 1, PerIPUpload: 1, TotalUpload: 1}); err != nil {
		t.Fatal(err)
	}
	if err := server.Start(root, t.TempDir(), 0); err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(server.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://127.0.0.1:" + port
	connection, _, _ := connectChat(t, endpoint, "限速验收")
	defer connection.Close()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	done := make(chan error, 1)
	go func() {
		response, err := client.Get(endpoint + "/file.bin")
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			err = readErr
			if err == nil && !bytes.Equal(body, data) {
				err = io.ErrUnexpectedEOF
			}
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("低速传输没有等待", err)
	case <-time.After(100 * time.Millisecond):
	}
	for _, route := range []struct{ path, body string }{
		{"/", ""}, {"/favicon.ico", ""}, {"/api/list", `{"path":"/"}`},
		{"/gate/check", `{"did":"12121212121212121212121212"}`},
		{"/gate/unlock", `{"did":"12121212121212121212121212","name":"测试"}`},
		{"/gate/theme", `{"did":"12121212121212121212121212","theme":"light"}`},
	} {
		method := http.MethodGet
		if route.body != "" {
			method = http.MethodPost
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		req, _ := http.NewRequestWithContext(ctx, method, endpoint+route.path, strings.NewReader(route.body))
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			cancel()
			t.Fatalf("控制接口受限速影响 %s: %v", route.path, err)
		}
		_, err = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		cancel()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("控制接口不可用 %s: %d %v", route.path, response.StatusCode, err)
		}
	}
	oldListener, oldChat := server.listener, server.chat
	if err := server.SetBandwidthLimits(config.BandwidthLimits{}); err != nil {
		t.Fatal(err)
	}
	if server.listener != oldListener || server.chat != oldChat {
		t.Fatal("热更新重启了服务")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("既有下载未即时解除限速")
	}
	if err := connection.WriteJSON(chatInput{Type: "message", Text: "限速修改后仍在线"}); err != nil {
		t.Fatal(err)
	}
	if event := readChatEvent(t, connection, "message"); event.Message.Text != "限速修改后仍在线" {
		t.Fatal("原连接不再可用")
	}
	if err := server.SetBandwidthLimits(config.BandwidthLimits{TotalDownload: 1}); err != nil {
		t.Fatal(err)
	}
	go func() {
		response, err := client.Get(endpoint + "/file.bin")
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("停止前下载没有等待")
	case <-time.After(100 * time.Millisecond):
	}
	server.Stop()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("停止时返回了完整文件")
		}
	case <-time.After(time.Second):
		t.Fatal("停止服务未终止等待的传输")
	}
	if err := server.Start(root, "", 0); err != nil {
		t.Fatal("停止后不能复用服务", err)
	}
}

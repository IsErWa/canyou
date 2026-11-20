package integration

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Canyou/internal/chat"
	"Canyou/internal/server"
)

func TestFileRoutesRemainAvailableWithChat(t *testing.T) {
	root, uploads := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("共享文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := chat.NewManager(filepath.Join(t.TempDir(), "chat.db"))
	defer manager.Disable()
	if err := manager.Enable(); err != nil {
		t.Fatal(err)
	}
	handler := server.NewHandler(root, uploads, manager, "dev", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sample.txt", nil))
	if response.Code != 200 || response.Body.String() != "共享文件" {
		t.Fatal("消息功能影响了文件下载")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "uploaded.txt")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(part, "上传内容")
	form.Close()
	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.OK {
		t.Fatalf("上传失败: %s %v", response.Body.String(), err)
	}
	if data, err := os.ReadFile(filepath.Join(uploads, "uploaded.txt")); err != nil || string(data) != "上传内容" {
		t.Fatal("上传内容未正确保存")
	}
	values := url.Values{"base": {"/"}, "paths": {"/sample.txt"}}
	request = httptest.NewRequest(http.MethodPost, "/zip", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil || len(archive.File) != 1 {
		t.Fatalf("打包下载失败: %v", err)
	}
	reader, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "共享文件" {
		t.Fatal("打包下载内容不一致")
	}
}

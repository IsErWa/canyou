package share

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"Canyou/internal/bandwidth"
	"Canyou/internal/config"
)

func bandwidthHandler(t *testing.T, limits config.BandwidthLimits) (http.Handler, *bandwidth.Manager, []byte, string) {
	t.Helper()
	root, uploads := t.TempDir(), t.TempDir()
	data := make([]byte, 64*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "media.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	manager := bandwidth.New()
	if err := manager.Update(limits); err != nil {
		t.Fatal(err)
	}
	return NewHandler(root, uploads, nil, "dev", manager), manager, data, uploads
}

func TestBandwidthConcurrentDownloads(t *testing.T) {
	for _, test := range []struct {
		name    string
		limits  config.BandwidthLimits
		sameIP  bool
		minimum time.Duration
	}{
		{"同IP共享", config.BandwidthLimits{PerIPDownload: 128 * 1024}, true, 800 * time.Millisecond},
		{"不同IP独立", config.BandwidthLimits{PerIPDownload: 128 * 1024}, false, 350 * time.Millisecond},
		{"不同IP共用总额", config.BandwidthLimits{TotalDownload: 128 * 1024}, false, 800 * time.Millisecond},
		{"两层同时生效", config.BandwidthLimits{PerIPDownload: 256 * 1024, TotalDownload: 128 * 1024}, false, 800 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, _, data, _ := bandwidthHandler(t, test.limits)
			start := time.Now()
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					req := httptest.NewRequest(http.MethodGet, "/media.bin?did="+strings.Repeat(string(rune('a'+i)), 32), nil)
					req.RemoteAddr = "192.0.2.1:1234"
					if i == 1 {
						req.RemoteAddr = "192.0.2.1:5678"
						if !test.sameIP {
							req.RemoteAddr = "192.0.2.2:5678"
						}
					}
					req.Header.Set("X-Forwarded-For", "198.51.100."+string(rune('1'+i)))
					req.Header.Set("X-Real-IP", "203.0.113."+string(rune('1'+i)))
					ctx, cancel := context.WithTimeout(req.Context(), 8*time.Second)
					defer cancel()
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, req.WithContext(ctx))
					if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) {
						t.Error("传输内容被截断")
					}
				}(i)
			}
			wg.Wait()
			elapsed := time.Since(start)
			if elapsed < test.minimum {
				t.Fatalf("限速被并发/请求标识绕过: %v < %v", elapsed, test.minimum)
			}
			t.Logf("两条 64 KiB 下载耗时 %v", elapsed)
		})
	}
}

func TestBandwidthRangeHeadConditionalAndZip(t *testing.T) {
	handler, manager, data, _ := bandwidthHandler(t, config.BandwidthLimits{PerIPDownload: 256 * 1024, TotalDownload: 512 * 1024})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	do := func(method, span, modified string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+"/media.bin", nil)
		if span != "" {
			req.Header.Set("Range", span)
		}
		if modified != "" {
			req.Header.Set("If-Modified-Since", modified)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := do(http.MethodGet, "bytes=7-106", "")
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 206 || !bytes.Equal(body, data[7:107]) || response.Header.Get("Content-Range") != "bytes 7-106/65536" {
		t.Fatal("单段 Range 损坏", err)
	}
	response = do(http.MethodGet, "bytes=0-9,100-109", "")
	kind, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/byteranges" {
		t.Fatal("多段 Range 头错误", err)
	}
	reader := multipart.NewReader(response.Body, params["boundary"])
	for _, expected := range [][]byte{data[:10], data[100:110]} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(part)
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatal("多段 Range 内容错误", err)
		}
	}
	response.Body.Close()
	if err := manager.Update(config.BandwidthLimits{PerIPDownload: 1, TotalDownload: 1}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	response = do(http.MethodHead, "", "")
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || len(body) != 0 || response.ContentLength != int64(len(data)) {
		t.Fatal("HEAD 行为错误", err)
	}
	response = do(http.MethodGet, "", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 304 || len(body) != 0 || time.Since(start) > time.Second {
		t.Fatal("无实体响应被限速", err)
	}
	if err := manager.Update(config.BandwidthLimits{PerIPDownload: 256 * 1024}); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	response, err = client.PostForm(server.URL+"/zip", url.Values{"base": {"/"}, "paths": {"/media.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("ZIP 响应头没有及时刷新")
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("ZIP 响应绕过了下载限速")
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(archive.File) != 1 {
		t.Fatal("ZIP 不完整", err)
	}
	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("ZIP 解压内容错误", err)
	}
}

func TestBandwidthHeadErrorsDoNotWait(t *testing.T) {
	handler, _, _, _ := bandwidthHandler(t, config.BandwidthLimits{PerIPDownload: 1, TotalDownload: 1})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for _, test := range []struct {
		path   string
		span   string
		status int
	}{
		{"/missing.bin", "", http.StatusNotFound},
		{"/media.bin", "bytes=999999-", http.StatusRequestedRangeNotSatisfiable},
		{"/zip", "", http.StatusMethodNotAllowed},
	} {
		t.Run(test.path+test.span, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodHead, server.URL+test.path, nil)
			request.Header.Set("Range", test.span)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal("无实体 HEAD 错误响应进入了限速等待", err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || len(body) != 0 || response.StatusCode != test.status {
				t.Fatal("HEAD 错误响应的协议行为改变", response.StatusCode, err)
			}
		})
	}
}

type countedBody struct {
	io.Reader
	read        atomic.Int64
	cancel      context.CancelFunc
	cancelAfter int64
}

func (body *countedBody) Read(p []byte) (int, error) {
	n, err := body.Reader.Read(p)
	if size := body.read.Add(int64(n)); body.cancel != nil && size >= body.cancelAfter {
		body.cancel()
	}
	return n, err
}
func (*countedBody) Close() error { return nil }

func TestBandwidthUploadBeforeParsingAndHotUpdate(t *testing.T) {
	handler, manager, data, uploads := bandwidthHandler(t, config.BandwidthLimits{PerIPUpload: 1, TotalUpload: 1, TotalDownload: 1})
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, _ := writer.CreateFormFile("file", "uploaded.bin")
	part.Write(data)
	writer.Close()
	body := &countedBody{Reader: bytes.NewReader(form.Bytes())}
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, req.WithContext(ctx)); close(done) }()
	time.Sleep(100 * time.Millisecond)
	if body.read.Load() != 0 {
		t.Fatal("multipart 在获得总上传额度之前读取了请求体")
	}
	if entries, err := os.ReadDir(uploads); err != nil || len(entries) != 0 {
		t.Fatal("上传读取未限速而只限制了落盘", err)
	}
	if err := manager.Update(config.BandwidthLimits{TotalDownload: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("取消上传限速没有唤醒正在解析的请求")
	}
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatal(response.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(uploads, "uploaded.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("上传内容损坏", err)
	}
	// 上传结果 JSON 不应受总下载限制。
	if ctx.Err() != nil {
		t.Fatal("上传结果响应被下载限速")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestBandwidthUploadCancellationAndSizeLimit(t *testing.T) {
	handler, _, _, uploads := bandwidthHandler(t, config.BandwidthLimits{})
	temporary := t.TempDir()
	t.Setenv("TMP", temporary)
	t.Setenv("TEMP", temporary)
	t.Setenv("TMPDIR", temporary)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prefix := "--test-boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"large.bin\"\r\nContent-Type: application/octet-stream\r\n\r\n"
	body := &countedBody{Reader: io.MultiReader(strings.NewReader(prefix), io.LimitReader(zeroReader{}, 34<<20), strings.NewReader("\r\n--test-boundary--\r\n")), cancel: cancel, cancelAfter: (32 << 20) + (64 << 10)}
	req := httptest.NewRequest(http.MethodPost, "/upload", body).WithContext(ctx)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test-boundary")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if ctx.Err() == nil || response.Code != 413 {
		t.Fatal("上传取消未中止解析")
	}
	for _, directory := range []string{temporary, uploads} {
		if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
			t.Fatal("取消后残留上传文件或 multipart 临时文件", entries, err)
		}
	}
	oversized := &countedBody{Reader: strings.NewReader("not read")}
	req = httptest.NewRequest(http.MethodPost, "/upload", oversized)
	req.ContentLength = maxUploadBytes + 1
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 413 || oversized.read.Load() != 0 {
		t.Fatal("已知超大上传未提前拒绝")
	}
}

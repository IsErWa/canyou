package share

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newListingTestHandler 构造带根目录/子目录/文件的共享处理器（聊天关闭、允许上传）
func newListingTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "子目录")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// 固定修改时间保证排序断言稳定：b 比 a 新
	for name, content := range map[string]string{"a.txt": "A", "b.txt": "BB"} {
		target := filepath.Join(root, name)
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
		if name == "b.txt" {
			stamp = stamp.Add(time.Hour)
		}
		if err := os.Chtimes(target, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(root, t.TempDir(), func() bool { return false }, "dev", nil), root
}

func postList(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/list", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeList(t *testing.T, recorder *httptest.ResponseRecorder) listResponse {
	t.Helper()
	if recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("响应不是 JSON: %q", recorder.Header().Get("Content-Type"))
	}
	var result listResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, recorder.Body.String())
	}
	return result
}

// TestListAPIReturnsJSONListing 校验 /api/list 根目录与子目录的字段、排序与 parent
func TestListAPIReturnsJSONListing(t *testing.T) {
	handler, _ := newListingTestHandler(t)

	root := decodeList(t, postList(t, handler, `{"path":"/"}`))
	if !root.OK || root.Path != "/" || root.Parent != "" {
		t.Fatalf("根目录响应异常: %+v", root)
	}
	// 目录优先，其后按修改时间新到旧
	if len(root.Items) != 3 {
		t.Fatalf("根目录条目数错误: %+v", root.Items)
	}
	first := root.Items[0]
	if !first.IsDirectory || first.Name != "子目录" || first.Path != "/子目录" || first.Link != "/子目录/" || first.Icon != "📁" {
		t.Fatalf("目录条目字段错误: %+v", first)
	}
	if b, a := root.Items[1], root.Items[2]; b.Name != "b.txt" || a.Name != "a.txt" {
		t.Fatalf("文件排序错误: %+v", root.Items)
	}
	file := root.Items[1]
	if file.IsDirectory || file.Link != "/b.txt" || file.Size != "2 B" || file.Icon != "📄" || !strings.Contains(file.ModTime, "2026-11-01") {
		t.Fatalf("文件条目字段错误: %+v", file)
	}

	sub := decodeList(t, postList(t, handler, `{"path":"/子目录"}`))
	if !sub.OK || sub.Path != "/子目录" || sub.Parent != "/" {
		t.Fatalf("子目录响应异常: %+v", sub)
	}
	if len(sub.Items) != 0 {
		t.Fatalf("空子目录应返回空列表: %+v", sub.Items)
	}
	if !strings.Contains(postList(t, handler, `{"path":"子目录"}`).Body.String(), `"parent":"/"`) {
		t.Fatal("缺首斜杠的路径应被归一化为根下的子目录")
	}
}

// TestListAPIRejectsBadRequests 校验穿越归一、不存在路径、非法方法与坏请求体
func TestListAPIRejectsBadRequests(t *testing.T) {
	handler, _ := newListingTestHandler(t)

	// 穿越一律被 Clean 限制回 viewDir 内部：/../../ 归一为 /
	traversal := decodeList(t, postList(t, handler, `{"path":"/子目录/../../"}`))
	if !traversal.OK || traversal.Path != "/" {
		t.Fatalf("穿越路径未被限制在共享目录: %+v", traversal)
	}

	if recorder := postList(t, handler, `{"path":"/不存在"}`); recorder.Code != http.StatusNotFound {
		t.Fatalf("不存在目录应 404，实际 %d", recorder.Code)
	} else if result := decodeList(t, recorder); result.OK || result.Message == "" {
		t.Fatalf("404 响应体错误: %+v", result)
	}

	// 文件不是目录，同样 404
	if recorder := postList(t, handler, `{"path":"/a.txt"}`); recorder.Code != http.StatusNotFound {
		t.Fatalf("文件路径应 404，实际 %d", recorder.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/list", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 应 405，实际 %d", recorder.Code)
	}

	if recorder := postList(t, handler, `not-json`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("坏请求体应 400，实际 %d", recorder.Code)
	}
}

// TestIndexShellIsDirectoryAgnostic 任意目录路径返回同一个单页壳，不再内嵌列表行
func TestIndexShellIsDirectoryAgnostic(t *testing.T) {
	handler, root := newListingTestHandler(t)
	if err := os.Mkdir(filepath.Join(root, "mydesktop"), 0o755); err != nil {
		t.Fatal(err)
	}

	fetch := func(target string) string {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Accept", "text/html")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s 应 200，实际 %d", target, recorder.Code)
		}
		return recorder.Body.String()
	}

	home := fetch("/")
	desktop := fetch("/mydesktop/?did=00000a5f9297600000ed86d40c")
	for name, body := range map[string]string{"根页": home, "旧目录页": desktop} {
		for _, marker := range []string{`id="pathTitle"`, `id="fileList"`, `'/api/list'`, `data-base="/"`, `kindOf`, `lightbox`, `lb-media`, `_blank`} {
			if !strings.Contains(body, marker) {
				t.Fatalf("%s 缺少壳标记 %s", name, marker)
			}
		}
		// 列表不再服务端渲染：文件名与 .. 行都不应出现在壳里
		for _, leaked := range []string{"a.txt", "b.txt", "子目录", ">..<"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("%s 不应内嵌目录内容: %s", name, leaked)
			}
		}
		// 聊天关闭不渲染聊天按钮；允许上传则渲染上传按钮
		if strings.Contains(body, `id="chatToggle"`) {
			t.Fatalf("%s 聊天关闭时不应渲染 chatToggle", name)
		}
		if !strings.Contains(body, `id="uploadToggle"`) {
			t.Fatalf("%s 应渲染 uploadToggle", name)
		}
	}
	if home != desktop {
		t.Fatal("同一壳应与请求路径无关")
	}

	// 文件仍由 http.FileServer 走真实路径直接服务
	request := httptest.NewRequest(http.MethodGet, "/a.txt", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "A" {
		t.Fatalf("文件直链行为回归: code=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

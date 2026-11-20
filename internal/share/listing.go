package share

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// listingItem 目录列表接口中的单个条目
type listingItem struct {
	Name        string `json:"name"`
	Link        string `json:"link"`
	Path        string `json:"path"`
	Icon        string `json:"icon"`
	Size        string `json:"size"`
	ModTime     string `json:"modTime"`
	IsDirectory bool   `json:"isDir"`
}

// listResponse /api/list 目录列表响应
type listResponse struct {
	OK      bool          `json:"ok"`
	Path    string        `json:"path,omitempty"`
	Parent  string        `json:"parent,omitempty"`
	Items   []listingItem `json:"items"`
	Message string        `json:"message,omitempty"`
}

// listingEntry 待排序的目录条目：保留 FileInfo 以便按类型与修改时间排序
type listingEntry struct {
	info os.FileInfo
}

// sortListingEntries 目录优先，同组内按修改时间从新到旧，时间相同时按名称升序
func sortListingEntries(entries []listingEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].info, entries[j].info
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		if modDiff := a.ModTime().Compare(b.ModTime()); modDiff != 0 {
			return modDiff > 0
		}
		return a.Name() < b.Name()
	})
}

// buildListing 读取目录并生成排序后的列表条目；reqPath 必须是 path.Clean("/"+p) 归一化后的路径
func buildListing(viewDir, reqPath string) ([]listingItem, error) {
	entries, err := os.ReadDir(filepath.Join(viewDir, filepath.FromSlash(reqPath)))
	if err != nil {
		return nil, err
	}

	var sortable []listingEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // 条目信息不可读（如已被删除）时跳过
		}
		sortable = append(sortable, listingEntry{info: info})
	}
	sortListingEntries(sortable)

	items := make([]listingItem, 0, len(sortable))
	for _, item := range sortable {
		var size string
		modTime := item.info.ModTime().Format("2006-01-02 15:04")
		if !item.info.IsDir() {
			size = humanSize(item.info.Size())
		}
		icon := "📄"
		if item.info.IsDir() {
			icon = "📁"
		}
		items = append(items, listingItem{
			Name: item.info.Name(), Link: joinURL(reqPath, item.info.Name(), item.info.IsDir()),
			Path: joinURL(reqPath, item.info.Name(), false),
			Icon: icon, Size: size, ModTime: modTime, IsDirectory: item.info.IsDir(),
		})
	}
	return items, nil
}

// serveListAPI POST /api/list：以 JSON 返回目录列表，供单页前端动态渲染
func serveListAPI(writer http.ResponseWriter, request *http.Request, viewDir string) {
	if request.Method != http.MethodPost {
		writeListJSON(writer, http.StatusMethodNotAllowed, listResponse{Message: "目录列表仅支持 POST"})
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 1<<16)).Decode(&body); err != nil {
		writeListJSON(writer, http.StatusBadRequest, listResponse{Message: "请求体格式错误"})
		return
	}
	reqPath := path.Clean("/" + body.Path) // 强制根路径 + Clean：穿越一律限制在 viewDir 内
	info, err := os.Stat(filepath.Join(viewDir, filepath.FromSlash(reqPath)))
	if err != nil || !info.IsDir() {
		writeListJSON(writer, http.StatusNotFound, listResponse{Message: "目录不存在"})
		return
	}
	items, err := buildListing(viewDir, reqPath)
	if err != nil {
		writeListJSON(writer, http.StatusInternalServerError, listResponse{Message: "读取目录失败"})
		return
	}
	response := listResponse{OK: true, Path: reqPath, Items: items}
	if reqPath != "/" {
		response.Parent = path.Dir(reqPath)
	}
	writeListJSON(writer, http.StatusOK, response)
}

// writeListJSON 输出目录列表接口的 JSON 响应
func writeListJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		log.Println("共享服务异常:", err)
	}
}

// joinURL 拼接子项访问链接，目录以 / 结尾
func joinURL(base, name string, isDir bool) string {
	link := path.Join(base, name)
	if isDir {
		link += "/"
	}
	return link
}

// humanSize 将字节数格式化为易读单位
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

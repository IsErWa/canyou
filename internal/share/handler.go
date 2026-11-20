package share

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"Canyou/internal/device"
	"Canyou/internal/webui"
)

// isDirectoryPath 判断请求路径映射到 viewDir 下是否为目录
func isDirectoryPath(viewDir, urlPath string) bool {
	name := filepath.FromSlash(path.Clean("/" + urlPath))
	info, err := os.Stat(filepath.Join(viewDir, name))
	return err == nil && info.IsDir()
}

// serveIndexPage 渲染单页壳：不再读盘列目录，列表由前端 POST /api/list 获取
func serveIndexPage(writer http.ResponseWriter, request *http.Request, uploadDir string, canChat bool, version string) {
	data := webui.FilesData{CanUpload: uploadDir != "", CanChat: canChat, ClientIP: device.PeerIP(request), Version: version}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := webui.RenderFiles(writer, data); err != nil {
		log.Println("共享服务异常:", err) // 响应已部分写出，仅记录
	}
}

// NewHandler 处理目录、文件、上传及 ZIP；聊天可用性由组装方查询。
func NewHandler(viewDir, uploadDir string, canChat func() bool, version string) http.Handler {
	fileServer := http.FileServer(http.Dir(viewDir))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/upload":
			handleFileUpload(writer, request, uploadDir)
		case request.URL.Path == "/zip":
			handleZipDownload(writer, request, viewDir)
		case request.URL.Path == "/api/list":
			serveListAPI(writer, request, viewDir)
		case isDirectoryPath(viewDir, request.URL.Path):
			serveIndexPage(writer, request, uploadDir, canChat != nil && canChat(), version)
		default:
			fileServer.ServeHTTP(writer, request)
		}
	})
}

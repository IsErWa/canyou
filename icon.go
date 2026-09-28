package main

import (
	_ "embed"
	"net/http"
	"os"
	"path/filepath"

	"github.com/tadvi/winc"
)

// appIconBytes 通过 go:embed 内嵌应用图标，保证最终产物仍为单文件 EXE
//
//go:embed favicon-128x128.ico
var appIconBytes []byte

// applyWindowIcon 将内嵌图标设置为窗口标题栏小图标与任务栏大图标
func applyWindowIcon(form *winc.Form) {
	// ExtractIcon 只能从磁盘文件读取，故先把内嵌字节落地到临时文件
	iconPath := filepath.Join(os.TempDir(), "canyou_app.ico")
	if err := os.WriteFile(iconPath, appIconBytes, 0o600); err != nil {
		return
	}

	icon, err := winc.ExtractIcon(iconPath, 0)
	if err != nil {
		return
	}
	form.SetIcon(0, icon) // ICON_SMALL：标题栏左上角
	form.SetIcon(1, icon) // ICON_BIG：任务栏与 Alt+Tab
}

// newShareHandler 组装共享站点路由：内嵌 favicon、目录浏览页（含上传按钮）、上传接口，其余回退到静态文件服务
func newShareHandler(viewDir, uploadDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(viewDir))

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/favicon.ico":
			writer.Header().Set("Content-Type", "image/x-icon")
			writer.Header().Set("Cache-Control", "max-age=86400")
			writer.Write(appIconBytes)
		case request.URL.Path == "/upload":
			handleFileUpload(writer, request, uploadDir)
		case isDirectoryPath(viewDir, request.URL.Path):
			serveDirectoryListing(writer, request, viewDir, uploadDir)
		default:
			fileServer.ServeHTTP(writer, request)
		}
	})
}

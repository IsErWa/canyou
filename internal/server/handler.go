package server

import (
	"net/http"

	"Canyou/internal/assets"
	"Canyou/internal/bandwidth"
	"Canyou/internal/chat"
	"Canyou/internal/share"
)

// NewHandler 组装共享站点路由，不依赖原生桌面控件。
func NewHandler(viewDir, uploadDir string, manager *chat.Manager, version string, limits *bandwidth.Manager) http.Handler {
	files := share.NewHandler(viewDir, uploadDir, manager.Enabled, version, limits)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ws/chat":
			manager.ServeWebSocket(writer, request)
		case "/favicon.ico":
			writer.Header().Set("Content-Type", "image/x-icon")
			writer.Header().Set("Cache-Control", "max-age=86400")
			writer.Write(assets.Icon())
		default:
			files.ServeHTTP(writer, request)
		}
	})
}

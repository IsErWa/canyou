package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"syscall"
)

// ErrServerAlreadyRunning 服务已在运行中，禁止重复启动
var ErrServerAlreadyRunning = errors.New("服务已在运行中")

// FileServer 基于 net/http 的静态目录共享服务，支持一键启停与端口确定性释放
type FileServer struct {
	mu       sync.Mutex
	httpSrv  *http.Server
	listener net.Listener
	running  bool
}

// NewFileServer 创建一个处于空闲状态的共享服务实例
func NewFileServer() *FileServer {
	return &FileServer{}
}

// Windows Winsock 监听失败的常见错误码。
// 注：Go 在 Windows 上的 syscall.EADDRINUSE 是占位值（不等于真实 Winsock 码），故显式定义
const (
	wsaAddrInUse syscall.Errno = 10048 // 端口已被其它连接占用
	wsaAccess    syscall.Errno = 10013 // 端口落在系统保留区间或缺少权限
)

// listenError 把监听失败错误翻译成友好中文提示；无法归类的错误返回 false，交由调用方保留原始详情
func listenError(err error, port int) (string, bool) {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case wsaAddrInUse:
			return fmt.Sprintf("端口 %d 已被占用，请更换端口", port), true
		case wsaAccess:
			return fmt.Sprintf("端口 %d 被系统保留或需要管理员权限，请更换端口", port), true
		}
	}
	return "", false
}

// Start 在指定端口启动目录共享服务；viewDir 供浏览下载，uploadDir 接收网页上传；端口被占用时返回友好错误，不进入运行状态
func (server *FileServer) Start(viewDir, uploadDir string, port int) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.running {
		return ErrServerAlreadyRunning
	}

	address := fmt.Sprintf("0.0.0.0:%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		if msg, ok := listenError(err, port); ok {
			return errors.New(msg)
		}
		return fmt.Errorf("启动服务失败: %w", err)
	}

	server.httpSrv = &http.Server{Handler: newShareHandler(viewDir, uploadDir)}
	server.listener = listener
	server.running = true

	// 服务在独立协程运行，避免阻塞 UI 主线程；Close() 触发的 ErrServerClosed 属正常停止
	go func() {
		if err := server.httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logServiceFailure(err)
		}
	}()
	return nil
}

// Stop 关闭服务并释放端口，空闲状态下调用无副作用
func (server *FileServer) Stop() {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.httpSrv == nil {
		return
	}
	server.httpSrv.Close()
	server.httpSrv = nil
	server.listener = nil
	server.running = false
}

// IsRunning 返回服务当前是否处于运行状态
func (server *FileServer) IsRunning() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.running
}

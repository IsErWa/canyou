package webui

import "embed"

// 资源只从编译时的嵌入文件系统读取，运行目录无需携带网页源码。
//
//go:embed templates styles scripts
var resources embed.FS

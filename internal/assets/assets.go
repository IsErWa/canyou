package assets

import _ "embed"

// 应用图标仅在此嵌入；桌面与 HTTP 响应共用同一资源。
//
//go:embed favicon-128x128.ico
var icon []byte

// Icon 返回独立的图标字节，防止调用方修改共享资源。
func Icon() []byte { return append([]byte(nil), icon...) }

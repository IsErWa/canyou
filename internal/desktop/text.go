package desktop

import (
	"Canyou/internal/config"
)

// defaultPort 界面初始端口
const defaultPort = config.DefaultPort

// 界面展示文案集中定义，便于统一调整与后续本地化（按用途分组）
const (
	// 窗口与分组标题
	textWindowTitle = "Canyou"
	textGroupShare  = "设置参数"
	textGroupQR     = "访问地址（需在同一局域网内访问，手机可以直接扫码）"

	// 表单字段标签与输入提示
	labelDirectory    = "查看目录"
	labelUploadDir    = "上传目录"
	labelLocalIP      = "本机IP"
	labelPort         = "端口"
	labelGatePass     = "访问密码"
	textPortRangeHint = "范围 1-65535"
	textAdapterPrefix = "网卡 "
	hintPassOff       = "未设置"
	hintPassOn        = "已启用"

	// 按钮文字
	buttonChooseDir       = "选择目录"
	buttonRefreshIP       = "刷新"
	buttonStart           = "启动服务"
	buttonStop            = "停止服务"
	buttonCopyURL         = "复制地址"
	buttonCopyPass        = "复制"
	buttonOpen            = "浏览器打开"
	buttonApplyPass       = "设置密码"
	buttonBandwidth       = "传输限速"
	buttonSaveBandwidth   = "保存并应用"
	buttonCancelBandwidth = "取消"

	// 状态与访问地址展示
	statusIdle        = "● 未运行"
	statusRunning     = "● 运行中"
	textURLPrefix     = "浏览器打开："
	hintNoDir         = "尚未选择目录"
	hintNoIP          = "未找到局域网IP，连接网络后点击刷新"
	hintQRPlaceholder = "选择IP并输入合法端口后自动生成二维码"
	textQRWaiting     = "等待生成二维码…"

	// 对话框与弹窗文案
	dialogChooseDirTitle  = "选择查看目录"
	dialogChooseUploadDir = "选择上传目录"
	msgPromptTitle        = "提示"
	msgPortInvalidTitle   = "端口无效"
	msgNeedDir            = "请先选择查看目录"
	msgNeedPassCopy       = "请先在密码框输入要复制的密码"
	msgCopiedTitle        = "已复制"

	// 传输限速窗口
	labelBandwidthPerIP    = "单客户端"
	labelBandwidthTotal    = "所有客户端"
	labelBandwidthUpload   = "上传（MB/s）"
	labelBandwidthDownload = "下载（MB/s）"
	textBandwidthHint      = "0 表示不限速；支持小数，最大 10000 MB/s。"
	textBandwidthDirection = "上传：客户端 → 本机；下载：本机 → 客户端。"
	textBandwidthShared    = "同一 IP 的多个连接共享额度，并同时受总量限制。"
	errSaveBandwidth       = "保存限速失败：%w"

	// 错误提示模板（%s / %q 为运行时填充的错误详情）
	errInvalidPort = "端口 %q 非法：仅允许 1-65535 的数字"
	errEmptyPort   = "端口不能为空：请输入 1-65535 的数字"
	errWriteQR     = "写入二维码失败: %s"
	errShowQR      = "显示二维码失败: %s"
	errCopyFailed  = "复制失败: %s"
	errOpenBrowser = "无法打开浏览器: %s"
)

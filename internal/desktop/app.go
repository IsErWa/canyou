package desktop

import (
	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// Run 启动原生窗口；调用顺序与单文件入口保持一致。
func Run(version string) {
	// 以 GUI 子系统编译（build.bat 的 -H=windowsgui），启动时不再分配控制台窗口，
	// 也避免第三方注入组件向控制台输出日志
	// 控件构造时继承 DefaultFont，故必须在创建任何控件前设置
	winc.DefaultFont = winc.NewFont("Microsoft YaHei UI", 9, 0)

	mainWindow := winc.NewForm(nil)
	// 补上 WS_CLIPCHILDREN，避免父窗口擦背景时覆盖子控件（容器窗口的标准做法）
	addWindowStyle(mainWindow.Handle(), w32.WS_CLIPCHILDREN)
	// 固定客户区底色并让标签透明，避免首帧未擦背景时 STATIC 标签露出灰块
	installFormBackdrop(mainWindow)
	mainWindow.SetText(textWindowTitle + " " + version)
	// SetSize 设置的是含标题栏/边框的窗口外框尺寸（本机非客户区约占 39px），
	// 454 使客户区高约 415，完整容纳底边在 y=406 的访问地址分组框并留余量
	mainWindow.SetSize(400, 454)
	applyWindowIcon(mainWindow)
	// 固定窗口尺寸：禁用右下角拖拽缩放边框与最大化按钮
	mainWindow.EnableSizable(false)
	mainWindow.EnableMaxButton(false)

	controller := newController(mainWindow, version)
	controller.buildLayout()
	controller.applyLoadedConfig()
	controller.loadIPAddresses()

	mainWindow.OnClose().Bind(controller.onWindowClose)
	centerOnCursorMonitor(mainWindow)
	mainWindow.Show()
	// Show 后强制重绘一次整块客户区，规避首帧未完全绘制的情况
	repaintWindow(mainWindow.Handle())

	winc.RunMainLoop()
}

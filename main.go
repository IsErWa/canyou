package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// defaultPort 界面初始端口
const defaultPort = "8080"

// Version 程序版本号，构建时经 -ldflags "-X main.Version=..." 注入（如 v1.2.3），本地未注入时默认 dev
var Version = "dev"

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
	buttonChooseDir = "选择目录"
	buttonRefreshIP = "刷新"
	buttonStart     = "启动服务"
	buttonStop      = "停止服务"
	buttonCopyURL   = "复制地址"
	buttonCopyPass  = "复制"
	buttonOpen      = "浏览器打开"
	buttonApplyPass = "设置密码"

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

	// 错误提示模板（%s / %q 为运行时填充的错误详情）
	errInvalidPort = "端口 %q 非法：仅允许 1-65535 的数字"
	errEmptyPort   = "端口不能为空：请输入 1-65535 的数字"
	errWriteQR     = "写入二维码失败: %s"
	errShowQR      = "显示二维码失败: %s"
	errCopyFailed  = "复制失败: %s"
	errOpenBrowser = "无法打开浏览器: %s"
)

// UIController 聚合界面控件与业务状态，所有回调均运行于 UI 主线程
type UIController struct {
	mainWindow  *winc.Form
	fileServer  *FileServer
	selectedDir string
	uploadDir   string
	lanIPs      []LANIP
	config      AppConfig

	boldFont        *winc.Font
	dirEdit         *winc.Edit
	uploadEdit      *winc.Edit
	ipComboBox      *ipComboBox
	refreshIPButton *winc.PushButton
	portEdit        *numericEdit
	shareButton     *winc.PushButton
	copyButton      *winc.PushButton
	openButton      *winc.PushButton
	gatePassEdit    *winc.Edit
	gateHint        *winc.Label
	statusLabel     *statusLabelView
	urlLabel        *winc.Label
	qrImageView     *winc.ImageView
	qrHintLabel     *winc.Label
	currentURL      string
	qrTempFile      string
	fixingPortText  bool
}

// logServiceFailure 记录服务协程的意外运行期错误（启动后极少发生）
func logServiceFailure(err error) {
	log.Println("共享服务异常:", err)
}

func main() {
	// 以 GUI 子系统编译（build.bat 的 -H=windowsgui），启动时不再分配控制台窗口，
	// 也避免第三方注入组件向控制台输出日志
	// 控件构造时继承 DefaultFont，故必须在创建任何控件前设置
	winc.DefaultFont = winc.NewFont("Microsoft YaHei UI", 9, 0)

	mainWindow := winc.NewForm(nil)
	// 补上 WS_CLIPCHILDREN，避免父窗口擦背景时覆盖子控件（容器窗口的标准做法）
	addWindowStyle(mainWindow.Handle(), w32.WS_CLIPCHILDREN)
	// 固定客户区底色并让标签透明，避免首帧未擦背景时 STATIC 标签露出灰块
	installFormBackdrop(mainWindow)
	mainWindow.SetText(textWindowTitle + " " + Version)
	// SetSize 设置的是含标题栏/边框的窗口外框尺寸（本机非客户区约占 39px），
	// 454 使客户区高约 415，完整容纳底边在 y=406 的访问地址分组框并留余量
	mainWindow.SetSize(400, 454)
	applyWindowIcon(mainWindow)
	// 固定窗口尺寸：禁用右下角拖拽缩放边框与最大化按钮
	mainWindow.EnableSizable(false)
	mainWindow.EnableMaxButton(false)

	controller := NewUIController(mainWindow)
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

// addWindowStyle 为窗口追加一个样式位（如 WS_CLIPCHILDREN、WS_CLIPSIBLINGS）
func addWindowStyle(hwnd w32.HWND, style uint32) {
	current := uint32(w32.GetWindowLong(hwnd, w32.GWL_STYLE))
	if current&style == 0 {
		w32.SetWindowLong(hwnd, w32.GWL_STYLE, current|style)
	}
}

// repaintWindow 强制窗口立即重绘整块客户区
func repaintWindow(hwnd w32.HWND) {
	w32.InvalidateRect(hwnd, nil, true)
	w32.UpdateWindow(hwnd)
}

// setBounds 以“父窗口客户区坐标”设置控件位置与大小。
// 不用 winc 的 SetPos/SetSize：它们会把显示器工作区原点(workRect.Left/Top)错误叠加进坐标，
// 在多显示器或工作区原点非(0,0)时把控件整体平移出窗体，导致客户区空白（整片灰）。
func setBounds(ctrl winc.Controller, x, y, width, height int) {
	w32.SetWindowPos(ctrl.Handle(), 0, x, y, width, height, w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
}

// centerOnCursorMonitor 将窗口居中到“鼠标指针所在显示器”的工作区。
// 不用 winc 的 Form.Center()：它按窗口创建时的默认位置取最近显示器，
// 在多显示器下会时主屏时副屏。改为跟随鼠标，符合用户当下关注的那块屏。
func centerOnCursorMonitor(form *winc.Form) {
	hwnd := form.Handle()
	var monitor w32.HMONITOR
	if x, y, ok := w32.GetCursorPos(); ok {
		monitor = w32.MonitorFromPoint(x, y, w32.MONITOR_DEFAULTTONEAREST)
	} else {
		monitor = w32.MonitorFromWindow(hwnd, w32.MONITOR_DEFAULTTONEAREST)
	}

	var info w32.MONITORINFO
	info.CbSize = uint32(unsafe.Sizeof(info))
	if !w32.GetMonitorInfo(monitor, &info) {
		form.Center()
		return
	}

	rect := w32.GetWindowRect(hwnd)
	winWidth := int(rect.Right - rect.Left)
	winHeight := int(rect.Bottom - rect.Top)
	work := info.RcWork
	x := int(work.Left) + (int(work.Right-work.Left)-winWidth)/2
	y := int(work.Top) + (int(work.Bottom-work.Top)-winHeight)/2
	w32.SetWindowPos(hwnd, 0, x, y, 0, 0, w32.SWP_NOSIZE|w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
}

// NewUIController 创建界面控制器
func NewUIController(mainWindow *winc.Form) *UIController {
	return &UIController{
		mainWindow: mainWindow,
		fileServer: NewFileServer(),
		config:     LoadAppConfig(),
		boldFont:   winc.NewFont("Microsoft YaHei UI", 9, winc.FontBold),
		qrTempFile: filepath.Join(os.TempDir(), "canyou_qrcode.png"),
	}
}

// buildLayout 以分组设置面板形式组装界面
func (controller *UIController) buildLayout() {
	controller.newGroup(textGroupShare, 8, 8, 372, 136)
	controller.buildDirectoryRow()
	controller.buildUploadRow()
	controller.buildIPPortRow()
	controller.buildGateRow()

	controller.newGroup(textGroupQR, 8, 152, 372, 254)
	controller.buildQRArea()
}

// newGroup 创建一个带标题的分组框，作为设置面板的一个区块
func (controller *UIController) newGroup(title string, x, y, width, height int) {
	group := winc.NewGroupBox(controller.mainWindow)
	// 分组框重绘时不裁剪同层级控件会盖住子控件，补 WS_CLIPSIBLINGS
	addWindowStyle(group.Handle(), w32.WS_CLIPSIBLINGS)
	// 分组框默认按按钮面色擦背景，与窗体底色不一致，统一接管其背景擦除
	installGroupBackdrop(group)
	group.SetText(title)
	setBounds(group, x, y, width, height)
	group.SetFont(controller.boldFont)
}

// buildDirectoryRow 构建目录选择行：标签、只读路径输入框、浏览按钮
func (controller *UIController) buildDirectoryRow() {
	controller.newFieldLabel(labelDirectory, 12, 34, 48)

	controller.dirEdit = winc.NewEdit(controller.mainWindow)
	controller.dirEdit.SetText(hintNoDir)
	controller.dirEdit.SetReadOnly(true)
	setBounds(controller.dirEdit, 64, 32, 245, 22)

	browseButton := winc.NewPushButton(controller.mainWindow)
	browseButton.SetText(buttonChooseDir)
	setBounds(browseButton, 310, 30, 65, 24)
	browseButton.OnClick().Bind(controller.onChooseDirectory)
}

// buildUploadRow 构建上传目录选择行，结构与查看目录行一致
func (controller *UIController) buildUploadRow() {
	controller.newFieldLabel(labelUploadDir, 12, 60, 48)

	controller.uploadEdit = winc.NewEdit(controller.mainWindow)
	controller.uploadEdit.SetText(hintNoDir)
	controller.uploadEdit.SetReadOnly(true)
	setBounds(controller.uploadEdit, 64, 58, 245, 22)

	browseButton := winc.NewPushButton(controller.mainWindow)
	browseButton.SetText(buttonChooseDir)
	setBounds(browseButton, 310, 56, 65, 24)
	browseButton.OnClick().Bind(controller.onChooseUploadDir)
}

// buildIPPortRow 将本机IP下拉选择与端口输入（含范围提示）合并到同一行
func (controller *UIController) buildIPPortRow() {
	controller.newFieldLabel(labelLocalIP, 12, 86, 48)

	// 下拉框 102px 宽装不下"IP (网卡名)"，自建控件在创建窗口时就带自绘样式，
	// 展开时画两行：第一行 IP、第二行网卡名，收起后仍只显示 IP
	controller.ipComboBox = newIPComboBox(controller.mainWindow, func(index int) (string, string) {
		if index < 0 || index >= len(controller.lanIPs) {
			return "", ""
		}
		return controller.lanIPs[index].IP, controller.lanIPs[index].Adapter
	})
	setBounds(controller.ipComboBox, 64, 84, 102, 500)
	controller.ipComboBox.OnSelectedChange().Bind(controller.onIPChanged)

	controller.refreshIPButton = winc.NewPushButton(controller.mainWindow)
	controller.refreshIPButton.SetText(buttonRefreshIP)
	setBounds(controller.refreshIPButton, 168, 83, 38, 24)
	controller.refreshIPButton.OnClick().Bind(controller.onRefreshIPAddresses)

	controller.newFieldLabel(labelPort, 212, 86, 24)

	controller.portEdit = newNumericEdit(controller.mainWindow)
	controller.portEdit.SetText(controller.config.Port)
	setBounds(controller.portEdit, 240, 84, 48, 22)
	controller.portEdit.OnChange().Bind(controller.onPortChanged)

	controller.newFieldLabel(textPortRangeHint, 292, 86, 80)
}

// buildGateRow 构建访问密码行：标签、明文输入框、生效状态提示、复制与应用按钮；
// 留空并应用即取消密码，已授权设备名单保留
func (controller *UIController) buildGateRow() {
	controller.newFieldLabel(labelGatePass, 12, 112, 48)

	controller.gatePassEdit = winc.NewEdit(controller.mainWindow)
	setBounds(controller.gatePassEdit, 64, 110, 148, 22)

	controller.gateHint = winc.NewLabel(controller.mainWindow)
	setBounds(controller.gateHint, 216, 112, 42, 20)

	copyButton := winc.NewPushButton(controller.mainWindow)
	copyButton.SetText(buttonCopyPass)
	setBounds(copyButton, 262, 108, 32, 24)
	copyButton.OnClick().Bind(controller.onCopyGatePassword)

	applyButton := winc.NewPushButton(controller.mainWindow)
	applyButton.SetText(buttonApplyPass)
	setBounds(applyButton, 310, 108, 65, 24)
	applyButton.OnClick().Bind(controller.onApplyGatePassword)
}

// buildQRArea 构建二维码展示区：左侧为访问地址与二维码图像（含占位提示），
// 右侧竖排复制/打开按钮、运行状态与共享启停按钮；
// 控件均为窗体绝对坐标，分组框因新增访问密码行下移 24px，组内控件必须同步下移
func (controller *UIController) buildQRArea() {
	controller.urlLabel = winc.NewLabel(controller.mainWindow)
	controller.urlLabel.SetText(hintQRPlaceholder)
	setBounds(controller.urlLabel, 16, 176, 270, 20)

	controller.copyButton = winc.NewPushButton(controller.mainWindow)
	controller.copyButton.SetText(buttonCopyURL)
	setBounds(controller.copyButton, 280, 213, 76, 25)
	controller.copyButton.SetEnabled(false)
	controller.copyButton.OnClick().Bind(controller.onCopyAddress)

	controller.openButton = winc.NewPushButton(controller.mainWindow)
	controller.openButton.SetText(buttonOpen)
	setBounds(controller.openButton, 280, 263, 76, 25)
	controller.openButton.SetEnabled(false)
	controller.openButton.OnClick().Bind(controller.onOpenAddress)

	controller.qrImageView = winc.NewImageView(controller.mainWindow)
	setBounds(controller.qrImageView, 25, 200, qrImagePixelSize, qrImagePixelSize)
	controller.qrImageView.Hide()

	controller.qrHintLabel = winc.NewLabel(controller.mainWindow)
	controller.qrHintLabel.SetText(textQRWaiting)
	setBounds(controller.qrHintLabel, 25, 290, 200, 20)

	controller.statusLabel = newStatusLabelView(controller.mainWindow)
	setBounds(controller.statusLabel, 294, 339, 76, 18)
	controller.statusLabel.SetFont(controller.boldFont)
	controller.statusLabel.setStatus(statusIdle, statusColorIdle)

	controller.shareButton = winc.NewPushButton(controller.mainWindow)
	controller.shareButton.SetText(buttonStart)
	setBounds(controller.shareButton, 280, 361, 76, 28)
	controller.shareButton.OnClick().Bind(controller.onToggleService)
}

// newFieldLabel 创建表单行左侧的字段标签
func (controller *UIController) newFieldLabel(text string, x, y, width int) *winc.Label {
	label := winc.NewLabel(controller.mainWindow)
	label.SetText(text)
	setBounds(label, x, y, width, 20)
	return label
}

// onChooseDirectory 唤起系统原生文件夹选择对话框，选中后回显路径
func (controller *UIController) onChooseDirectory(*winc.Event) {
	folder, accepted := winc.ShowBrowseFolderDlg(controller.mainWindow, dialogChooseDirTitle)
	if !accepted {
		return // 用户取消，保留原目录
	}
	controller.selectedDir = folder
	controller.dirEdit.SetText(folder)
	controller.saveConfig()
	controller.applyRunningConfig()
}

// onChooseUploadDir 唤起文件夹选择对话框，选中后回显上传目录并记录配置
func (controller *UIController) onChooseUploadDir(*winc.Event) {
	folder, accepted := winc.ShowBrowseFolderDlg(controller.mainWindow, dialogChooseUploadDir)
	if !accepted {
		return // 用户取消，保留原目录
	}
	controller.uploadDir = folder
	controller.uploadEdit.SetText(folder)
	controller.saveConfig()
	controller.applyRunningConfig()
}

// onIPChanged 网卡 IP 切换后刷新二维码并记录配置
func (controller *UIController) onIPChanged(*winc.Event) {
	controller.refreshQRCode()
	controller.saveConfig()
}

// onRefreshIPAddresses 仅重新扫描地址及更新二维码，不重启共享或消息服务。
func (controller *UIController) onRefreshIPAddresses(*winc.Event) {
	controller.refreshIPButton.SetEnabled(false)
	defer controller.refreshIPButton.SetEnabled(true)
	if controller.loadIPAddresses() {
		controller.saveConfig()
	}
}

// onPortChanged 端口每次输入变化：刷新二维码与按钮，并即时校验端口（未启动与运行中都提示非法）；
// 运行中端口合法则按新端口重启生效，端口被清空/非法则停止服务
func (controller *UIController) onPortChanged(*winc.Event) {
	controller.sanitizePortInput()
	controller.refreshQRCode()
	controller.saveConfig()
	port, ok := controller.validatePortInput()
	if !controller.fileServer.IsRunning() {
		return
	}
	if ok {
		controller.restartWith(port)
	} else {
		controller.stopService()
	}
}

// sanitizePortInput 端口值越界（超过 65535）时去掉末位，避免非法值留在输入框（非数字已由 ES_NUMBER 拦下）
func (controller *UIController) sanitizePortInput() {
	if controller.fixingPortText {
		return
	}
	text := controller.portEdit.Text()
	if value, err := strconv.Atoi(text); err == nil && value > 65535 {
		controller.fixingPortText = true
		controller.portEdit.SetText(text[:len(text)-1])
		controller.fixingPortText = false
		controller.portEdit.moveCaretToEnd()
	}
}

// onToggleService 启停合一按钮：空闲时启动共享，运行时停止共享
func (controller *UIController) onToggleService(*winc.Event) {
	if controller.fileServer.IsRunning() {
		controller.stopService()
		return
	}
	controller.startService()
}

// validatePortInput 仅校验端口输入：非法时弹窗提示，返回端口值与是否合法
func (controller *UIController) validatePortInput() (int, bool) {
	port, err := parseLegalPort(controller.portEdit.Text())
	if err != nil {
		winc.MsgBox(controller.mainWindow, msgPortInvalidTitle, err.Error(), w32.MB_ICONWARNING|w32.MB_OK)
		return 0, false
	}
	return port, true
}

// onApplyGatePassword 把输入框明文哈希后热更新门禁并持久化；
// 明文一并保存供界面回显与复制，应用后保留输入框内容不清空
func (controller *UIController) onApplyGatePassword(*winc.Event) {
	plaintext := controller.gatePassEdit.Text()
	hash := hashAccessPassword(plaintext)
	controller.config.PasswordHash = hash
	controller.config.Password = plaintext
	controller.fileServer.SetAccessPasswordHash(hash)
	controller.saveConfig()
	controller.updateGateHint()
}

// onCopyGatePassword 把密码输入框当前内容复制到系统剪贴板；框为空时提示先输入
func (controller *UIController) onCopyGatePassword(*winc.Event) {
	password := controller.gatePassEdit.Text()
	if password == "" {
		winc.MsgBoxOk(controller.mainWindow, msgPromptTitle, msgNeedPassCopy)
		return
	}
	if err := copyToClipboard(password); err != nil {
		winc.Errorf(controller.mainWindow, errCopyFailed, err)
		return
	}
	winc.MsgBoxOk(controller.mainWindow, msgCopiedTitle, password)
}

// updateGateHint 同步密码行的生效状态提示
func (controller *UIController) updateGateHint() {
	if controller.config.PasswordHash == "" {
		controller.gateHint.SetText(hintPassOff)
	} else {
		controller.gateHint.SetText(hintPassOn)
	}
}

// validateStartParams 校验启动/重启所需参数：共享目录已选 + 端口合法；供手动启动与目录变更重启复用
func (controller *UIController) validateStartParams() (int, bool) {
	if controller.selectedDir == "" {
		winc.MsgBoxOk(controller.mainWindow, msgPromptTitle, msgNeedDir)
		return 0, false
	}
	return controller.validatePortInput()
}

// startService 启动服务：校验目录与端口全部合法后开启共享
func (controller *UIController) startService() {
	port, ok := controller.validateStartParams()
	if !ok {
		return
	}

	if err := controller.fileServer.Start(controller.selectedDir, controller.uploadDir, port); err != nil {
		winc.Errorf(controller.mainWindow, "%s", err)
		return
	}
	controller.applyRunningState(true)
	controller.saveConfig()
}

// stopService 停止服务并释放端口
func (controller *UIController) stopService() {
	controller.fileServer.Stop()
	controller.applyRunningState(false)
}

// applyRunningConfig 运行期间目录变更后，按最新配置重启服务使其立即生效；
// 参数校验与手动启动完全一致，非法时弹窗提示并保留当前运行实例不打断共享
func (controller *UIController) applyRunningConfig() {
	if !controller.fileServer.IsRunning() {
		return
	}
	port, ok := controller.validateStartParams()
	if !ok {
		return
	}
	controller.restartWith(port)
}

// restartWith 按给定端口停止并以最新目录重启服务（假定参数已校验合法）
func (controller *UIController) restartWith(port int) {
	controller.fileServer.Stop()
	if err := controller.fileServer.Start(controller.selectedDir, controller.uploadDir, port); err != nil {
		winc.Errorf(controller.mainWindow, "%s", err)
		controller.applyRunningState(false)
		return
	}
	controller.applyRunningState(true)
}

// onWindowClose 程序关闭前保存配置、销毁服务、释放端口
func (controller *UIController) onWindowClose(*winc.Event) {
	controller.saveConfig()
	controller.fileServer.Stop()
	winc.Exit()
}

// applyRunningState 同步界面到运行/空闲两种状态：切换启停按钮文字与状态标签，并刷新复制/打开按钮可用态
func (controller *UIController) applyRunningState(running bool) {
	controller.updateAccessButtonsEnabled()
	if running {
		controller.shareButton.SetText(buttonStop)
		controller.statusLabel.setStatus(statusRunning, statusColorRunning)
		if controller.currentURL != "" {
			controller.urlLabel.SetText(textURLPrefix + controller.currentURL)
		}
		return
	}
	controller.shareButton.SetText(buttonStart)
	controller.statusLabel.setStatus(statusIdle, statusColorIdle)
}

// loadIPAddresses 扫描网卡并填充下拉框，默认选中第一个有效 IP
func (controller *UIController) loadIPAddresses() bool {
	entries, err := ScanLANIPv4()
	if err != nil {
		winc.Errorf(controller.mainWindow, "%s", err)
		return false
	}
	controller.applyIPAddresses(entries)
	return true
}

// applyIPAddresses 优先保留当前选择；旧地址失效则选新地址，无网卡时清除旧地址与二维码。
// 下拉项文本只放 IP，网卡名由自绘列表单独显示，保证收起后窄下拉框仍能完整显示地址
func (controller *UIController) applyIPAddresses(entries []LANIP) {
	preferredIP := controller.currentIP()
	if preferredIP == "" {
		preferredIP = controller.config.SelectedIP
	}
	selectedIndex := preferredIPIndex(entries, preferredIP)
	controller.ipComboBox.DeleteAllItems()
	controller.lanIPs = entries
	for index, entry := range entries {
		controller.ipComboBox.InsertItem(index, entry.IP)
	}
	controller.ipComboBox.SetSelectedItem(selectedIndex)
	controller.refreshQRCode()
}

// applyLoadedConfig 将上次保存的查看目录、上传目录与访问密码状态回填到界面
func (controller *UIController) applyLoadedConfig() {
	if controller.config.Directory != "" {
		controller.selectedDir = controller.config.Directory
		controller.dirEdit.SetText(controller.config.Directory)
	}
	if controller.config.UploadDirectory != "" {
		controller.uploadDir = controller.config.UploadDirectory
		controller.uploadEdit.SetText(controller.config.UploadDirectory)
	}
	// 密码只存哈希，重启后直接恢复门禁生效状态
	controller.fileServer.SetAccessPasswordHash(controller.config.PasswordHash)
	controller.updateGateHint()
	// 回显明文密码，供查看与复制
	controller.gatePassEdit.SetText(controller.config.Password)
}

// saveConfig 采集当前界面状态并持久化（尽力而为，失败仅记录）
func (controller *UIController) saveConfig() {
	config := AppConfig{
		Directory:       controller.selectedDir,
		UploadDirectory: controller.uploadDir,
		Port:            controller.portEdit.Text(),
		SelectedIP:      controller.currentIP(),
		PasswordHash:    controller.config.PasswordHash,
		Password:        controller.config.Password,
	}
	if err := config.Save(); err != nil {
		log.Println("保存配置失败:", err)
	}
}

// refreshQRCode 依据当前 IP 与端口重绘二维码；条件不满足时显示占位提示
func (controller *UIController) refreshQRCode() {
	accessURL, ok := controller.currentAccessURL()
	if !ok {
		controller.currentURL = ""
		if controller.currentIP() == "" {
			controller.urlLabel.SetText(hintNoIP)
		} else {
			controller.urlLabel.SetText(hintQRPlaceholder)
		}
		controller.showQRResult(false)
		return
	}

	pngBytes, err := GenerateQRPNG(accessURL)
	if err != nil {
		winc.Errorf(controller.mainWindow, "%s", err)
		return
	}

	if err := os.WriteFile(controller.qrTempFile, pngBytes, 0o600); err != nil {
		winc.Errorf(controller.mainWindow, errWriteQR, err)
		return
	}

	if err := controller.qrImageView.DrawImageFile(controller.qrTempFile); err != nil {
		winc.Errorf(controller.mainWindow, errShowQR, err)
		return
	}

	controller.currentURL = accessURL
	controller.urlLabel.SetText(textURLPrefix + accessURL)
	controller.showQRResult(true)
	controller.qrImageView.Invalidate(true)
}

// showQRResult 在二维码图像与占位提示之间切换，并刷新复制/打开按钮可用态
func (controller *UIController) showQRResult(hasQR bool) {
	controller.updateAccessButtonsEnabled()
	if hasQR {
		controller.qrHintLabel.Hide()
		controller.qrImageView.Show()
		return
	}
	controller.qrImageView.Hide()
	controller.qrHintLabel.Show()
}

// updateAccessButtonsEnabled 仅当服务运行中且已生成有效访问地址（有二维码）时，才启用“复制地址/浏览器打开”
func (controller *UIController) updateAccessButtonsEnabled() {
	enabled := controller.fileServer.IsRunning() && controller.currentURL != ""
	controller.copyButton.SetEnabled(enabled)
	controller.openButton.SetEnabled(enabled)
}

// onCopyAddress 将当前访问地址复制到系统剪贴板
func (controller *UIController) onCopyAddress(*winc.Event) {
	if controller.currentURL == "" {
		return
	}
	if err := copyToClipboard(controller.currentURL); err != nil {
		winc.Errorf(controller.mainWindow, errCopyFailed, err)
		return
	}
	winc.MsgBoxOk(controller.mainWindow, msgCopiedTitle, controller.currentURL)
}

// onOpenAddress 用系统默认浏览器打开当前访问地址
func (controller *UIController) onOpenAddress(*winc.Event) {
	if controller.currentURL == "" {
		return
	}
	if err := w32.ShellExecute(controller.mainWindow.Handle(), "open", controller.currentURL, "", "", 1); err != nil {
		winc.Errorf(controller.mainWindow, errOpenBrowser, err)
	}
}

// copyToClipboard 借助系统自带 clip.exe 写入文本；URL 为纯 ASCII 无编码问题
func copyToClipboard(text string) error {
	cmd := exec.Command("clip")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	_, writeErr := io.WriteString(stdin, text)
	closeErr := stdin.Close()
	waitErr := cmd.Wait()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return waitErr
}

// currentIP 返回下拉框当前选中的 IP，未选中时返回空串
func (controller *UIController) currentIP() string {
	index := controller.ipComboBox.SelectedItem()
	if index < 0 || index >= len(controller.lanIPs) {
		return ""
	}
	return controller.lanIPs[index].IP
}

// currentAccessURL 拼接当前访问地址，IP 未选或端口非法时返回 false
func (controller *UIController) currentAccessURL() (string, bool) {
	ip := controller.currentIP()
	port := controller.portEdit.Text()
	if ip == "" || !isLegalPort(port) {
		return "", false
	}
	return buildAccessURL(ip, port), true
}

// buildAccessURL 拼接局域网访问地址 http://IP:端口
func buildAccessURL(ip string, port string) string {
	return fmt.Sprintf("http://%s:%s", ip, port)
}

// isLegalPort 判断端口文本是否为 1-65535 的合法数字
func isLegalPort(portText string) bool {
	_, err := parseLegalPort(portText)
	return err == nil
}

// parseLegalPort 解析端口文本，非数字或超范围返回错误
func parseLegalPort(portText string) (int, error) {
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		if portText == "" {
			return 0, errors.New(errEmptyPort)
		}
		return 0, fmt.Errorf(errInvalidPort, portText)
	}
	return port, nil
}

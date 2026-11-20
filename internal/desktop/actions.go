package desktop

import (
	"strconv"

	"Canyou/internal/config"

	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// onChooseDirectory 唤起系统原生文件夹选择对话框，选中后回显路径
func (controller *controller) onChooseDirectory(*winc.Event) {
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
func (controller *controller) onChooseUploadDir(*winc.Event) {
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
func (controller *controller) onIPChanged(*winc.Event) {
	controller.refreshQRCode()
	controller.saveConfig()
}

// onRefreshIPAddresses 仅重新扫描地址及更新二维码，不重启共享或消息服务。
func (controller *controller) onRefreshIPAddresses(*winc.Event) {
	controller.refreshIPButton.SetEnabled(false)
	defer controller.refreshIPButton.SetEnabled(true)
	if controller.loadIPAddresses() {
		controller.saveConfig()
	}
}

// onPortChanged 端口每次输入变化：刷新二维码与按钮，并即时校验端口（未启动与运行中都提示非法）；
// 运行中端口合法则按新端口重启生效，端口被清空/非法则停止服务
func (controller *controller) onPortChanged(*winc.Event) {
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
func (controller *controller) sanitizePortInput() {
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
func (controller *controller) onToggleService(*winc.Event) {
	if controller.fileServer.IsRunning() {
		controller.stopService()
		return
	}
	controller.startService()
}

// validatePortInput 仅校验端口输入：非法时弹窗提示，返回端口值与是否合法
func (controller *controller) validatePortInput() (int, bool) {
	port, err := parseLegalPort(controller.portEdit.Text())
	if err != nil {
		winc.MsgBox(controller.mainWindow, msgPortInvalidTitle, err.Error(), w32.MB_ICONWARNING|w32.MB_OK)
		return 0, false
	}
	return port, true
}

// onApplyGatePassword 把输入框明文哈希后热更新门禁并持久化；
// 明文一并保存供界面回显与复制，应用后保留输入框内容不清空
func (controller *controller) onApplyGatePassword(*winc.Event) {
	plaintext := controller.gatePassEdit.Text()
	hash := config.HashAccessPassword(plaintext)
	controller.config.PasswordHash = hash
	controller.config.Password = plaintext
	controller.fileServer.SetAccessPasswordHash(hash)
	controller.saveConfig()
	controller.updateGateHint()
}

// onCopyGatePassword 把密码输入框当前内容复制到系统剪贴板；框为空时提示先输入
func (controller *controller) onCopyGatePassword(*winc.Event) {
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

// validateStartParams 校验启动/重启所需参数：共享目录已选 + 端口合法；供手动启动与目录变更重启复用
func (controller *controller) validateStartParams() (int, bool) {
	if controller.selectedDir == "" {
		winc.MsgBoxOk(controller.mainWindow, msgPromptTitle, msgNeedDir)
		return 0, false
	}
	return controller.validatePortInput()
}

// startService 启动服务：校验目录与端口全部合法后开启共享
func (controller *controller) startService() {
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
func (controller *controller) stopService() {
	controller.fileServer.Stop()
	controller.applyRunningState(false)
}

// applyRunningConfig 运行期间目录变更后，按最新配置重启服务使其立即生效；
// 参数校验与手动启动完全一致，非法时弹窗提示并保留当前运行实例不打断共享
func (controller *controller) applyRunningConfig() {
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
func (controller *controller) restartWith(port int) {
	controller.fileServer.Stop()
	if err := controller.fileServer.Start(controller.selectedDir, controller.uploadDir, port); err != nil {
		winc.Errorf(controller.mainWindow, "%s", err)
		controller.applyRunningState(false)
		return
	}
	controller.applyRunningState(true)
}

// onWindowClose 程序关闭前保存配置、销毁服务、释放端口
func (controller *controller) onWindowClose(*winc.Event) {
	controller.saveConfig()
	controller.fileServer.Stop()
	winc.Exit()
}

// onCopyAddress 将当前访问地址复制到系统剪贴板
func (controller *controller) onCopyAddress(*winc.Event) {
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
func (controller *controller) onOpenAddress(*winc.Event) {
	if controller.currentURL == "" {
		return
	}
	if err := w32.ShellExecute(controller.mainWindow.Handle(), "open", controller.currentURL, "", "", 1); err != nil {
		winc.Errorf(controller.mainWindow, errOpenBrowser, err)
	}
}

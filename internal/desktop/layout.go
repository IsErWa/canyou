package desktop

import (
	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// buildLayout 以分组设置面板形式组装界面
func (controller *controller) buildLayout() {
	controller.newGroup(textGroupShare, 8, 8, 372, 136)
	controller.buildDirectoryRow()
	controller.buildUploadRow()
	controller.buildIPPortRow()
	controller.buildGateRow()

	controller.newGroup(textGroupQR, 8, 152, 372, 254)
	controller.buildQRArea()
}

// newGroup 创建一个带标题的分组框，作为设置面板的一个区块
func (controller *controller) newGroup(title string, x, y, width, height int) {
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
func (controller *controller) buildDirectoryRow() {
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
func (controller *controller) buildUploadRow() {
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
func (controller *controller) buildIPPortRow() {
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
func (controller *controller) buildGateRow() {
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
func (controller *controller) buildQRArea() {
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
func (controller *controller) newFieldLabel(text string, x, y, width int) *winc.Label {
	label := winc.NewLabel(controller.mainWindow)
	label.SetText(text)
	setBounds(label, x, y, width, 20)
	return label
}

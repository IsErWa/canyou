package desktop

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"Canyou/internal/config"
	"Canyou/internal/netinfo"
	"Canyou/internal/server"

	"github.com/tadvi/winc"
)

// controller 聚合界面控件与业务状态，所有回调均运行于 UI 主线程
type controller struct {
	mainWindow  *winc.Form
	fileServer  *server.FileServer
	selectedDir string
	uploadDir   string
	lanIPs      []netinfo.LANIP
	config      config.AppConfig

	boldFont        *winc.Font
	dirEdit         *winc.Edit
	uploadEdit      *winc.Edit
	ipComboBox      *ipComboBox
	refreshIPButton *winc.PushButton
	portEdit        *numericEdit
	shareButton     *winc.PushButton
	copyButton      *winc.PushButton
	openButton      *winc.PushButton
	bandwidthButton *winc.PushButton
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

// newController 创建界面控制器
func newController(mainWindow *winc.Form, version string) *controller {
	return &controller{
		mainWindow: mainWindow,
		fileServer: server.NewFileServer(version),
		config:     config.LoadAppConfig(),
		boldFont:   winc.NewFont("Microsoft YaHei UI", 9, winc.FontBold),
		qrTempFile: filepath.Join(os.TempDir(), "canyou_qrcode.png"),
	}
}

// updateGateHint 同步密码行的生效状态提示
func (controller *controller) updateGateHint() {
	if controller.config.PasswordHash == "" {
		controller.gateHint.SetText(hintPassOff)
	} else {
		controller.gateHint.SetText(hintPassOn)
	}
}

// applyRunningState 同步界面到运行/空闲两种状态：切换启停按钮文字与状态标签，并刷新复制/打开按钮可用态
func (controller *controller) applyRunningState(running bool) {
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
func (controller *controller) loadIPAddresses() bool {
	entries, err := netinfo.ScanLANIPv4()
	if err != nil {
		winc.Errorf(controller.mainWindow, "%s", err)
		return false
	}
	controller.applyIPAddresses(entries)
	return true
}

// applyIPAddresses 优先保留当前选择；旧地址失效则选新地址，无网卡时清除旧地址与二维码。
// 下拉项文本只放 IP，网卡名由自绘列表单独显示，保证收起后窄下拉框仍能完整显示地址
func (controller *controller) applyIPAddresses(entries []netinfo.LANIP) {
	preferredIP := controller.currentIP()
	if preferredIP == "" {
		preferredIP = controller.config.SelectedIP
	}
	selectedIndex := netinfo.PreferredIPIndex(entries, preferredIP)
	controller.ipComboBox.DeleteAllItems()
	controller.lanIPs = entries
	for index, entry := range entries {
		controller.ipComboBox.InsertItem(index, entry.IP)
	}
	controller.ipComboBox.SetSelectedItem(selectedIndex)
	controller.refreshQRCode()
}

// applyLoadedConfig 将上次保存的查看目录、上传目录与访问密码状态回填到界面
func (controller *controller) applyLoadedConfig() {
	if controller.config.Directory != "" {
		controller.selectedDir = controller.config.Directory
		controller.dirEdit.SetText(controller.config.Directory)
	}
	if controller.config.UploadDirectory != "" {
		controller.uploadDir = controller.config.UploadDirectory
		controller.uploadEdit.SetText(controller.config.UploadDirectory)
	}
	if err := controller.fileServer.SetBandwidthLimits(controller.config.Bandwidth); err != nil {
		log.Println("恢复限速设置失败:", err)
	}
	// 使用已保存的哈希恢复门禁生效状态
	controller.fileServer.SetAccessPasswordHash(controller.config.PasswordHash)
	controller.updateGateHint()
	// 回显明文密码，供查看与复制
	controller.gatePassEdit.SetText(controller.config.Password)
}

// saveConfig 采集当前界面状态并持久化（尽力而为，失败仅记录）
func (controller *controller) saveConfig() {
	if err := controller.collectConfig().Save(); err != nil {
		log.Println("保存配置失败:", err)
	}
}

// collectConfig 从完整配置复制，再采集界面字段，避免其他操作丢失限速设置。
func (controller *controller) collectConfig() config.AppConfig {
	cfg := controller.config
	cfg.Directory = controller.selectedDir
	cfg.UploadDirectory = controller.uploadDir
	cfg.Port = controller.portEdit.Text()
	cfg.SelectedIP = controller.currentIP()
	return cfg
}

// refreshQRCode 依据当前 IP 与端口重绘二维码；条件不满足时显示占位提示
func (controller *controller) refreshQRCode() {
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
func (controller *controller) showQRResult(hasQR bool) {
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
func (controller *controller) updateAccessButtonsEnabled() {
	enabled := controller.fileServer.IsRunning() && controller.currentURL != ""
	controller.copyButton.SetEnabled(enabled)
	controller.openButton.SetEnabled(enabled)
}

// currentIP 返回下拉框当前选中的 IP，未选中时返回空串
func (controller *controller) currentIP() string {
	index := controller.ipComboBox.SelectedItem()
	if index < 0 || index >= len(controller.lanIPs) {
		return ""
	}
	return controller.lanIPs[index].IP
}

// currentAccessURL 拼接当前访问地址，IP 未选或端口非法时返回 false
func (controller *controller) currentAccessURL() (string, bool) {
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

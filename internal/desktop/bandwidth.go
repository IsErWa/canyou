package desktop

import (
	"fmt"

	"Canyou/internal/config"

	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

type bandwidthDialog struct {
	form         *winc.Dialog
	edits        [4]*winc.Edit
	errorLabel   *winc.Label
	saveButton   *winc.PushButton
	cancelButton *winc.PushButton
	controller   *controller
}

func (controller *controller) onBandwidthSettings(*winc.Event) {
	dialog := controller.newBandwidthDialog()
	dialog.form.Show()
	dialog.edits[0].SetFocus()
}

func (controller *controller) newBandwidthDialog() *bandwidthDialog {
	dialog := &bandwidthDialog{form: winc.NewDialog(controller.mainWindow), controller: controller}
	form := dialog.form
	form.SetText(buttonBandwidth)
	form.SetSize(430, 320)
	form.EnableSizable(false)
	form.EnableMaxButton(false)
	addWindowStyle(form.Handle(), w32.WS_CLIPCHILDREN)
	label := func(text string, x, y, width, height int) *winc.Label {
		field := winc.NewLabel(form)
		field.SetText(text)
		setBounds(field, x, y, width, height)
		return field
	}
	label(labelBandwidthUpload, 120, 18, 130, 20)
	label(labelBandwidthDownload, 270, 18, 130, 20)
	label(labelBandwidthPerIP, 16, 52, 98, 20)
	label(labelBandwidthTotal, 16, 90, 98, 20)
	limits := controller.config.Bandwidth
	rates := []int64{limits.PerIPUpload, limits.PerIPDownload, limits.TotalUpload, limits.TotalDownload}
	for i, rate := range rates {
		edit := winc.NewEdit(form)
		edit.SetText(config.FormatBandwidthRate(rate))
		setBounds(edit, 120+(i%2)*150, 48+(i/2)*38, 130, 24)
		dialog.edits[i] = edit
	}
	label(textBandwidthHint, 16, 126, 392, 20)
	label(textBandwidthDirection, 16, 150, 392, 20)
	label(textBandwidthShared, 16, 174, 392, 20)
	dialog.errorLabel = label("", 16, 199, 392, 40)
	dialog.saveButton = winc.NewPushButton(form)
	dialog.saveButton.SetText(buttonSaveBandwidth)
	setBounds(dialog.saveButton, 210, 245, 105, 27)
	dialog.saveButton.OnClick().Bind(func(*winc.Event) {
		if dialog.apply() == nil {
			form.Close()
		}
	})
	dialog.cancelButton = winc.NewPushButton(form)
	dialog.cancelButton.SetText(buttonCancelBandwidth)
	setBounds(dialog.cancelButton, 328, 245, 72, 27)
	dialog.cancelButton.OnClick().Bind(func(*winc.Event) { form.Close() })
	form.SetButtons(dialog.saveButton, dialog.cancelButton)
	// 使用屏幕坐标将对话框居中于父窗，不调用会叠加工作区原点的 SetPos。
	parent := w32.GetWindowRect(controller.mainWindow.Handle())
	bounds := w32.GetWindowRect(form.Handle())
	x := int(parent.Left + (parent.Right-parent.Left-(bounds.Right-bounds.Left))/2)
	y := int(parent.Top + (parent.Bottom-parent.Top-(bounds.Bottom-bounds.Top))/2)
	w32.SetWindowPos(form.Handle(), 0, x, y, 0, 0, w32.SWP_NOSIZE|w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
	return dialog
}

func (dialog *bandwidthDialog) apply() error {
	var rates [4]int64
	for i, edit := range dialog.edits {
		rate, err := config.ParseBandwidthRate(edit.Text())
		if err != nil {
			dialog.errorLabel.SetText(err.Error())
			edit.SetFocus()
			return err
		}
		rates[i] = rate
	}
	limits := config.BandwidthLimits{PerIPUpload: rates[0], PerIPDownload: rates[1], TotalUpload: rates[2], TotalDownload: rates[3]}
	if err := dialog.controller.applyBandwidth(limits); err != nil {
		dialog.errorLabel.SetText(err.Error())
		return err
	}
	dialog.errorLabel.SetText("")
	return nil
}

// applyBandwidth 先持久化成功再热更新；UI 线程串行执行，不影响已有连接。
func (controller *controller) applyBandwidth(limits config.BandwidthLimits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	cfg := controller.collectConfig()
	cfg.Bandwidth = limits
	if err := cfg.Save(); err != nil {
		return fmt.Errorf(errSaveBandwidth, err)
	}
	if err := controller.fileServer.SetBandwidthLimits(limits); err != nil {
		return err
	}
	controller.config = cfg
	return nil
}

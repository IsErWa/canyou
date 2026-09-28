package main

import (
	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// 运行状态文字颜色：未运行灰色、运行中绿色
var (
	statusColorIdle    = winc.RGB(128, 128, 128)
	statusColorRunning = winc.RGB(0, 160, 0)
)

// statusLabelView 自绘状态文字控件。原生 STATIC 标签无法逐控件设置文字颜色，
// 故自绘 WM_PAINT 以支持“未运行灰 / 运行中绿”两态着色。
// 必须带 WS_CLIPSIBLINGS：否则分组框重绘时不会裁剪掉与本控件重叠的区域，会把文字盖掉
type statusLabelView struct {
	winc.ControlBase
	text  string
	color winc.Color
}

func newStatusLabelView(parent winc.Controller) *statusLabelView {
	view := new(statusLabelView)
	view.InitWindow("winc_StatusLabel", parent, 0, w32.WS_CHILD|w32.WS_VISIBLE|w32.WS_CLIPSIBLINGS)
	winc.RegMsgHandler(view)
	view.SetFont(winc.DefaultFont)
	return view
}

// setStatus 更新文字与颜色并触发重绘
func (view *statusLabelView) setStatus(text string, color winc.Color) {
	view.text = text
	view.color = color
	view.Invalidate(true)
}

func (view *statusLabelView) WndProc(msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case w32.WM_SIZE, w32.WM_SIZING:
		view.Invalidate(true)

	case w32.WM_ERASEBKGND:
		return 1 // 背景在 WM_PAINT 中统一绘制，避免闪烁

	case w32.WM_PAINT:
		canvas := winc.NewCanvasFromHwnd(view.Handle())
		defer canvas.Dispose()
		// ClientRect() 会把客户区矩形误当作屏幕坐标二次转换而错位，故直接用 (0,0) 起点的客户区尺寸
		rect := winc.NewRect(0, 0, view.ClientWidth(), view.ClientHeight())
		canvas.FillRect(rect, winc.DefaultBackgroundBrush)
		canvas.DrawText(view.text, rect, w32.DT_LEFT|w32.DT_VCENTER|w32.DT_SINGLELINE, view.Font(), view.color)
	}
	return w32.DefWindowProc(view.Handle(), msg, wparam, lparam)
}

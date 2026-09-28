package main

import (
	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// numericEdit 仅接受数字输入的原生编辑框。
// ES_NUMBER 必须在创建窗口时指定（EDIT 控件创建后用 SetWindowLong 改样式无效），
// 故自建 EDIT 窗口并带上 ES_NUMBER，由系统编辑控件原生拦掉非数字字符
type numericEdit struct {
	winc.ControlBase
	onChange winc.EventManager
}

func newNumericEdit(parent winc.Controller) *numericEdit {
	edit := new(numericEdit)
	edit.InitControl("EDIT", parent, w32.WS_EX_CLIENTEDGE,
		w32.WS_CHILD|w32.WS_VISIBLE|w32.WS_TABSTOP|w32.ES_LEFT|w32.ES_NUMBER|w32.ES_AUTOHSCROLL)
	winc.RegMsgHandler(edit)
	edit.SetFont(winc.DefaultFont)
	return edit
}

// OnChange 文本变化事件（父窗体反射 WM_COMMAND/EN_CHANGE 时触发）
func (edit *numericEdit) OnChange() *winc.EventManager {
	return &edit.onChange
}

// moveCaretToEnd 将光标移到文本末尾（SetText 会把光标重置到开头，回退非法输入后需归位）
func (edit *numericEdit) moveCaretToEnd() {
	length := uintptr(len(edit.Text()))
	w32.SendMessage(edit.Handle(), w32.EM_SETSEL, length, length)
}

func (edit *numericEdit) WndProc(msg uint32, wparam, lparam uintptr) uintptr {
	if msg == w32.WM_COMMAND && w32.HIWORD(uint32(wparam)) == w32.EN_CHANGE {
		edit.onChange.Fire(winc.NewEvent(edit, nil))
	}
	return w32.DefWindowProc(edit.Handle(), msg, wparam, lparam)
}

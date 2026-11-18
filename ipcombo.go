package main

import (
	"syscall"
	"unsafe"

	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
)

// ipComboItemHeight 自绘下拉项高度，需容纳 IP 与网卡名两行
const ipComboItemHeight = 34

// ipComboAdapterOffset 第二行（网卡名）相对项顶的纵偏移
const ipComboAdapterOffset = 17

// odtComboBox 为 DRAWITEMSTRUCT.CtlType 取值，w32 包未定义
const odtComboBox = 3

// odsSelected 为 DRAWITEMSTRUCT.ItemState 的选中标志
const odsSelected = 0x0001

// odsComboBoxEdit 表示这次要画的是下拉框收起后的选中显示区，而不是列表中的一行
const odsComboBoxEdit = 0x1000

// ipComboTextColorIdle 未选中时的 IP 文字色
const ipComboTextColorIdle = 0x000000

// ipComboAdapterColorIdle 未选中时的网卡名文字色
const ipComboAdapterColorIdle = 0x606060

// ipComboProvider 按下拉项序号返回该项的 IP 与网卡名
var ipComboProvider func(index int) (string, string)

// drawItemStruct 对应 Win32 DRAWITEMSTRUCT。
// w32 包没有这个结构，字段顺序与指针宽度必须与 C 声明保持一致
type drawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	DC         uintptr
	RcItem     w32.RECT
	ItemData   uintptr
}

// ipComboBox 自绘列表项的本机 IP 下拉框。
// CBS_OWNERDRAWFIXED 与 ES_NUMBER 一样必须在创建窗口时指定，事后用 SetWindowLong 改样式无效，
// 故不能复用 winc.NewComboBox。列表项文本仍只存 IP，收起后 102px 的下拉框能完整显示地址，
// 展开时由 paintIPComboItem 画两行，把网卡名单独放在第二行
type ipComboBox struct {
	winc.ControlBase
	onSelectedChange winc.EventManager
}

func newIPComboBox(parent winc.Controller, provider func(index int) (string, string)) *ipComboBox {
	cb := new(ipComboBox)
	cb.InitControl("COMBOBOX", parent, 0,
		w32.WS_CHILD|w32.WS_VISIBLE|w32.WS_TABSTOP|w32.WS_VSCROLL|
			w32.CBS_DROPDOWNLIST|w32.CBS_OWNERDRAWFIXED|w32.CBS_HASSTRINGS)
	winc.RegMsgHandler(cb)
	cb.SetFont(winc.DefaultFont)
	// WM_MEASUREITEM 发给父窗口而父窗口不处理时项高会是 0，这里显式给出两行高度
	w32.SendMessage(cb.Handle(), w32.CB_SETITEMHEIGHT, 0, uintptr(ipComboItemHeight))

	ipComboProvider = provider
	formDrawItemHook = paintIPComboItem
	return cb
}

// DeleteAllItems 清空列表项
func (cb *ipComboBox) DeleteAllItems() bool {
	return w32.SendMessage(cb.Handle(), w32.CB_RESETCONTENT, 0, 0) == w32.TRUE
}

// InsertItem 在指定位置插入一项，项文本只保存 IP
func (cb *ipComboBox) InsertItem(index int, str string) bool {
	lp := uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(str)))
	return w32.SendMessage(cb.Handle(), w32.CB_INSERTSTRING, uintptr(index), lp) != w32.CB_ERR
}

// SelectedItem 返回当前选中项序号，未选中时为负数
func (cb *ipComboBox) SelectedItem() int {
	return int(int32(w32.SendMessage(cb.Handle(), w32.CB_GETCURSEL, 0, 0)))
}

// SetSelectedItem 选中指定项
func (cb *ipComboBox) SetSelectedItem(value int) bool {
	return int(int32(w32.SendMessage(cb.Handle(), w32.CB_SETCURSEL, uintptr(value), 0))) == value
}

// OnSelectedChange 选中项变化事件（父窗体反射 WM_COMMAND/CBN_SELCHANGE 时触发）
func (cb *ipComboBox) OnSelectedChange() *winc.EventManager {
	return &cb.onSelectedChange
}

func (cb *ipComboBox) WndProc(msg uint32, wparam, lparam uintptr) uintptr {
	if msg == w32.WM_COMMAND && w32.HIWORD(uint32(wparam)) == w32.CBN_SELCHANGE {
		cb.onSelectedChange.Fire(winc.NewEvent(cb, nil))
	}
	return w32.DefWindowProc(cb.Handle(), msg, wparam, lparam)
}

// paintIPComboItem 绘制一个 IP 下拉项，返回 true 表示已处理该绘制请求
func paintIPComboItem(lparam uintptr) bool {
	if ipComboProvider == nil {
		return false
	}
	// lparam 是系统传入的 DRAWITEMSTRUCT 指针。经参数变量地址二次解引用取值，
	// 既符合 C 侧的内存布局，也不会触发 vet 对 uintptr→Pointer 直接转换的误用告警
	item := *(**drawItemStruct)(unsafe.Pointer(&lparam))
	if item.CtlType != odtComboBox || item.DC == 0 {
		return false
	}
	ip, adapter := ipComboProvider(int(item.ItemID))
	if ip == "" {
		return false
	}

	dc := w32.HDC(item.DC)
	selected := item.ItemState&odsSelected != 0

	background := formBackdropBrush.GetHBRUSH()
	if selected {
		background = w32.GetSysColorBrush(w32.COLOR_HIGHLIGHT)
	}
	w32.FillRect(dc, &item.RcItem, background)

	hfont := w32.HGDIOBJ(w32.SendMessage(w32.HWND(item.HwndItem), w32.WM_GETFONT, 0, 0))
	if hfont != 0 {
		previousFont := w32.SelectObject(dc, hfont)
		defer w32.SelectObject(dc, previousFont)
	}
	w32.SetBkMode(dc, w32.TRANSPARENT)

	left := int(item.RcItem.Left)
	top := int(item.RcItem.Top)
	right := int(item.RcItem.Right)
	bottom := int(item.RcItem.Bottom)

	format := uint(w32.DT_LEFT | w32.DT_SINGLELINE | w32.DT_VCENTER | w32.DT_NOPREFIX | w32.DT_END_ELLIPSIS)

	// 选中显示区只有单行高度，这里再写两行会把网卡名压在 IP 下面形成残影
	if item.ItemState&odsComboBoxEdit != 0 {
		editRect := w32.RECT{Left: int32(left + 2), Top: int32(top), Right: int32(right - 2), Bottom: int32(bottom)}
		w32.SetTextColor(dc, w32.COLORREF(pickComboColor(selected, ipComboTextColorIdle)))
		w32.DrawText(dc, ip, -1, &editRect, format)
		return true
	}

	ipRect := w32.RECT{Left: int32(left + 2), Top: int32(top), Right: int32(right - 2), Bottom: int32(top + ipComboAdapterOffset)}
	w32.SetTextColor(dc, w32.COLORREF(pickComboColor(selected, ipComboTextColorIdle)))
	w32.DrawText(dc, ip, -1, &ipRect, format)

	adapterRect := w32.RECT{Left: int32(left + 2), Top: int32(top + ipComboAdapterOffset), Right: int32(right - 2), Bottom: int32(bottom)}
	w32.SetTextColor(dc, w32.COLORREF(pickComboColor(selected, ipComboAdapterColorIdle)))
	w32.DrawText(dc, textAdapterPrefix+adapter, -1, &adapterRect, format)

	return true
}

// pickComboColor 选中行统一用系统高亮文字色，未选中行用各自的常规色
func pickComboColor(selected bool, idle uint32) uint32 {
	if selected {
		color, _, _ := procGetSysColor.Call(uintptr(w32.COLOR_HIGHLIGHTTEXT))
		return uint32(color)
	}
	return idle
}

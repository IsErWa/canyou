package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	// qrImagePixelSize 二维码像素尺寸，兼顾界面显示大小与扫码清晰度
	qrImagePixelSize = 200
	// qrQuietZoneModules 自绘静区宽度（模块数）。库默认 4 模块白边偏宽，
	// 收窄到 1 模块既留出扫码所需安全边距，又让图案更饱满
	qrQuietZoneModules = 1
)

// GenerateQRPNG 将访问地址编码为 PNG 字节，纠错等级 Medium 平衡密度与识别率；
// 关闭库内置的 4 模块白边，改为自绘窄静区
func GenerateQRPNG(accessURL string) ([]byte, error) {
	qr, err := qrcode.New(accessURL, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("生成二维码失败: %w", err)
	}
	qr.DisableBorder = true
	modules := qr.Bitmap() // 去白边后的 N×N 模块矩阵，true 表示黑块

	// 将「静区 + 码图 + 静区」共 total 个模块按比例映射到 side×side 像素，
	// 逐像素定位所属模块，保证放大后模块边缘横平竖直
	n := len(modules)
	side := qrImagePixelSize
	quiet := qrQuietZoneModules
	total := n + 2*quiet

	img := image.NewGray(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		my := y*total/side - quiet
		for x := 0; x < side; x++ {
			mx := x*total/side - quiet
			inDarkArea := my >= 0 && my < n && mx >= 0 && mx < n
			if inDarkArea && modules[my][mx] {
				img.SetGray(x, y, color.Gray{Y: 0}) // 黑块
			} else {
				img.SetGray(x, y, color.Gray{Y: 255}) // 白块与静区
			}
		}
	}

	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return nil, fmt.Errorf("编码二维码PNG失败: %w", err)
	}
	return buf.Bytes(), nil
}

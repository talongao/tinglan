//go:build windows

package typography

import (
	"sort"
	"sync"
	"syscall"
	"unsafe"

	"github.com/vivaui/viva/ui"
)

var gdi = syscall.NewLazyDLL("gdi32.dll")
var user = syscall.NewLazyDLL("user32.dll")
var createDC = gdi.NewProc("CreateCompatibleDC")
var deleteDC = gdi.NewProc("DeleteDC")
var deleteObject = gdi.NewProc("DeleteObject")
var selectObject = gdi.NewProc("SelectObject")
var createFont = gdi.NewProc("CreateFontIndirectW")
var createDIB = gdi.NewProc("CreateDIBSection")
var drawText = user.NewProc("DrawTextW")
var fontMu sync.Mutex
var enumeratedFamilies map[string]bool
var enumerateCallback = syscall.NewCallback(func(raw *logFont, metric, kind, data uintptr) uintptr {
	name := syscall.UTF16ToString(raw.Face[:])
	if name != "" && name[0] != '@' && enumeratedFamilies != nil {
		enumeratedFamilies[name] = true
	}
	return 1
})

type logFont struct {
	Height, Width, Escapement, Orientation, Weight                                              int32
	Italic, Underline, StrikeOut, CharSet, OutPrecision, ClipPrecision, Quality, PitchAndFamily byte
	Face                                                                                        [32]uint16
}
type bitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
	Colors                 uint32
}

func DefaultFamily() string { return "Microsoft YaHei UI" }
func Families() []string {
	fontMu.Lock()
	defer fontMu.Unlock()
	dc, _, _ := createDC.Call(0)
	if dc == 0 {
		return nil
	}
	defer deleteDC.Call(dc)
	seen := map[string]bool{}
	enumeratedFamilies = seen
	defer func() { enumeratedFamilies = nil }()
	query := logFont{CharSet: 1}
	gdi.NewProc("EnumFontFamiliesExW").Call(dc, uintptr(unsafe.Pointer(&query)), enumerateCallback, 0, 0)
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
func Render(text, family string, size, weight float32, width, height int, tint ui.Color) ui.Bitmap {
	if width < 1 || height < 1 || width > 4096 || height > 512 {
		return ui.Bitmap{}
	}
	fontMu.Lock()
	defer fontMu.Unlock()
	dc, _, _ := createDC.Call(0)
	if dc == 0 {
		return ui.Bitmap{}
	}
	defer deleteDC.Call(dc)
	info := bitmapInfo{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bitmap, _, _ := createDIB.Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return ui.Bitmap{}
	}
	defer deleteObject.Call(bitmap)
	oldBitmap, _, _ := selectObject.Call(dc, bitmap)
	defer selectObject.Call(dc, oldBitmap)
	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	clear(pixels)
	lf := logFont{Height: -int32(size * 2), Weight: 400, CharSet: 1, Quality: 4}
	if weight >= ui.WeightMedium {
		lf.Weight = 500
	}
	if weight >= ui.WeightSemibold {
		lf.Weight = 600
	}
	if weight >= ui.WeightBold {
		lf.Weight = 700
	}
	name, err := syscall.UTF16FromString(family)
	if err != nil {
		return ui.Bitmap{}
	}
	copy(lf.Face[:31], name)
	font, _, _ := createFont.Call(uintptr(unsafe.Pointer(&lf)))
	if font == 0 {
		return ui.Bitmap{}
	}
	defer deleteObject.Call(font)
	oldFont, _, _ := selectObject.Call(dc, font)
	defer selectObject.Call(dc, oldFont)
	gdi.NewProc("SetBkMode").Call(dc, 1)
	gdi.NewProc("SetTextColor").Call(dc, 0xffffff)
	value, err := syscall.UTF16FromString(text)
	if err != nil {
		return ui.Bitmap{}
	}
	bounds := [4]int32{0, 0, int32(width), int32(height)}
	// White antialiased glyphs on black become a premultiplied-alpha mask.
	drawText.Call(dc, uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)-1), uintptr(unsafe.Pointer(&bounds)), 0x20|0x4|0x800|0x8000)
	gdi.NewProc("GdiFlush").Call()
	pix := make([]byte, len(pixels))
	for i := 0; i < len(pix); i += 4 {
		alpha := float32(pixels[i]) * tint.Alpha
		pix[i] = byte(alpha * tint.Blue)
		pix[i+1] = byte(alpha * tint.Green)
		pix[i+2] = byte(alpha * tint.Red)
		pix[i+3] = byte(alpha)
	}
	return ui.Bitmap{Pix: pix, Width: width, Height: height, Stride: width * 4, Version: 1}
}

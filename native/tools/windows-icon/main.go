// windows-icon writes a PNG-backed ICO without browser or image-tool dependencies.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	input, err := os.Open(os.Args[1])
	must(err)
	source, err := png.Decode(input)
	must(err)
	must(input.Close())
	icon := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.CatmullRom.Scale(icon, icon.Bounds(), source, source.Bounds(), draw.Over, nil)
	var pixels bytes.Buffer
	must(png.Encode(&pixels, icon))
	var ico bytes.Buffer
	for _, n := range []uint16{0, 1, 1} {
		must(binary.Write(&ico, binary.LittleEndian, n))
	}
	ico.Write([]byte{0, 0, 0, 0})
	for _, n := range []uint16{1, 32} {
		must(binary.Write(&ico, binary.LittleEndian, n))
	}
	for _, n := range []uint32{uint32(pixels.Len()), 22} {
		must(binary.Write(&ico, binary.LittleEndian, n))
	}
	ico.Write(pixels.Bytes())
	must(os.WriteFile(os.Args[2], ico.Bytes(), 0644))
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}

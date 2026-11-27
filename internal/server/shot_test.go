package server

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func TestClipboardBitmap(t *testing.T) {
	img := image.NewRGBA(image.Rect(3, 5, 5, 7))
	img.SetRGBA(3, 5, color.RGBA{R: 255, A: 255})
	img.SetRGBA(4, 6, color.RGBA{B: 255, A: 255})
	dib := clipboardBitmap(img)
	if len(dib) != 56 || binary.LittleEndian.Uint32(dib[4:]) != 2 ||
		binary.LittleEndian.Uint32(dib[8:]) != 2 || binary.LittleEndian.Uint16(dib[14:]) != 32 {
		t.Fatal("invalid DIB header")
	}
	want := []byte{0, 0, 0, 0, 255, 0, 0, 0, 0, 0, 255, 0, 0, 0, 0, 0}
	if !bytes.Equal(dib[40:], want) {
		t.Fatalf("unexpected bottom-up BGRX pixels: %v", dib[40:])
	}
}

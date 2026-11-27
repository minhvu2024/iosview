package server

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/png"
	"io"
	"iosview/internal/device"
	"net/http"
)

func (s *Server) shot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// Same-origin UI only: browsers always send Origin on cross-site POSTs.
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	d := s.M.Get(r.URL.Query().Get("d"))
	if d == nil {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil {
		http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
		return
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil || int64(cfg.Width)*int64(cfg.Height) > 16<<20 {
		http.Error(w, "invalid PNG size", http.StatusBadRequest)
		return
	}
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		http.Error(w, "invalid PNG", http.StatusBadRequest)
		return
	}
	if err = device.WriteClipboardDIB(clipboardBitmap(img)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CF_DIB with a 40-byte BITMAPINFOHEADER and bottom-up BGRX scanlines.
func clipboardBitmap(img image.Image) []byte {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	stride := width * 4
	data := make([]byte, 40+stride*height)
	binary.LittleEndian.PutUint32(data[0:], 40)
	binary.LittleEndian.PutUint32(data[4:], uint32(width))
	binary.LittleEndian.PutUint32(data[8:], uint32(height))
	binary.LittleEndian.PutUint16(data[12:], 1)
	binary.LittleEndian.PutUint16(data[14:], 32)
	binary.LittleEndian.PutUint32(data[20:], uint32(stride*height))
	line := image.NewRGBA(image.Rect(0, 0, width, 1))
	for y := 0; y < height; y++ {
		draw.Draw(line, line.Bounds(), img, image.Pt(bounds.Min.X, bounds.Min.Y+y), draw.Src)
		row := data[40+(height-1-y)*stride:]
		for x := 0; x < stride; x += 4 {
			row[x], row[x+1], row[x+2] = line.Pix[x+2], line.Pix[x+1], line.Pix[x]
		}
	}
	return data
}

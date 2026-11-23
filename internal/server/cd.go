package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// cdVideo relays a CoreDevice phone's HEVC stream (iosview-cd sidecar) to the
// UI in the same 12-byte-header framing as the TrollVNC H.264 stream, so the
// browser side stays one viewer:
//
//	first message: flags bit3 (config) · payload = {"codec","description"} JSON
//	then frames:   flags bit0 key, bit2 reset (rebuild the decoder) · payload =
//	               4-byte-length-prefixed NAL units (hvcC framing)
//
// Width/height/rotation are 0: the decoder reports the size, and the sidecar
// stream is always the phone's own orientation. Browser→phone 'K' asks for a
// keyframe; bitrate/profile commands do not apply (the phone's encoder decides).
var cdHTTP = &http.Client{Transport: &http.Transport{DisableCompression: true, Proxy: nil}}

func (s *Server) cdVideo(ctx context.Context, ws *websocket.Conn, base string) error {
	// Ends when the UI goes away (the reader below fails), which also aborts
	// the stream request even if the phone sends nothing for a while.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cfg, err := cdCodec(ctx, base)
	if err != nil {
		return err
	}
	if err := ws.Write(ctx, websocket.MessageBinary, frame(cfg, 8)); err != nil {
		return nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/stream.bin", nil)
	resp, err := cdHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("iosview-cd: luồng hình chưa sẵn sàng")
	}

	go func() { // UI → phone: only keyframe requests mean something here
		defer cancel()
		var last time.Time
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if len(data) == 1 && data[0] == 'K' && time.Since(last) > 700*time.Millisecond {
				last = time.Now()
				if r, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/pli", nil); err == nil {
					if resp, err := cdHTTP.Do(r); err == nil {
						resp.Body.Close()
					}
				}
			}
		}
	}()

	br := bufio.NewReaderSize(resp.Body, 256*1024)
	var buf []byte
	for {
		var hdr [5]byte // u32 length (type + data) · u8 type
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return nil
		}
		n := int(binary.BigEndian.Uint32(hdr[:4]))
		if n < 1 || n > 16<<20 {
			return errors.New("iosview-cd: khung hình lỗi")
		}
		size := n - 1
		if cap(buf) < 12+size {
			buf = make([]byte, 12+size+size/2)
		}
		msg := buf[:12+size]
		if _, err := io.ReadFull(br, msg[12:]); err != nil {
			return nil
		}
		var flags byte
		switch hdr[4] {
		case 0:
			flags = 1
		case 2:
			flags = 1 | 4
		}
		putHeader(msg, size, flags)
		if err := ws.Write(ctx, websocket.MessageBinary, msg); err != nil {
			return nil
		}
	}
}

// cdCodec fetches the decoder setup; the sidecar answers 503 while the
// phone's encoder is still starting.
func cdCodec(ctx context.Context, base string) ([]byte, error) {
	var last error
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/codec", nil)
		resp, err := cdHTTP.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			resp.Body.Close()
			var v struct {
				Codec       string `json:"codec"`
				Description string `json:"description"`
			}
			if resp.StatusCode == http.StatusOK && json.Unmarshal(body, &v) == nil && v.Codec != "" {
				return body, nil
			}
			last = errors.New("iosview-cd: bộ mã hoá chưa sẵn sàng")
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, last
}

func putHeader(msg []byte, size int, flags byte) {
	binary.BigEndian.PutUint32(msg[0:4], uint32(size))
	binary.BigEndian.PutUint32(msg[4:8], 0) // width, height
	msg[8], msg[9], msg[10], msg[11] = flags, 0, 0, 0
}

func frame(payload []byte, flags byte) []byte {
	msg := make([]byte, 12+len(payload))
	putHeader(msg, len(payload), flags)
	copy(msg[12:], payload)
	return msg
}

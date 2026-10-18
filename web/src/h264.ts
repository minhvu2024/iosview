// Hardware H.264 viewer: the phone (patched TrollVNC) encodes with its video
// engine, Chrome/Edge decodes on the GPU via WebCodecs. Exposes the tiny part
// of noVNC's RFB API that Tile uses (disconnect, qualityLevel).
//
// Each WebSocket message = one encoded frame:
//   u32 len · u16 width · u16 height · u8 flags(bit0 key) · u8 rotation · u16 0 · Annex-B data

// Quality slider (0..9) → target bitrate in kbps.
const KBPS = [300, 500, 700, 900, 1200, 1600, 2000, 2800, 3600, 4500];

export function h264Supported(): boolean {
  return window.isSecureContext && typeof (window as any).VideoDecoder === "function";
}

// "avc1.PPCCLL" from the SPS inside an Annex-B keyframe.
function codecFromSPS(d: Uint8Array): string {
  for (let i = 0; i + 4 < d.length; i++) {
    if (d[i] === 0 && d[i + 1] === 0 && d[i + 2] === 1 && (d[i + 3] & 0x1f) === 7 && i + 6 < d.length) {
      const hx = (b: number) => b.toString(16).padStart(2, "0");
      return "avc1." + hx(d[i + 4]) + hx(d[i + 5]) + hx(d[i + 6]);
    }
  }
  return "";
}

export class H264View {
  readonly canvas: HTMLCanvasElement;
  private ctx: CanvasRenderingContext2D;
  private ws: WebSocket;
  private dec: VideoDecoder | null = null;
  private codec = "";
  private needKey = true;
  private seq = 0;
  private q: number;
  gotFrame = false;
  closed = false;

  onframe = () => {};
  onbytes = (_n: number) => {};
  onfirst = () => {};
  onclose = (_gotFrame: boolean) => {};

  constructor(container: HTMLElement, url: string, quality: number) {
    this.q = quality;
    this.canvas = document.createElement("canvas");
    this.canvas.className = "h264";
    container.appendChild(this.canvas);
    this.ctx = this.canvas.getContext("2d", { alpha: false, desynchronized: true })!;
    this.ws = new WebSocket(url, ["binary"]);
    this.ws.binaryType = "arraybuffer";
    this.ws.onopen = () => this.sendBitrate();
    this.ws.onmessage = (e) => this.onMessage(e.data as ArrayBuffer);
    this.ws.onclose = () => this.finish();
    this.ws.onerror = () => this.finish();
  }

  get qualityLevel() { return this.q; }
  set qualityLevel(q: number) {
    if (q === this.q) return;
    this.q = q;
    this.sendBitrate();
  }

  disconnect() {
    try { this.ws.close(); } catch { /* ignore */ }
    this.finish();
  }

  private sendBitrate() {
    if (this.ws.readyState !== WebSocket.OPEN) return;
    const kbps = KBPS[Math.max(0, Math.min(9, this.q))];
    const b = new Uint8Array(5);
    b[0] = 0x42; // 'B'
    new DataView(b.buffer).setUint32(1, kbps);
    this.ws.send(b);
  }

  private requestKey() {
    this.needKey = true;
    if (this.ws.readyState === WebSocket.OPEN) this.ws.send(new Uint8Array([0x4b])); // 'K'
  }

  private onMessage(buf: ArrayBuffer) {
    if (this.closed || buf.byteLength < 12) return;
    this.onbytes(buf.byteLength);
    const v = new DataView(buf);
    const n = v.getUint32(0);
    const key = (v.getUint8(8) & 1) === 1;
    const rot = v.getUint8(9) & 3;
    const data = new Uint8Array(buf, 12, Math.min(n, buf.byteLength - 12));
    if (this.needKey && !key) return;

    if (key) {
      const codec = codecFromSPS(data) || this.codec || "avc1.64001f";
      if (!this.dec || this.dec.state === "closed" || codec !== this.codec) this.newDecoder(codec);
      this.needKey = false;
    }
    const dec = this.dec;
    if (!dec || dec.state !== "configured") return;
    // Decoder falling behind (should not happen with GPU decode): skip to a fresh keyframe.
    if (!key && dec.decodeQueueSize > 10) { this.requestKey(); return; }
    try {
      // Rotation rides in the timestamp's low bits so it stays paired with its frame.
      dec.decode(new EncodedVideoChunk({ type: key ? "key" : "delta", timestamp: this.seq++ * 4 + rot, data }));
    } catch (e) {
      console.warn("H264 decode:", (e as Error)?.message || e);
      this.dec = null;
      this.requestKey();
    }
  }

  private newDecoder(codec: string) {
    try { this.dec?.close(); } catch { /* ignore */ }
    this.codec = codec;
    const dec = new VideoDecoder({
      output: (f) => this.draw(f),
      error: (e) => { console.warn("H264 decoder:", e?.message || e); if (this.dec === dec) { this.dec = null; this.requestKey(); } },
    });
    // "no-preference": Chrome/Edge still pick the GPU decoder when there is one,
    // but keep working (software) on machines without it.
    dec.configure({ codec, optimizeForLatency: true, hardwareAcceleration: "no-preference" });
    this.dec = dec;
  }

  private draw(f: VideoFrame) {
    if (this.closed) { f.close(); return; }
    const rot = f.timestamp % 4;
    const w = f.displayWidth, h = f.displayHeight;
    const cw = rot % 2 ? h : w, ch = rot % 2 ? w : h;
    const c = this.canvas;
    if (c.width !== cw || c.height !== ch) { c.width = cw; c.height = ch; }
    const x = this.ctx;
    // rotation = quarter turns clockwise (same convention as TrollVNC's vImage rotate)
    if (rot === 0) x.setTransform(1, 0, 0, 1, 0, 0);
    else if (rot === 1) x.setTransform(0, 1, -1, 0, h, 0);
    else if (rot === 2) x.setTransform(-1, 0, 0, -1, w, h);
    else x.setTransform(0, -1, 1, 0, 0, w);
    x.drawImage(f, 0, 0);
    f.close();
    this.onframe();
    if (!this.gotFrame) { this.gotFrame = true; this.onfirst(); }
  }

  private finish() {
    if (this.closed) return;
    this.closed = true;
    try { this.dec?.close(); } catch { /* ignore */ }
    this.dec = null;
    this.canvas.remove();
    this.onclose(this.gotFrame);
  }
}

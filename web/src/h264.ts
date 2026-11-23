// Hardware H.264 viewer: the phone (patched TrollVNC) encodes with its video
// engine, Chrome/Edge decodes on the GPU via WebCodecs. Exposes the tiny part
// of noVNC's RFB API that Tile uses (disconnect, qualityLevel).
//
// Each WebSocket message = one encoded frame:
//   u32 len · u16 width · u16 height · u8 flags(bit0 key) · u8 rotation · u16 0 · Annex-B data
//
// Commands to the phone: 'K' keyframe · 'B'+u32 kbps · 'P'+fps|0x80+pct|0x80
// (frame rate and encoded size; TrollVNC 3.3-1004+, older builds ignore it).
//
// CoreDevice phones (iOS 17+ without jailbreak) send HEVC in the same framing:
// first a config message (flags bit3) with {"codec","description"(base64 hvcC)},
// then frames of length-prefixed NAL units; flags bit2 = rebuild the decoder.

/** Frame rate (5..60) and encoded size in % of the phone screen (25..100). */
export type Profile = { fps: number; pct: number };

// Quality slider (0..9) → target bitrate in kbps.
const KBPS = [300, 500, 700, 900, 1200, 1600, 2000, 2800, 3600, 4500];
// Decoder this many chunks behind: skip deltas and resync on a fresh keyframe
// (bounds the extra lag instead of a long backlog). Not lower: every resync
// costs the phone a large keyframe, which is what made 0.10.9 stutter.
const MAX_BACKLOG = 10;
// Keyframe requests ('K') at most this often; re-sent while still waiting.
const KEY_RETRY_MS = 1000;

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
  private hevc: { codec: string; description: Uint8Array } | null = null;
  private needKey = true;
  private seq = 0;
  private q: number;
  private cfg: VideoDecoderConfig | null = null;
  private lastKeyReq = -1e9;
  private prof: Profile;
  private encW = 0;       // size of the stream the decoder is set up for
  private encH = 0;
  private next: VideoFrame | null = null;   // newest decoded frame, drawn on the next display refresh
  private raf = 0;
  private isPaused = false;
  dropped = 0;            // frames skipped to catch up (stats)
  lagMs = 0;              // receive → drawn, smoothed (stats): PC-side decode + pacing delay
  private recvAt = new Map<number, number>();   // chunk timestamp → performance.now() at receive
  gotFrame = false;
  closed = false;

  onframe = () => {};
  onbytes = (_n: number) => {};
  onfirst = () => {};
  onclose = (_gotFrame: boolean) => {};

  constructor(container: HTMLElement, url: string, quality: number, profile: Profile) {
    this.q = quality;
    this.prof = profile;
    this.canvas = document.createElement("canvas");
    this.canvas.className = "h264";
    container.appendChild(this.canvas);
    this.ctx = this.canvas.getContext("2d", { alpha: false, desynchronized: true })!;
    this.ws = new WebSocket(url, ["binary"]);
    this.ws.binaryType = "arraybuffer";
    this.ws.onopen = () => { this.sendBitrate(); this.sendProfile(); };
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

  get profile() { return this.prof; }
  set profile(p: Profile) {
    if (p.fps === this.prof.fps && p.pct === this.prof.pct) return;
    this.prof = p;
    this.sendProfile();
  }

  // Paused = hidden behind another view: keep decoding (the stream stays in
  // sync) but only keep the newest frame; it is drawn when shown again.
  get paused() { return this.isPaused; }
  set paused(p: boolean) {
    if (p === this.isPaused) return;
    this.isPaused = p;
    if (!p) this.schedule();
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

  private sendProfile() {
    if (this.ws.readyState !== WebSocket.OPEN) return;
    const fps = Math.max(5, Math.min(60, Math.round(this.prof.fps)));
    const pct = Math.max(25, Math.min(100, Math.round(this.prof.pct)));
    this.ws.send(new Uint8Array([0x50, 0x80 | fps, 0x80 | pct])); // 'P'
  }

  private requestKey() {
    this.needKey = true;
    const now = performance.now();
    if (now - this.lastKeyReq < KEY_RETRY_MS) return;
    this.lastKeyReq = now;
    if (this.ws.readyState === WebSocket.OPEN) this.ws.send(new Uint8Array([0x4b])); // 'K'
  }

  private onMessage(buf: ArrayBuffer) {
    if (this.closed || buf.byteLength < 12) return;
    this.onbytes(buf.byteLength);
    const v = new DataView(buf);
    const n = v.getUint32(0);
    const flags = v.getUint8(8);
    if (flags & 8) { this.setHEVC(new Uint8Array(buf, 12, Math.min(n, buf.byteLength - 12))); return; }
    const key = (flags & 1) === 1;
    const w = v.getUint16(4), h = v.getUint16(6);
    const rot = v.getUint8(9) & 3;
    const data = new Uint8Array(buf, 12, Math.min(n, buf.byteLength - 12));
    // Waiting for a keyframe: skip deltas (re-asks if the keyframe is late).
    if (this.needKey && !key) { this.dropped++; this.requestKey(); return; }

    if (key) {
      const codec = this.hevc ? this.hevc.codec : codecFromSPS(data) || this.codec || "avc1.64001f";
      // New codec or new size (profile changed on the phone), or the stream
      // says its references were lost (flags bit2): fresh decoder.
      if (!this.dec || this.dec.state === "closed" || codec !== this.codec || w !== this.encW || h !== this.encH || (flags & 4)) {
        this.newDecoder(codec);
        this.encW = w; this.encH = h;
      }
      // Old frames still queued: drop them so this keyframe shows right away.
      else if (this.dec.decodeQueueSize > 1 && this.cfg) { this.dec.reset(); this.dec.configure(this.cfg); }
      this.needKey = false;
    }
    const dec = this.dec;
    if (!dec || dec.state !== "configured") return;
    // Decoder falling behind: skip to a fresh keyframe.
    if (!key && dec.decodeQueueSize > MAX_BACKLOG) { this.dropped++; this.requestKey(); return; }
    try {
      // Rotation rides in the timestamp's low bits so it stays paired with its frame.
      const ts = this.seq++ * 4 + rot;
      if (this.recvAt.size > 64) this.recvAt.clear();   // decoder reset / frames never output
      this.recvAt.set(ts, performance.now());
      dec.decode(new EncodedVideoChunk({ type: key ? "key" : "delta", timestamp: ts, data }));
    } catch (e) {
      console.warn("H264 decode:", (e as Error)?.message || e);
      this.dec = null;
      this.requestKey();
    }
  }

  // CoreDevice stream setup: HEVC with out-of-band parameter sets (hvcC).
  private setHEVC(json: Uint8Array) {
    try {
      const c = JSON.parse(new TextDecoder().decode(json)) as { codec: string; description: string };
      this.hevc = { codec: c.codec, description: Uint8Array.from(atob(c.description), (ch) => ch.charCodeAt(0)) };
      this.codec = "";      // force a fresh decoder on the next keyframe
      this.needKey = true;
    } catch (e) {
      console.warn("HEVC config:", (e as Error)?.message || e);
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
    this.cfg = { codec, optimizeForLatency: true, hardwareAcceleration: "no-preference" };
    if (this.hevc) this.cfg.description = this.hevc.description;
    dec.configure(this.cfg);
    this.dec = dec;
  }

  // Frame pacing: keep only the newest decoded frame and draw it on the next
  // display refresh, so frames that arrive in a burst don't show unevenly
  // (and the ones that would never be seen are not drawn at all).
  private draw(f: VideoFrame) {
    if (this.closed) { f.close(); return; }
    this.next?.close();
    this.next = f;
    this.schedule();
  }

  private schedule() {
    if (this.raf || this.isPaused || !this.next) return;
    this.raf = requestAnimationFrame(() => {
      this.raf = 0;
      const f = this.next;
      this.next = null;
      if (f && !this.closed) this.render(f); else f?.close();
    });
  }

  get queue() { return this.dec?.decodeQueueSize ?? 0; }

  private render(f: VideoFrame) {
    const t0 = this.recvAt.get(f.timestamp);
    if (t0 !== undefined) { this.recvAt.delete(f.timestamp); this.lagMs += (performance.now() - t0 - this.lagMs) * 0.1; }
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
    cancelAnimationFrame(this.raf);
    this.raf = 0;
    this.next?.close();
    this.next = null;
    this.canvas.remove();
    this.onclose(this.gotFrame);
  }
}

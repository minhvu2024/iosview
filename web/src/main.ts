// iOSView UI — grid of live iPhone screens (noVNC, view-only) with our own
// input layer. All input goes to the Go agent over one WebSocket; the agent
// fans it out to the sync group with per-device low-latency connections.
import RFB from "@novnc/novnc";
import { H264View, h264Supported } from "./h264";

type Dev = {
  id: string; name: string; vncName: string; transport: "usb" | "wifi"; addr: string;
  status: "connecting" | "online" | "offline" | "error"; error?: string;
  width: number; height: number; inSync: boolean;
  udid?: string; wifi?: string;   // wifi = LAN address of the same phone (fallback path)
};
type State = { devices: Dev[]; syncOn: boolean; usbError?: string; password: string; quality: number; focusQuality: number; h264: boolean; vncPort: number; stripAccents: boolean };

const $ = <T extends HTMLElement = HTMLElement>(s: string) => document.querySelector(s) as T;
const grid = $("#grid");
let state: State = { devices: [], syncOn: true, password: "", quality: 5, focusQuality: 7, h264: true, vncPort: 5901, stripAccents: true };
// Quality for grid tiles hidden behind the focus overlay: still streaming (so
// they come back instantly) but as cheap as possible for phone, Wi-Fi and PC.
const BACKGROUND_QUALITY = 1;
let pageHidden = false;       // app window minimised / in background
const lsGet = (k: string) => { try { return localStorage.getItem(k) || ""; } catch { return ""; } };
const lsSet = (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* ignore */ } };
let showStats = lsGet("iosview.stats") === "1";
let activeId = "";            // last device the user touched / hovered
let hoverId = "";

// ---------------------------------------------------------------- control WS
let ws: WebSocket | null = null;
const outbox: string[] = [];
function send(m: object) {
  const s = JSON.stringify(m);
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(s);
  else if (outbox.length < 50) outbox.push(s);
}
function connectControl() {
  ws = new WebSocket(`ws://${location.host}/ws/control`);
  ws.onopen = () => { while (outbox.length) ws!.send(outbox.shift()!); };
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.t === "state") applyState(m.state);
    else if (m.t === "toast") toast(m.s);
    else if (m.t === "clip") onPhoneClipboard(m.d, m.s);
    else if (m.t === "scan") {
      $("#scanBtn").removeAttribute("disabled");
      toast(m.found?.length ? `Tìm thấy ${m.found.length} máy: ${m.found.join(", ")}` : "Không tìm thấy TrollVNC nào trong LAN");
    }
  };
  ws.onclose = () => { setTimeout(connectControl, 1000); };
}

// ---------------------------------------------------------------- tiles
// Per-phone navigation buttons (Back = left-edge swipe, Home, App switcher).
const ICONS = {
  back: `<svg viewBox="0 0 24 24"><path d="M15 5l-7 7 7 7"/></svg>`,
  home: `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="6.5"/></svg>`,
  switcher: `<svg viewBox="0 0 24 24"><rect x="4" y="6" width="9" height="12" rx="2"/><path d="M16 7.5h2a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2"/></svg>`,
};
const NAV_BUTTONS =
  `<button data-nav="back" title="Quay lại (vuốt từ mép trái)">${ICONS.back}</button>` +
  `<button data-nav="home" title="Home">${ICONS.home}</button>` +
  `<button data-nav="switcher" title="Đa nhiệm">${ICONS.switcher}</button>`;

function bindNav(bar: HTMLElement, id: () => string, onUse?: () => void) {
  bar.querySelectorAll<HTMLElement>("[data-nav]").forEach((b) => {
    b.onclick = (e) => {
      e.stopPropagation();
      const d = id();
      if (!d) return;
      onUse?.();
      send({ t: "btn", d, b: b.dataset.nav });
      const t = tiles.get(d);
      if (t) ripple(echoTargets(t), b.dataset.nav === "back" ? 0.03 : 0.5, b.dataset.nav === "back" ? 0.45 : 0.96, "btn");
    };
  });
}

class Tile {
  dev: Dev;
  el: HTMLElement;
  host: HTMLElement;      // moves between grid and focus stage
  view: HTMLElement;      // noVNC target
  overlay: HTMLElement;   // input capture layer
  msg: HTMLElement;
  stat: HTMLElement;
  rfb: RFB | H264View | null = null;   // current viewer (H.264 or VNC/JPEG)
  h264Failed = false;     // phone has no H.264 stream (old TrollVNC) → use VNC
  firstTimer = 0;
  frames = 0;             // frames drawn since last stats tick
  bytes = 0;              // bytes received since last stats tick
  fps = 0;
  bps = 0;
  visible = true;
  wantQuality = state.quality;
  retry = 0;
  retryTimer = 0;
  pw = "";

  constructor(dev: Dev) {
    this.dev = dev;
    this.el = document.createElement("section");
    this.el.className = "tile";
    this.el.innerHTML = `
      <div class="tile-head">
        <label class="chk" title="Thuộc nhóm đồng bộ"><input type="checkbox"><span></span></label>
        <span class="dot"></span>
        <span class="name" title="Nhấp đúp để đổi tên"></span>
        <span class="badge"></span>
        <span class="res"></span>
        <button class="t-focus" title="Phóng to (hoặc nhấp đúp vào tiêu đề)">⤢</button>
        <button class="t-link" title="Máy Wi‑Fi này cũng đang cắm cáp USB? Gộp thành 1 máy" hidden>⛓</button>
        <button class="t-more" title="Xoá máy Wi‑Fi">✕</button>
      </div>
      <div class="host"><div class="view"></div><div class="overlay"></div><div class="msg"></div><div class="stat" hidden></div></div>
      <div class="navbar">${NAV_BUTTONS}</div>`;
    this.host = this.el.querySelector(".host")!;
    this.view = this.el.querySelector(".view")!;
    this.overlay = this.el.querySelector(".overlay")!;
    this.msg = this.el.querySelector(".msg")!;
    this.stat = this.el.querySelector(".stat")!;
    const chk = this.el.querySelector<HTMLInputElement>(".chk input")!;
    chk.onchange = () => send({ t: "group", ids: [this.dev.id], on: chk.checked });
    const nameEl = this.el.querySelector<HTMLElement>(".name")!;
    nameEl.ondblclick = (e) => { e.stopPropagation(); this.rename(); };
    this.el.querySelector<HTMLElement>(".tile-head")!.ondblclick = () => openFocus(this);
    this.el.querySelector<HTMLElement>(".t-focus")!.onclick = () => openFocus(this);
    this.el.querySelector<HTMLElement>(".t-more")!.onclick = () => {
      if (confirmRemove(this.dev)) send({ t: "remove", d: this.dev.id });
    };
    this.el.querySelector<HTMLElement>(".t-link")!.onclick = () => linkToUSB(this.dev);
    bindInput(this);
    bindNav(this.el.querySelector(".navbar")!, () => this.dev.id, () => setActive(this));
    this.update(dev);
  }

  label() { return this.dev.name || this.dev.vncName || shortId(this.dev); }

  rename() {
    const nameEl = this.el.querySelector<HTMLElement>(".name")!;
    const inp = document.createElement("input");
    inp.className = "rename";
    inp.value = this.dev.name || this.dev.vncName || "";
    nameEl.replaceWith(inp);
    inp.focus(); inp.select();
    const done = (ok: boolean) => {
      inp.replaceWith(nameEl);
      if (ok) send({ t: "rename", d: this.dev.id, name: inp.value.trim() });
    };
    inp.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") done(true); if (e.key === "Escape") done(false); };
    inp.onblur = () => done(true);
  }

  update(dev: Dev) {
    const was = this.dev.status;
    const oldTransport = this.dev.transport;
    this.dev = dev;
    if (dev.transport !== oldTransport && this.rfb) this.disconnect(); // cable plugged / unplugged: reconnect on the new path
    const q = (s: string) => this.el.querySelector<HTMLElement>(s)!;
    q(".name").textContent = this.label();
    q(".badge").textContent = dev.transport === "usb" ? (dev.wifi ? "USB+Wi‑Fi" : "USB") : "Wi‑Fi";
    q(".badge").title = dev.wifi ? `Ưu tiên cáp USB; Wi‑Fi ${dev.wifi} dự phòng` : "";
    q(".badge").dataset.t = dev.transport;
    q(".res").textContent = dev.width ? `${dev.width}×${dev.height}` : "";
    q(".dot").dataset.s = dev.status;
    q(".t-more").hidden = !dev.wifi;
    q(".t-more").title = dev.udid ? "Bỏ kết nối Wi‑Fi (giữ máy USB)" : "Xoá máy Wi‑Fi";
    q(".t-link").hidden = !!dev.udid || dev.transport !== "wifi" || !usbCandidates().length;
    this.el.querySelector<HTMLInputElement>(".chk input")!.checked = dev.inSync;
    this.el.classList.toggle("in-sync", dev.inSync && state.syncOn);
    this.el.classList.toggle("off", dev.status !== "online");
    if (dev.width && dev.height) this.host.style.setProperty("--ar", `${dev.width} / ${dev.height}`);
    if (dev.status !== "online") {
      this.msg.textContent = dev.status === "connecting" ? "Đang kết nối…" : (dev.error || "Mất kết nối");
    }
    if (dev.status === "online" && was !== "online") { this.retry = 0; this.h264Failed = false; }
    if (this.pw !== state.password && this.rfb) { this.disconnect(); }
    this.sync();
  }

  // connect / disconnect the viewer depending on device status + visibility
  sync() {
    const want = this.dev.status === "online" && !pageHidden && (this.visible || focusTile === this);
    if (want && !this.rfb && !this.retryTimer) this.connect();
    if (!want && this.rfb) this.disconnect();
  }

  connect() {
    this.pw = state.password;
    if (state.h264 && !this.h264Failed && h264Supported()) this.connectH264();
    else this.connectVNC();
  }

  // Hardware H.264 stream. If no frame arrives quickly (phone runs a TrollVNC
  // without the iOSView H.264 patch), fall back to VNC for this device.
  connectH264() {
    const v = new H264View(this.view, `ws://${location.host}/ws/h264/${encodeURIComponent(this.dev.id)}`, this.wantQuality);
    v.onframe = () => { if (this.rfb === v) this.frames++; };
    v.onbytes = (n) => { if (this.rfb === v) this.bytes += n; };
    v.onfirst = () => { clearTimeout(this.firstTimer); this.el.classList.add("live"); this.retry = 0; };
    v.onclose = (got) => {
      clearTimeout(this.firstTimer);
      this.el.classList.remove("live");
      if (this.rfb !== v) return;
      this.rfb = null;
      if (!got) this.h264Failed = true;
      if (this.dev.status === "online") {
        const wait = got ? Math.min(500 * 2 ** this.retry++, 5000) : 0;
        this.retryTimer = window.setTimeout(() => { this.retryTimer = 0; this.sync(); }, wait);
      }
    };
    clearTimeout(this.firstTimer);
    this.firstTimer = window.setTimeout(() => { if (this.rfb === v && !v.gotFrame) v.disconnect(); }, 3500);
    this.rfb = v;
  }

  connectVNC() {
    this.pw = state.password;
    const url = `ws://${location.host}/ws/vnc/${encodeURIComponent(this.dev.id)}`;
    const rfb = new RFB(this.view, url, { shared: true, credentials: { password: state.password }, wsProtocols: ["binary"] });
    rfb.viewOnly = true;
    rfb.scaleViewport = true;
    rfb.resizeSession = false;
    rfb.focusOnClick = false;
    rfb.showDotCursor = false;
    rfb.background = "transparent";
    rfb.qualityLevel = this.wantQuality;
    rfb.compressionLevel = 1;   // USB/LAN have bandwidth to spare: spend less phone CPU on zlib
    this.meter(rfb);
    rfb.addEventListener("connect", () => { this.el.classList.add("live"); this.retry = 0; });
    rfb.addEventListener("credentialsrequired", () => {
      if (state.password) rfb.sendCredentials({ password: state.password });
      else { this.msg.textContent = "Cần mật khẩu — mở ⚙ Cài đặt"; }
    });
    rfb.addEventListener("securityfailure", () => { this.msg.textContent = "Sai mật khẩu VNC"; });
    rfb.addEventListener("disconnect", () => {
      this.el.classList.remove("live");
      if (this.rfb === rfb) this.rfb = null;
      if (this.dev.status === "online") {
        const wait = Math.min(500 * 2 ** this.retry++, 5000);
        this.retryTimer = window.setTimeout(() => { this.retryTimer = 0; this.sync(); }, wait);
      }
    });
    this.rfb = rfb;
  }

  // Count drawn frames and received bytes (noVNC internals, best effort —
  // if a future noVNC renames them, stats just stay at 0).
  meter(rfb: RFB) {
    const r = rfb as any;
    try {
      const d = r._display;
      const flip = d.flip.bind(d);
      d.flip = (...a: any[]) => { if (this.rfb === rfb) this.frames++; return flip(...a); };
    } catch { /* ignore */ }
    try {
      r._sock._websocket.addEventListener("message", (e: MessageEvent) => {
        if (this.rfb === rfb) this.bytes += (e.data as ArrayBuffer).byteLength || 0;
      });
    } catch { /* ignore */ }
  }

  tickStats(dt: number) {
    this.fps = this.frames / dt; this.bps = this.bytes / dt;
    this.frames = 0; this.bytes = 0;
    this.stat.hidden = !showStats || !this.rfb;
    if (!this.stat.hidden) this.stat.textContent = `${Math.round(this.fps)} fps · ${fmtRate(this.bps)} · ${this.rfb instanceof H264View ? "H.264" : "JPEG"} q${this.wantQuality}`;
  }

  disconnect() {
    const r = this.rfb; this.rfb = null;
    this.el.classList.remove("live");
    if (r) try { r.disconnect(); } catch { /* already closed */ }
  }

  setQuality(q: number) {
    this.wantQuality = q;
    if (this.rfb) this.rfb.qualityLevel = q;
  }

  canvas(): HTMLCanvasElement | null { return this.view.querySelector("canvas"); }

  destroy() {
    clearTimeout(this.retryTimer);
    this.disconnect();
    this.el.remove();
  }
}

const tiles = new Map<string, Tile>();
const io = new IntersectionObserver((entries) => {
  for (const e of entries) {
    const t = [...tiles.values()].find((t) => t.el === e.target);
    if (!t) continue;
    t.visible = e.isIntersecting;
    // pause off-screen viewers after a grace period to save CPU/bandwidth
    if (!t.visible) setTimeout(() => t.sync(), 3000); else t.sync();
  }
}, { rootMargin: "200px" });

function desiredQuality(t: Tile) {
  if (!focusTile) return state.quality;
  return focusTile === t ? state.focusQuality : Math.min(BACKGROUND_QUALITY, state.quality);
}
function applyQualities() { for (const t of tiles.values()) t.setQuality(desiredQuality(t)); }

function fmtRate(bps: number) {
  if (bps >= 1e6) return (bps / 1e6).toFixed(1) + " MB/s";
  return Math.round(bps / 1e3) + " KB/s";
}

// Once a second: per-tile fps / bandwidth + total in the toolbar.
let lastTick = performance.now();
setInterval(() => {
  const now = performance.now(), dt = (now - lastTick) / 1000;
  lastTick = now;
  let total = 0;
  for (const t of tiles.values()) { t.tickStats(dt); total += t.bps; }
  const el = $("#netTotal");
  el.hidden = !showStats;
  if (showStats) el.textContent = "↓ " + fmtRate(total);
}, 1000);

// App window minimised / hidden: stop all viewers after a short grace period
// (phones stop encoding, PC stops decoding). Control + sync keep working.
let hideTimer = 0;
document.addEventListener("visibilitychange", () => {
  clearTimeout(hideTimer);
  if (document.hidden) {
    hideTimer = window.setTimeout(() => { pageHidden = true; for (const t of tiles.values()) t.sync(); }, 3000);
  } else if (pageHidden) {
    pageHidden = false;
    for (const t of tiles.values()) t.sync();
  }
});

function shortId(d: Dev) {
  return d.udid ? "iPhone " + d.udid.slice(-6) : d.addr;
}

// Cable-connected phones that have no Wi-Fi address attached yet.
function usbCandidates(): Dev[] {
  return state.devices.filter((d) => d.udid && !d.wifi);
}

// Manual fallback when the phone cannot tell its identity (old TrollVNC build).
function linkToUSB(w: Dev) {
  const c = usbCandidates();
  if (!c.length) return;
  const list = c.map((d, i) => `${i + 1}. ${d.name || d.vncName || shortId(d)}`).join("\n");
  const n = parseInt(window.prompt(`Máy Wi‑Fi ${w.addr} chính là máy USB nào?\n${list}`, "1") || "", 10);
  if (n >= 1 && n <= c.length) send({ t: "link", d: w.id, s: c[n - 1].id });
}

function applyState(s: State) {
  const pwChanged = s.password !== state.password || s.h264 !== state.h264;
  state = s;
  const seen = new Set<string>();
  for (const d of s.devices) {
    seen.add(d.id);
    let t = tiles.get(d.id);
    if (!t) {
      t = new Tile(d);
      tiles.set(d.id, t);
      grid.appendChild(t.el);
      io.observe(t.el);
    } else t.update(d);
    t.setQuality(desiredQuality(t));
    if (pwChanged) { t.h264Failed = false; t.disconnect(); t.sync(); }
  }
  for (const [id, t] of tiles) if (!seen.has(id)) { io.unobserve(t.el); t.destroy(); tiles.delete(id); }

  document.body.classList.toggle("sync-on", s.syncOn);
  $("#syncLabel").textContent = s.syncOn ? "Đồng bộ: BẬT" : "Đồng bộ: TẮT";
  const online = s.devices.filter((d) => d.status === "online").length;
  const inGroup = s.devices.filter((d) => d.inSync && d.status === "online").length;
  $("#counter").innerHTML = `<b>${online}</b> / ${s.devices.length} online` + (s.syncOn ? ` · <span class="acc">${inGroup} đồng bộ</span>` : "");
  $("#empty").hidden = s.devices.length > 0;
  const warn = $("#usbWarn");
  warn.hidden = !s.usbError;
  warn.textContent = s.usbError ? "USB: " + s.usbError : "";
}

function confirmRemove(d: Dev) {
  return window.confirm(d.udid ? `Bỏ kết nối Wi‑Fi ${d.wifi} của máy này? (máy USB vẫn giữ)` : `Xoá ${d.name || d.addr} khỏi danh sách?`);
}

// ---------------------------------------------------------------- input
// Normalised (0..1) position of a pointer event on the device screen.
function norm(t: Tile, e: { clientX: number; clientY: number }) {
  const c = t.canvas();
  if (!c) return null;
  const r = c.getBoundingClientRect();
  if (!r.width || !r.height) return null;
  return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height, inside: e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom };
}

// Tiles that will receive an action started on tile `t` (for visual echo).
function echoTargets(t: Tile): Tile[] {
  if (state.syncOn && t.dev.inSync) return [...tiles.values()].filter((x) => x.dev.inSync && x.dev.status === "online");
  return [t];
}

function ripple(targets: Tile[], x: number, y: number, kind = "") {
  for (const t of targets) {
    const c = t.canvas();
    if (!c) continue;
    const hr = t.host.getBoundingClientRect(), cr = c.getBoundingClientRect();
    const dot = document.createElement("i");
    dot.className = "ripple " + kind;
    dot.style.left = `${cr.left - hr.left + x * cr.width}px`;
    dot.style.top = `${cr.top - hr.top + y * cr.height}px`;
    t.host.appendChild(dot);
    setTimeout(() => dot.remove(), 450);
  }
}

function bindInput(t: Tile) {
  const ov = t.overlay;
  let mask = 0;
  let lastWheel = 0;
  let pending: { x: number; y: number } | null = null;
  let raf = 0;
  const id = () => t.dev.id;
  const flushMove = () => {
    raf = 0;
    if (pending && mask) send({ t: "p", d: id(), x: +pending.x.toFixed(4), y: +pending.y.toFixed(4), m: mask });
    pending = null;
  };

  ov.addEventListener("contextmenu", (e) => e.preventDefault());
  ov.addEventListener("pointerenter", () => { hoverId = id(); });
  ov.addEventListener("pointerleave", () => { if (hoverId === id()) hoverId = ""; });
  ov.addEventListener("pointerdown", (e) => {
    const p = norm(t, e);
    if (!p || !p.inside) return;
    e.preventDefault();
    setActive(t);
    ov.setPointerCapture(e.pointerId);
    const bit = e.button === 0 ? 1 : e.button === 1 ? 2 : e.button === 2 ? 4 : 0;
    if (!bit) return;
    mask |= bit;
    send({ t: "p", d: id(), x: +p.x.toFixed(4), y: +p.y.toFixed(4), m: mask });
    ripple(echoTargets(t), p.x, p.y, bit === 1 ? "" : "btn");
  });
  ov.addEventListener("pointermove", (e) => {
    if (!mask) return;
    const p = norm(t, e);
    if (!p) return;
    pending = { x: Math.min(1, Math.max(0, p.x)), y: Math.min(1, Math.max(0, p.y)) };
    // send at display rate; the agent coalesces further if a phone lags
    if (!raf) raf = requestAnimationFrame(flushMove);
  });
  const up = (e: PointerEvent) => {
    if (!mask) return;
    if (raf) { cancelAnimationFrame(raf); flushMove(); }
    const p = norm(t, e) || { x: 0.5, y: 0.5 };
    const bit = e.button === 0 ? 1 : e.button === 1 ? 2 : e.button === 2 ? 4 : 0;
    mask &= ~bit;
    if (e.type !== "pointerup") mask = 0;
    send({ t: "p", d: id(), x: +Math.min(1, Math.max(0, p.x)).toFixed(4), y: +Math.min(1, Math.max(0, p.y)).toFixed(4), m: mask });
  };
  ov.addEventListener("pointerup", up);
  ov.addEventListener("pointercancel", up);
  ov.addEventListener("lostpointercapture", (e) => { if (mask) up(e as PointerEvent); });
  ov.addEventListener("wheel", (e) => {
    e.preventDefault();
    const now = performance.now();
    if (now - lastWheel < 35) return;
    lastWheel = now;
    const p = norm(t, e);
    if (!p) return;
    setActive(t);
    const bit = e.deltaY < 0 ? 8 : 16;
    send({ t: "p", d: id(), x: +p.x.toFixed(4), y: +p.y.toFixed(4), m: bit | mask });
    send({ t: "p", d: id(), x: +p.x.toFixed(4), y: +p.y.toFixed(4), m: mask });
  }, { passive: false });
}

function setActive(t: Tile) {
  if (activeId === t.dev.id) return;
  tiles.get(activeId)?.el.classList.remove("active");
  activeId = t.dev.id;
  t.el.classList.add("active");
}

// Keyboard → focused/hovered device (and its sync group).
function keyTarget() { return focusTile?.dev.id || hoverId || activeId; }

// Special (non-text) keys forwarded as a single down+up — never held, so a
// modifier can never get stuck on the phone.
const SPECIAL: Record<string, number> = {
  Enter: 0xff0d, NumpadEnter: 0xff0d, Backspace: 0xff08, Tab: 0xff09,
  Delete: 0xffff, Escape: 0xff1b, ArrowLeft: 0xff51, ArrowUp: 0xff52,
  ArrowRight: 0xff53, ArrowDown: 0xff54, Home: 0xff50, End: 0xff57,
  PageUp: 0xff55, PageDown: 0xff56,
};
function tapKey(d: string, ks: number) {
  send({ t: "k", d, k: ks, down: true });
  send({ t: "k", d, k: ks, down: false });
}
function isTyping(e: Event) {
  const el = e.target as HTMLElement;
  return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
}
let copyWait: { d: string; until: number } | null = null;

// Windows shortcuts → iOS ⌘ shortcuts. Ctrl+V is handled by the paste event
// (so we get the PC clipboard text without a permission prompt).
function sendCommand(d: string, letter: string) {
  send({ t: "btn", d, b: "cmd:" + letter });
}

window.addEventListener("keydown", (e) => {
  if (e.key === "F2") { e.preventDefault(); send({ t: "sync", on: !state.syncOn }); return; }
  if (e.key === "Escape" && focusTile) { closeFocus(); return; }
  if (isTyping(e) || (document.querySelector("dialog[open]"))) return;
  // Never forward bare modifier keys to the phone (that left Ctrl/Shift stuck
  // "down" on the device and corrupted the next paste).
  if (["Control", "Shift", "Alt", "Meta", "CapsLock", "ContextMenu"].includes(e.key)) return;
  const d = keyTarget();
  if (!d) return;
  const kt = tiles.get(d);
  if (kt) setActive(kt);
  if ((e.ctrlKey || e.metaKey) && !e.altKey) {
    const k = e.key.toLowerCase();
    if (k === "v") return;              // let the browser fire "paste"
    e.preventDefault();
    if (e.repeat) return;
    if (k === "a") { send({ t: "btn", d, b: "selectall" }); toast("Đang chọn hết văn bản trên máy…"); return; }
    if (k === "backspace" || k === "delete") { send({ t: "btn", d, b: "clear" }); toast("Đang xoá nội dung ô nhập…"); return; }
    if (["c", "x", "z", "y"].includes(k)) {
      sendCommand(d, k);
      if (k === "c" || k === "x") {
        copyWait = { d, until: performance.now() + 4000 };
        toast("Đang copy từ iPhone…");
      }
    }
    return;                            // swallow all other Ctrl/⌘ combos
  }
  if (SPECIAL[e.code] !== undefined || SPECIAL[e.key] !== undefined) {
    e.preventDefault();
    tapKey(d, SPECIAL[e.code] ?? SPECIAL[e.key]);
    return;
  }
  if (e.key.length === 1) {
    // Printable character: type it (handles uppercase/symbols via the agent).
    e.preventDefault();
    send({ t: "text", d, s: e.key });
  }
});

// Ctrl+V while pointing at a phone: paste the PC clipboard into it (and into
// the whole sync group when sync is on).
window.addEventListener("paste", (e) => {
  if (isTyping(e) || document.querySelector("dialog[open]")) return;
  const d = keyTarget();
  if (!d) { toast("Trỏ chuột vào màn hình iPhone rồi bấm Ctrl+V"); return; }
  const text = e.clipboardData?.getData("text/plain") || "";
  e.preventDefault();
  const pt = tiles.get(d);
  if (pt) setActive(pt);
  pasteText(d, text);
});

function pasteText(d: string, text: string) {
  if (!text) { toast("Clipboard của PC đang trống (chỉ hỗ trợ văn bản)"); return; }
  send({ t: "text", d, s: text });
  // ASCII is typed straight in; text with dấu tiếng Việt / emoji can't be typed
  // on an iOS HID keyboard, so it goes via clipboard and needs a tap on Paste.
  if (/[^\x00-\x7f]/.test(text)) {
    toast(state.stripAccents
      ? "Đang gõ (đã bỏ dấu tiếng Việt)…"
      : "Văn bản có dấu đã nạp vào clipboard máy — chạm “Paste” trên iPhone để dán");
  } else toast("Đang gõ vào máy…");
}

// Phone clipboard → PC clipboard (only from the phone you are working on).
async function onPhoneClipboard(d: string, text: string) {
  const fromCopy = copyWait && copyWait.d === d && performance.now() < copyWait.until;
  if (!fromCopy && d !== keyTarget()) return;
  if (fromCopy) copyWait = null;
  const name = tiles.get(d)?.label() || "iPhone";
  const ok = await writePCClipboard(text);
  const preview = text.length > 40 ? text.slice(0, 40) + "…" : text;
  toast(ok ? `Đã copy từ ${name}: “${preview}”` : "Không ghi được clipboard của PC");
}

async function writePCClipboard(text: string): Promise<boolean> {
  try { await navigator.clipboard.writeText(text); return true; } catch { /* fall back */ }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch { return false; }
}


// ---------------------------------------------------------------- focus mode
let focusTile: Tile | null = null;
function openFocus(t: Tile) {
  if (focusTile) closeFocus();
  focusTile = t;
  setActive(t);
  $("#focusName").textContent = t.label() + (state.syncOn && t.dev.inSync ? "  ·  đang đồng bộ nhóm" : "");
  $("#focusStage").appendChild(t.host);
  $("#focus").hidden = false;
  applyQualities();           // zoomed phone sharp, the hidden grid behind it cheap
  t.sync();
}
function closeFocus() {
  const t = focusTile;
  if (!t) return;
  focusTile = null;
  t.el.appendChild(t.host);
  applyQualities();
  $("#focus").hidden = true;
  t.sync();
}
$("#focusClose").onclick = closeFocus;
$("#focusNav").innerHTML = NAV_BUTTONS;
bindNav($("#focusNav"), () => focusTile?.dev.id || "");

// ---------------------------------------------------------------- toolbar
// Toolbar actions: sync on → whole group (d = ""), off → active device.
function toolbarTarget(): string | null {
  if (state.syncOn) return "";
  const d = focusTile?.dev.id || activeId;
  if (!d) { toast("Chọn (chạm) một máy trước, hoặc bật Đồng bộ"); return null; }
  return d;
}
$("#syncBtn").onclick = () => send({ t: "sync", on: !state.syncOn });
$("#selAll").onclick = () => send({ t: "group", ids: state.devices.map((d) => d.id), on: true });
$("#selNone").onclick = () => send({ t: "group", ids: state.devices.map((d) => d.id), on: false });
document.querySelectorAll<HTMLElement>("[data-btn]").forEach((b) => {
  b.onclick = () => {
    const d = toolbarTarget();
    if (d !== null) send({ t: "btn", d, b: b.dataset.btn });
  };
});
$("#pasteBtn").onclick = async () => {
  const d = toolbarTarget();
  if (d === null) return;
  try { pasteText(d, await navigator.clipboard.readText()); }
  catch { toast("Trình duyệt chặn đọc clipboard — dùng Ctrl+V khi trỏ chuột vào iPhone"); }
};
$("#textForm").onsubmit = (e) => {
  e.preventDefault();
  const inp = $<HTMLInputElement>("#textInput");
  if (!inp.value) return;
  const d = toolbarTarget();
  if (d === null) return;
  send({ t: "text", d, s: inp.value });
  inp.value = "";
};
const sizeRange = $<HTMLInputElement>("#sizeRange");
const savedSize = (() => { try { return localStorage.getItem("iosview.tile") || ""; } catch { return ""; } })();
if (savedSize) sizeRange.value = savedSize;
const applySize = () => {
  document.documentElement.style.setProperty("--tile-w", sizeRange.value + "px");
  try { localStorage.setItem("iosview.tile", sizeRange.value); } catch { /* ignore */ }
};
sizeRange.oninput = applySize;
applySize();

$("#scanBtn").onclick = () => {
  $("#scanBtn").setAttribute("disabled", "");
  toast("Đang quét mạng LAN…");
  send({ t: "scan" });
};
const wifiDlg = $<HTMLDialogElement>("#wifiDlg");
$("#addWifiBtn").onclick = () => { $<HTMLTextAreaElement>("#wifiAddr").value = ""; wifiDlg.showModal(); };
wifiDlg.addEventListener("close", () => {
  if (wifiDlg.returnValue !== "ok") return;
  for (const line of $<HTMLTextAreaElement>("#wifiAddr").value.split(/[\s,;]+/)) {
    if (line.trim()) send({ t: "addwifi", s: line.trim() });
  }
});
const setDlg = $<HTMLDialogElement>("#settings");
$("#settingsBtn").onclick = () => {
  $<HTMLInputElement>("#setPw").value = state.password;
  $<HTMLInputElement>("#setPort").value = String(state.vncPort);
  $<HTMLInputElement>("#setQ").value = String(state.quality);
  $<HTMLInputElement>("#setFQ").value = String(state.focusQuality);
  $<HTMLInputElement>("#setStats").checked = showStats;
  $<HTMLInputElement>("#setH264").checked = state.h264;
  $<HTMLInputElement>("#setStrip").checked = state.stripAccents;
  setDlg.showModal();
};
setDlg.addEventListener("close", () => {
  if (setDlg.returnValue !== "save") return;
  showStats = $<HTMLInputElement>("#setStats").checked;
  lsSet("iosview.stats", showStats ? "1" : "0");
  const h264 = $<HTMLInputElement>("#setH264").checked;
  if (h264 !== state.h264) send({ t: "h264", on: h264 });
  send({
    t: "settings",
    pw: $<HTMLInputElement>("#setPw").value,
    port: +$<HTMLInputElement>("#setPort").value,
    q: +$<HTMLInputElement>("#setQ").value,
    fq: +$<HTMLInputElement>("#setFQ").value,
    strip: $<HTMLInputElement>("#setStrip").checked,
  });
});

let toastTimer = 0;
function toast(s: string) {
  const el = $("#toast");
  el.textContent = s;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (el.hidden = true), 3200);
}

connectControl();

// iOSView UI — grid of live iPhone screens (noVNC / H.264, view-only) with our
// own input layer. All input goes to the Go agent over one WebSocket; the agent
// fans it out to the sync group with per-device low-latency connections.
//
// Layout: grid of phones · big screen of the main phone · its functions ·
// control panel (buttons, sliders, tags).
import RFB from "@novnc/novnc";
import { H264View, h264Supported } from "./h264";

type Dev = {
  id: string; name: string; no: number; vncName: string; transport: "usb" | "wifi"; addr: string;
  status: "connecting" | "online" | "offline" | "error"; error?: string;
  width: number; height: number; inSync: boolean;
  udid?: string; wifi?: string;   // wifi = LAN address of the same phone (fallback path)
};
type Tag = { name: string; ids: string[] };
type State = {
  devices: Dev[]; tags: Tag[]; syncOn: boolean; usbError?: string; password: string;
  quality: number; focusQuality: number; h264: boolean; vncPort: number; stripAccents: boolean;
};

const $ = <T extends HTMLElement = HTMLElement>(s: string) => document.querySelector(s) as T;
const grid = $("#grid");
let state: State = { devices: [], tags: [], syncOn: true, password: "", quality: 5, focusQuality: 7, h264: true, vncPort: 5901, stripAccents: true };
// Quality for grid tiles hidden behind the full-window view: still streaming
// (so they come back instantly) but as cheap as possible for phone, Wi-Fi, PC.
const BACKGROUND_QUALITY = 1;
let pageHidden = false;       // app window minimised / in background
const lsGet = (k: string) => { try { return localStorage.getItem(k) || ""; } catch { return ""; } };
const lsSet = (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* ignore */ } };
const prefs = {
  stats: lsGet("iosview.stats") === "1",
  big: lsGet("iosview.big") === "1",   // big screen open (closed by default)
  tileW: +lsGet("iosview.tile") || 150,
  stage: +lsGet("iosview.stage") || 100,
  master: lsGet("iosview.master"),
  filter: "all",
  tag: "",                    // grid filtered to this tag ("" = none)
  gridCtl: lsGet("iosview.gridctl") !== "0",   // touch directly on the small screens (Ctrl+S)
};
let activeId = "";            // last device the user touched
let hoverId = "";             // device under the mouse

const pad = (n: number) => String(n || 0).padStart(2, "0");
const esc = (s: string) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]!));

// ---------------------------------------------------------------- icons
const IC: Record<string, string> = {
  back: `<path d="M15 5l-7 7 7 7"/>`,
  home: `<circle cx="12" cy="12" r="6.5"/>`,
  switcher: `<rect x="4" y="6" width="9" height="12" rx="2"/><path d="M16 7.5h2a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2"/>`,
  power: `<path d="M12 4v7M7.5 7a7 7 0 1 0 9 0"/>`,
  volup: `<path d="M4 9.5h3l4-3.5v12l-4-3.5H4z"/><path d="M15 9.5a3.5 3.5 0 0 1 0 5M18 7a7 7 0 0 1 0 10"/>`,
  voldown: `<path d="M4 9.5h3l4-3.5v12l-4-3.5H4z"/><path d="M15 9.5a3.5 3.5 0 0 1 0 5"/>`,
  mute: `<path d="M4 9.5h3l4-3.5v12l-4-3.5H4z"/><path d="M15.5 9.5l5 5M20.5 9.5l-5 5"/>`,
  paste: `<rect x="6" y="5" width="12" height="16" rx="2"/><path d="M9 3h6v4H9z"/>`,
  selectall: `<rect x="4" y="5" width="16" height="14" rx="1.5" stroke-dasharray="3 2"/><path d="M8 10h8M8 14h5"/>`,
  clear: `<path d="M20 6H9l-5 6 5 6h11z"/><path d="M12 9.5l5 5M17 9.5l-5 5"/>`,
  shot: `<rect x="3" y="7" width="18" height="13" rx="2"/><circle cx="12" cy="13.5" r="3.5"/><path d="M9 7l1.5-2.5h3L15 7"/>`,
  rename: `<path d="M4 20l1.5-4.5L16 5l3 3L8.5 18.5z"/><path d="M14 7l3 3"/>`,
  expand: `<path d="M14 4h6v6M10 20H4v-6M20 4l-7 7M4 20l7-7"/>`,
  full: `<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>`,
  hash: `<path d="M9 4L7 20M17 4l-2 16M4 9h16M3 15h16"/>`,
  unlink: `<path d="M9 15l6-6"/><path d="M11 6l1.5-1.5a4 4 0 0 1 5.7 5.7L16.5 12M13 18l-1.5 1.5a4 4 0 0 1-5.7-5.7L7.5 12"/>`,
  link: `<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7L11.5 6.8M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1.5-1.5"/>`,
  sync: `<path d="M5 10a7 7 0 0 1 12.5-3.5M19 14a7 7 0 0 1-12.5 3.5"/><path d="M18 3v4h-4M6 21v-4h4"/>`,
  all: `<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 12l3 3 5-6"/>`,
  none: `<rect x="4" y="4" width="16" height="16" rx="2"/>`,
  cursor: `<path d="M9 7l11 5-5 1.5L13.5 19z"/><path d="M5 2v3M1 6h3M2.5 2.5l2 2"/>`,
  chev: `<path d="M5 6l6 6-6 6M12 6l6 6-6 6"/>`,
  chevl: `<path d="M19 6l-6 6 6 6M12 6l-6 6 6 6"/>`,
  moon: `<path d="M19 15A8 8 0 0 1 9 5a8 8 0 1 0 10 10z"/>`,
  sun: `<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5L19 19M5 19l1.5-1.5M17.5 6.5L19 5"/>`,
  refresh: `<path d="M20 11a8 8 0 1 0-2.5 6M20 4v7h-7"/>`,
  trash: `<path d="M5 7h14M10 7V4h4v3M7 7l1 13h8l1-13"/>`,
};
const ico = (n: string, vb = "0 0 24 24") => `<svg viewBox="${vb}">${IC[n]}</svg>`;

// ---------------------------------------------------------------- control WS
let ws: WebSocket | null = null;
// Bumped whenever the control link drops or comes back: a gesture begun on
// another link is abandoned, never resumed (the agent lifts any finger it
// left down when the old link closed).
let controlEpoch = 0;
let offlineWarned = false;
const outbox: string[] = [];
// Realtime input is never queued: replaying it after a reconnect would fire
// stale taps/drags on the phones. Only settings-type messages wait.
const REALTIME = new Set(["p", "k", "text", "btn"]);
function send(m: { t: string; [k: string]: unknown }) {
  const s = JSON.stringify(m);
  if (ws && ws.readyState === WebSocket.OPEN) { ws.send(s); return; }
  if (REALTIME.has(m.t)) {
    if (!offlineWarned) { offlineWarned = true; toast("Mất kết nối tới iOSView — thao tác bị bỏ qua"); }
    return;
  }
  if (outbox.length < 50) outbox.push(s);
}
function connectControl() {
  const sock = new WebSocket(`ws://${location.host}/ws/control`);
  ws = sock;
  sock.onopen = () => {
    controlEpoch++;
    offlineWarned = false;
    while (outbox.length) sock.send(outbox.shift()!);
  };
  sock.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.t === "state") applyState(m.state);
    else if (m.t === "toast") toast(m.s);
    else if (m.t === "clip") onPhoneClipboard(m.d, m.s);
    else if (m.t === "scan") {
      $("#scanBtn").removeAttribute("disabled");
      toast(m.found?.length ? `Tìm thấy ${m.found.length} máy: ${m.found.join(", ")}` : "Không tìm thấy TrollVNC nào trong LAN");
    }
  };
  sock.onclose = () => {
    if (ws !== sock) return;   // only one reconnect loop
    controlEpoch++;
    setTimeout(connectControl, 1000);
  };
}

// ---------------------------------------------------------------- tiles
const NAV_BUTTONS =
  `<button data-nav="switcher" title="Đa nhiệm">${ico("switcher")}</button>` +
  `<button data-nav="home" title="Home">${ico("home")}</button>` +
  `<button data-nav="back" title="Quay lại (vuốt từ mép trái)">${ico("back")}</button>`;

function bindNav(bar: HTMLElement, id: () => string, onUse?: () => void) {
  bar.querySelectorAll<HTMLElement>("[data-nav]").forEach((b) => {
    b.onclick = (e) => {
      e.stopPropagation();
      const d = id();
      if (!d) return;
      onUse?.();
      const t = tiles.get(d);
      if (t) navPress(t, b.dataset.nav!, e.altKey);
      else send({ t: "btn", d, b: b.dataset.nav });
    };
  });
}

class Tile {
  dev: Dev;
  el: HTMLElement;
  slot: HTMLElement;      // place of the screen inside the tile
  host: HTMLElement;      // the live screen; moves between slot, big screen, full view
  view: HTMLElement;      // viewer target
  overlay: HTMLElement;   // input capture layer
  msg: HTMLElement;
  stat: HTMLElement;
  nameEl: HTMLElement;
  rfb: RFB | H264View | null = null;   // current viewer (H.264 or VNC/JPEG)
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
  clickTimer = 0;

  constructor(dev: Dev) {
    this.dev = dev;
    this.el = document.createElement("section");
    this.el.className = "tile";
    this.el.innerHTML = `
      <div class="t-head" title="Nhấp: chọn / bỏ chọn · Nhấp đúp: mở màn lớn · Chuột phải: menu">
        <span class="badge"></span><span class="no" title="Nhấp đúp để đổi số máy"></span><span class="name" title="Nhấp đúp để đổi tên (F2)"></span><span class="dot"></span><button class="t-big" title="Mở màn lớn">${ico("expand")}</button>
      </div>
      <div class="slot">
        <div class="host"><div class="view"></div><div class="overlay"></div><div class="msg"></div><div class="stat" hidden></div></div>
        <div class="ctl-ph">${ico("cursor", "0 0 22 22")}<span>Đang ở màn lớn</span><small>nhấp để đóng</small></div>
      </div>
      <div class="navbar">${NAV_BUTTONS}</div>`;
    this.slot = this.el.querySelector(".slot")!;
    this.host = this.el.querySelector(".host")!;
    this.view = this.el.querySelector(".view")!;
    this.overlay = this.el.querySelector(".overlay")!;
    this.msg = this.el.querySelector(".msg")!;
    this.stat = this.el.querySelector(".stat")!;
    this.nameEl = this.el.querySelector(".name")!;
    const head = this.el.querySelector<HTMLElement>(".t-head")!;
    // single click = toggle selection, double click = main phone / rename
    head.onclick = (e) => {
      if ((e.target as HTMLElement).closest("input")) return;
      if ((e.target as HTMLElement).closest(".t-big")) { clearTimeout(this.clickTimer); openBig(this.dev.id); return; }
      clearTimeout(this.clickTimer);
      this.clickTimer = window.setTimeout(() => { selAnchor = this.dev.id; toggleSel([this.dev.id]); }, 230);
    };
    head.ondblclick = (e) => {
      clearTimeout(this.clickTimer);
      if ((e.target as HTMLElement).closest("input")) return;
      const tg = e.target as HTMLElement;
      if (tg.closest(".t-big")) return;
      if (tg.closest(".name")) this.rename();
      else if (tg.closest(".no")) inlineNumber(this.el.querySelector(".no")!, this.dev);
      else openBig(this.dev.id);
    };
    head.oncontextmenu = (e) => { e.preventDefault(); clearTimeout(this.clickTimer); openMenu(this.dev.id, e.clientX, e.clientY); };
    this.el.querySelector<HTMLElement>(".ctl-ph")!.onclick = () => closeBig();
    this.el.addEventListener("pointerenter", () => { hoverId = this.dev.id; });
    this.el.addEventListener("pointerleave", () => { if (hoverId === this.dev.id) hoverId = ""; });
    bindInput(this);
    bindNav(this.el.querySelector(".navbar")!, () => this.dev.id, () => setActive(this));
    this.update(dev);
  }

  label() { return this.dev.name || this.dev.vncName || shortId(this.dev); }

  rename() { inlineRename(this.nameEl, this.dev); }

  update(dev: Dev) {
    const was = this.dev.status;
    const oldTransport = this.dev.transport;
    this.dev = dev;
    if (dev.transport !== oldTransport && this.rfb) this.disconnect(); // cable plugged / unplugged: reconnect on the new path
    const q = (s: string) => this.el.querySelector<HTMLElement>(s)!;
    const badge = q(".badge");
    badge.textContent = dev.status !== "online" ? (dev.transport === "usb" ? "USB" : "Wi‑Fi") : dev.transport === "usb" ? (dev.wifi ? "USB+" : "USB") : "Wi‑Fi";
    badge.dataset.t = dev.status === "online" ? dev.transport : "off";
    badge.title = dev.wifi && dev.transport === "usb" ? `Ưu tiên cáp USB; Wi‑Fi ${dev.wifi} dự phòng` : dev.transport === "wifi" ? dev.addr : "";
    const noEl = this.el.querySelector<HTMLElement>(".no");
    if (noEl) noEl.textContent = pad(dev.no);
    if (this.nameEl.isConnected) this.nameEl.textContent = this.label();
    q(".dot").dataset.s = dev.status;
    q(".dot").title = statusText(dev);
    this.el.classList.toggle("off", dev.status !== "online");
    if (dev.width && dev.height) {
      const ar = `${dev.width} / ${dev.height}`;
      this.el.style.setProperty("--ar", ar);
      this.host.style.setProperty("--ar", ar);
    }
    if (dev.status !== "online") {
      this.msg.textContent = dev.status === "connecting" ? "Đang kết nối…" : (dev.error || "Mất kết nối");
    }
    if (dev.status === "online" && was !== "online") this.retry = 0;
    if (this.pw !== state.password && this.rfb) { this.disconnect(); }
    this.paint();
    this.sync();
  }

  // selection / main-phone / filter classes
  paint() {
    const d = this.dev;
    this.el.classList.toggle("sel", d.inSync);
    this.el.classList.toggle("paused", d.inSync && !state.syncOn);
    this.el.classList.toggle("master", prefs.big && d.id === masterId());
    this.el.classList.toggle("staged", this.host.parentElement !== this.slot);
    this.el.hidden = !passesFilter(d);
  }

  // connect / disconnect the viewer depending on device status + visibility
  sync() {
    const want = this.dev.status === "online" && !pageHidden && (this.visible || isBig(this));
    if (want && !this.rfb && !this.retryTimer) this.connect();
    if (!want && this.rfb) this.disconnect();
  }

  connect() {
    this.pw = state.password;
    // H.264 always (JPEG only if turned off in settings, or WebCodecs missing)
    if (state.h264 && h264Supported()) this.connectH264();
    else this.connectVNC();
  }

  // Hardware H.264 stream. If no frame arrives quickly (phone runs a TrollVNC
  // without the iOSView H.264 patch), fall back to VNC for this device.
  connectH264() {
    const v = new H264View(this.view, `ws://${location.host}/ws/h264/${encodeURIComponent(this.dev.id)}`, this.wantQuality);
    v.onframe = () => { if (this.rfb === v) this.frames++; };
    v.onbytes = (n) => { if (this.rfb === v) this.bytes += n; };
    v.onfirst = () => { clearTimeout(this.firstTimer); this.host.classList.add("live"); this.retry = 0; };
    v.onclose = () => {
      clearTimeout(this.firstTimer);
      this.host.classList.remove("live");
      if (this.rfb !== v) return;
      this.rfb = null;
      // Every phone has H.264: whatever went wrong (Wi-Fi hiccup, agent
      // restart…), just reconnect the same stream with a short backoff.
      if (this.dev.status === "online") {
        const wait = Math.min(500 * 2 ** this.retry++, 5000);
        this.retryTimer = window.setTimeout(() => { this.retryTimer = 0; this.sync(); }, wait);
      }
    };
    clearTimeout(this.firstTimer);
    // stream connected but no picture: reconnect it
    this.firstTimer = window.setTimeout(() => { if (this.rfb === v && !v.gotFrame) v.disconnect(); }, 8000);
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
    rfb.addEventListener("connect", () => { this.host.classList.add("live"); this.retry = 0; });
    rfb.addEventListener("credentialsrequired", () => {
      if (state.password) rfb.sendCredentials({ password: state.password });
      else { this.msg.textContent = "Cần mật khẩu — mở tab Cài đặt"; }
    });
    rfb.addEventListener("securityfailure", () => { this.msg.textContent = "Sai mật khẩu VNC"; });
    rfb.addEventListener("disconnect", () => {
      this.host.classList.remove("live");
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
    this.stat.hidden = !prefs.stats || !this.rfb;
    if (!this.stat.hidden) {
      this.stat.textContent = `${Math.round(this.fps)} fps · ${fmtRate(this.bps)} · ${this.rfb instanceof H264View ? "H.264" : "JPEG"} q${this.wantQuality}`;
    }
  }

  disconnect() {
    const r = this.rfb; this.rfb = null;
    this.host.classList.remove("live");
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
    this.host.remove();
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

function statusText(d: Dev) {
  return d.status === "online" ? "Đang kết nối tốt" : d.status === "connecting" ? "Đang kết nối…" : (d.error || "Mất kết nối");
}

function fmtRate(bps: number) {
  if (bps >= 1e6) return (bps / 1e6).toFixed(1) + " MB/s";
  return Math.round(bps / 1e3) + " KB/s";
}

function shortId(d: Dev) {
  return d.udid ? "iPhone " + d.udid.slice(-6) : d.addr;
}

// ---------------------------------------------------------------- main phone & big screen
// The main phone is shown large next to the grid (pinned big screen); the
// full-window view is used when the big screen is turned off.
let fullTile: Tile | null = null;   // phone shown in the full-window view

function masterId(): string {
  if (prefs.master && tiles.has(prefs.master)) return prefs.master;
  return "";
}
function masterTile(): Tile | null { return tiles.get(masterId()) || null; }
function stagedTile(): Tile | null { return prefs.big ? masterTile() : null; }
function isBig(t: Tile) { return fullTile === t || stagedTile() === t; }

function setMaster(id: string) {
  if (!tiles.has(id)) return;
  prefs.master = id;
  lsSet("iosview.master", id);
  setActive(tiles.get(id)!);
  layout();
}
// Big screen: opened on demand for one phone, closed to give the grid the room.
function openBig(id: string) {
  if (!tiles.has(id)) return;
  prefs.big = true; lsSet("iosview.big", "1");
  renderButtons();
  setMaster(id);
}
function closeBig() {
  if (!prefs.big) return;
  prefs.big = false; lsSet("iosview.big", "0");
  renderButtons();
  layout();
}

// Put every screen where it belongs (slot / big screen / full view), size the
// big screen, and update the main-phone panel.
function layout() {
  const staged = stagedTile();
  for (const t of tiles.values()) {
    const where = fullTile === t ? $("#focusStage") : staged === t ? $("#stageScreen") : t.slot;
    if (t.host.parentElement !== where) where.appendChild(t.host);
  }
  $("#stage").hidden = !staged;
  if (staged) sizeStage(staged);
  for (const t of tiles.values()) { t.paint(); t.setQuality(desiredQuality(t)); t.sync(); }
  renderDevPanel();
  renderTags();
}

function sizeStage(t: Tile) {
  const app = $(".app");
  const avail = app.clientHeight - 20 - 6 - 30 - 28;  // padding, top border, nav bar, caption
  const w0 = t.dev.width || 750, h0 = t.dev.height || 1334;
  const h = Math.max(200, Math.round(avail * prefs.stage / 100));
  const w = Math.round(h * w0 / h0);
  const sc = $("#stageScreen");
  sc.style.width = w + "px"; sc.style.height = h + "px";
  $("#stage").style.width = (w + 12) + "px";
  const d = t.dev;
  $("#stageCap").textContent = `${pad(d.no)} · ${t.label()} — máy chính` + (state.syncOn && d.inSync ? ` · đồng bộ ${syncCount()} máy` : "");
}
new ResizeObserver(() => { const s = stagedTile(); if (s) sizeStage(s); }).observe($(".app"));

function desiredQuality(t: Tile) {
  if (fullTile) return fullTile === t ? state.focusQuality : Math.min(BACKGROUND_QUALITY, state.quality);
  return stagedTile() === t ? state.focusQuality : state.quality;
}

function openFull(t: Tile) {
  fullTile = t;
  setActive(t);
  $("#focusName").textContent = `${pad(t.dev.no)} · ${t.label()}` + (state.syncOn && t.dev.inSync ? "  ·  đang đồng bộ nhóm" : "");
  $("#focus").hidden = false;
  layout();
}
function closeFull() {
  if (!fullTile) return;
  fullTile = null;
  $("#focus").hidden = true;
  layout();
}
$("#focusClose").onclick = closeFull;
$("#stageClose").onclick = closeBig;
$("#focusNav").innerHTML = NAV_BUTTONS;
bindNav($("#focusNav"), () => fullTile?.dev.id || "");
$("#stageNav").innerHTML = NAV_BUTTONS;
bindNav($("#stageNav"), () => masterId());

// ---------------------------------------------------------------- selection
function syncCount() { return state.devices.filter((d) => d.inSync && d.status === "online").length; }
function toggleSel(ids: string[], on?: boolean) {
  const devs = state.devices.filter((d) => ids.includes(d.id));
  if (!devs.length) return;
  const val = on ?? !devs.every((d) => d.inSync);
  for (const d of devs) d.inSync = val;              // optimistic, server confirms
  send({ t: "group", ids: devs.map((d) => d.id), on: val });
  for (const t of tiles.values()) t.paint();
  renderTags(); renderFooter();
}

function passesFilter(d: Dev) {
  if (prefs.tag) {
    const tg = state.tags.find((x) => x.name === prefs.tag);
    if (tg && !tg.ids.includes(d.id)) return false;
  }
  switch (prefs.filter) {
    case "usb": return d.transport === "usb" && d.status === "online";
    case "wifi": return d.transport === "wifi" && d.status === "online";
    case "off": return d.status !== "online";
  }
  return true;
}

// ---------------------------------------------------------------- state
function applyState(s: State) {
  const pwChanged = s.password !== state.password || s.h264 !== state.h264;
  s.tags = s.tags || [];
  state = s;
  const seen = new Set<string>();
  let order = 0;
  for (const d of s.devices) {
    seen.add(d.id);
    let t = tiles.get(d.id);
    if (!t) {
      t = new Tile(d);
      tiles.set(d.id, t);
      io.observe(t.el);
    } else t.update(d);
    t.el.style.order = String(order++);
    if (t.el.parentElement !== grid) grid.appendChild(t.el);
    if (pwChanged) { t.disconnect(); t.sync(); }
  }
  for (const [id, t] of tiles) {
    if (seen.has(id)) continue;
    io.unobserve(t.el);
    if (fullTile === t) { fullTile = null; $("#focus").hidden = true; }
    t.destroy(); tiles.delete(id);
  }
  // first run: the first phone becomes the main phone
  if (!masterId() && s.devices.length) { prefs.master = s.devices[0].id; lsSet("iosview.master", prefs.master); }
  if (prefs.tag && !s.tags.some((x) => x.name === prefs.tag)) prefs.tag = "";

  $("#empty").hidden = s.devices.length > 0;
  const warn = $("#usbWarn");
  warn.hidden = !s.usbError;
  warn.textContent = s.usbError ? "USB: " + s.usbError : "";
  $('[data-tab="conn"]').classList.toggle("alert", !!s.usbError);
  $('[data-tab="conn"]').title = s.usbError ? "Có lỗi kết nối USB — xem chi tiết" : "";
  renderButtons();
  syncSliders();
  layout();
  renderFooter();
}

function renderFooter() {
  const online = state.devices.filter((d) => d.status === "online").length;
  const sel = state.devices.filter((d) => d.inSync).length;
  $("#selinfo").textContent = `Đã chọn ${sel}/${state.devices.length} · online ${online}`;
}

// ---------------------------------------------------------------- rename
function inlineRename(target: HTMLElement, d: Dev) {
  if (!target.isConnected) return;
  const inp = document.createElement("input");
  inp.className = "rename-in";
  inp.maxLength = 32;
  inp.value = d.name || d.vncName || "";
  inp.placeholder = d.vncName || shortId(d);
  inp.setAttribute("aria-label", `Tên máy ${pad(d.no)}`);
  target.replaceWith(inp);
  inp.focus(); inp.select();
  let done = false;
  const finish = (ok: boolean) => {
    if (done) return;
    done = true;
    inp.replaceWith(target);
    if (!ok) return;
    const v = inp.value.trim();
    if (v === (d.name || d.vncName || "")) return;
    send({ t: "rename", d: d.id, name: v });
    target.textContent = v || d.vncName || shortId(d);
    toast(v ? `Đã đổi tên máy ${pad(d.no)}: ${v}` : `Máy ${pad(d.no)} dùng lại tên mặc định`);
  };
  inp.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") finish(true); if (e.key === "Escape") finish(false); };
  inp.onblur = () => finish(true);
  for (const ev of ["click", "dblclick", "pointerdown"]) inp.addEventListener(ev, (e) => e.stopPropagation());
}
// Change the display number (1..999). Taken numbers are swapped by the agent,
// and the grid re-sorts from the next state push.
function inlineNumber(target: HTMLElement, d: Dev) {
  if (!target.isConnected) return;
  const inp = document.createElement("input");
  inp.className = "rename-in num-in";
  inp.type = "number"; inp.min = "1"; inp.max = "999";
  inp.value = String(d.no);
  inp.setAttribute("aria-label", `Số máy (đang là ${pad(d.no)})`);
  target.replaceWith(inp);
  inp.focus(); inp.select();
  let done = false;
  const finish = (ok: boolean) => {
    if (done) return;
    done = true;
    inp.replaceWith(target);
    const n = parseInt(inp.value, 10);
    if (!ok || !n || n === d.no) return;
    if (n < 1 || n > 999) { toast("Số máy phải từ 1 đến 999"); return; }
    const other = state.devices.find((x) => x.no === n && x.id !== d.id);
    send({ t: "number", d: d.id, no: n });
    target.textContent = pad(n);
    toast(other ? `Máy ${pad(d.no)} ↔ ${pad(n)}: đã đổi số cho nhau` : `Đã đổi máy ${pad(d.no)} thành số ${pad(n)}`);
  };
  inp.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") finish(true); if (e.key === "Escape") finish(false); };
  inp.onblur = () => finish(true);
  for (const ev of ["click", "dblclick", "pointerdown"]) inp.addEventListener(ev, (e) => e.stopPropagation());
}
function renumber(id: string) {
  const t = tiles.get(id);
  if (!t) return;
  const no = t.el.querySelector<HTMLElement>(".no");
  if (no && !t.el.hidden) inlineNumber(no, t.dev);
  else if (id === masterId() && !$("#dev").hidden) inlineNumber($("#devNo"), t.dev);
}

function renameDevice(id: string) {
  const t = tiles.get(id);
  if (!t) return;
  if (t.nameEl.isConnected && !t.el.hidden && getComputedStyle(t.nameEl).display !== "none") t.rename();
  else if (id === masterId() && !$("#dev").hidden) inlineRename($("#devName"), t.dev);
  else { openBig(id); requestAnimationFrame(() => inlineRename($("#devName"), t.dev)); }
}

// ---------------------------------------------------------------- main-phone panel
type Fn = { k: string; label: string; icon: string; kbd?: string; red?: boolean };
const DEV_FNS: Fn[] = [
  { k: "home", label: "Home", icon: "home" },
  { k: "switcher", label: "Đa nhiệm", icon: "switcher" },
  { k: "back", label: "Quay lại", icon: "back" },
  { k: "power", label: "Khoá / mở màn hình", icon: "power" },
  { k: "volup", label: "Tăng âm lượng", icon: "volup" },
  { k: "voldown", label: "Giảm âm lượng", icon: "voldown" },
  { k: "mute", label: "Tắt / bật tiếng", icon: "mute" },
  { k: "#paste", label: "Dán từ PC", icon: "paste", kbd: "Ctrl+V" },
  { k: "selectall", label: "Chọn hết chữ", icon: "selectall" },
  { k: "clear", label: "Xoá ô nhập", icon: "clear", kbd: "Ctrl+⌫" },
  { k: "#shot", label: "Chụp màn hình", icon: "shot" },
  { k: "#full", label: "Phóng to toàn cửa sổ", icon: "full" },
];
$("#devFns").innerHTML = DEV_FNS.map((f) =>
  `<button class="fn" data-fn="${f.k}">${ico(f.icon)}${f.label}${f.kbd ? `<kbd>${f.kbd}</kbd>` : ""}</button>`).join("");
$("#devRename").innerHTML = ico("rename");
$("#devFns").onclick = async (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>("[data-fn]");
  const t = masterTile();
  if (!b || !t) return;
  setActive(t);
  const k = b.dataset.fn!;
  if (k === "#paste") pasteFromPC(t.dev.id);
  else if (k === "#shot") shoot([t]);
  else if (k === "#full") openFull(t);
  else {
    send({ t: "btn", d: t.dev.id, b: k });
    if (k === "selectall") toast("Đang chọn hết văn bản trên máy…");
    if (k === "clear") toast("Đang xoá nội dung ô nhập…");
  }
};
$("#devName").ondblclick = () => { const t = masterTile(); if (t) inlineRename($("#devName"), t.dev); };
$("#devNo").ondblclick = () => { const t = masterTile(); if (t) inlineNumber($("#devNo"), t.dev); };
$("#devRename").onclick = () => { const t = masterTile(); if (t) inlineRename($("#devName"), t.dev); };

function renderDevPanel() {
  const t = stagedTile();
  $("#dev").hidden = !t;
  if (!t) return;
  const d = t.dev;
  if ($("#devNo")) $("#devNo").textContent = pad(d.no);
  const nm = $("#devName");
  if (nm.isConnected) nm.textContent = t.label();
  $("#devScope").textContent = state.syncOn && d.inSync ? `· cả nhóm (${syncCount()})` : "· riêng máy này";
  const via = d.transport === "usb" ? "USB" + (d.wifi ? ` · Wi‑Fi ${d.wifi} dự phòng` : "") : `Wi‑Fi ${d.addr}`;
  const lines = [
    statusText(d),
    via,
    d.width ? `${d.width}×${d.height}` : "",
    d.vncName && d.vncName !== d.name ? d.vncName : "",
    d.udid ? `UDID ${d.udid.slice(0, 8)}…${d.udid.slice(-6)}` : "",
    t.rfb ? (t.rfb instanceof H264View ? "Hình: H.264" : "Hình: JPEG") : "",
  ].filter(Boolean);
  $("#devMeta").innerHTML = lines.map(esc).join("<br>");
  // Wi-Fi extras: unlink / remove, manual merge
  const extra: string[] = [];
  const cands = d.transport === "wifi" && !d.udid ? usbCandidates() : [];
  if (cands.length) extra.push(`<div class="sec">GỘP VỚI MÁY USB</div>` + cands.map((c) =>
    `<button class="fn" data-link="${esc(c.id)}">${ico("link")}${pad(c.no)} · ${esc(c.name || c.vncName || shortId(c))}</button>`).join(""));
  if (d.wifi) extra.push(`<button class="fn red" data-remove="1">${ico(d.udid ? "unlink" : "trash")}${d.udid ? "Bỏ kết nối Wi‑Fi" : "Xoá máy Wi‑Fi"}</button>`);
  $("#devExtra").innerHTML = extra.join("");
}
$("#devExtra").onclick = (e) => {
  const t = masterTile();
  if (!t) return;
  const b = (e.target as HTMLElement).closest<HTMLElement>("button");
  if (b?.dataset.link) send({ t: "link", d: t.dev.id, s: b.dataset.link });
  if (b?.dataset.remove && confirmRemove(t.dev)) send({ t: "remove", d: t.dev.id });
};

// Cable-connected phones that have no Wi-Fi address attached yet.
function usbCandidates(): Dev[] {
  return state.devices.filter((d) => d.udid && !d.wifi);
}
function confirmRemove(d: Dev) {
  return window.confirm(d.udid ? `Bỏ kết nối Wi‑Fi ${d.wifi} của máy ${pad(d.no)}? (máy USB vẫn giữ)` : `Xoá máy ${pad(d.no)} · ${d.name || d.addr} khỏi danh sách?`);
}

// ---------------------------------------------------------------- context menu
function openMenu(id: string, x: number, y: number) {
  const t = tiles.get(id);
  if (!t) return;
  const d = t.dev;
  const cands = d.transport === "wifi" && !d.udid ? usbCandidates() : [];
  let h = `<div class="mh">${pad(d.no)} · ${esc(t.label())}</div>`;
  h += `<button data-m="rename">${ico("rename")}Đổi tên…<kbd>F2</kbd></button>`;
  h += `<button data-m="number">${ico("hash")}Đổi số máy…</button>`;
  h += prefs.big && id === masterId()
    ? `<button data-m="closebig">${ico("expand")}Đóng màn lớn</button>`
    : `<button data-m="master">${ico("expand")}Mở màn lớn</button>`;
  h += `<button data-m="sel">${ico(d.inSync ? "none" : "all")}${d.inSync ? "Bỏ chọn" : "Chọn"} (nhóm đồng bộ)</button>`;
  h += `<button data-m="full">${ico("full")}Phóng to toàn cửa sổ</button>`;
  h += `<button data-m="shot">${ico("shot")}Chụp màn hình</button>`;
  if (cands.length) {
    h += `<hr><div class="mh">GỘP VỚI MÁY USB</div>`;
    h += cands.map((c) => `<button data-link="${esc(c.id)}">${ico("link")}${pad(c.no)} · ${esc(c.name || c.vncName || shortId(c))}</button>`).join("");
  }
  if (d.wifi) h += `<hr><button class="red" data-m="remove">${ico(d.udid ? "unlink" : "trash")}${d.udid ? "Bỏ kết nối Wi‑Fi" : "Xoá máy Wi‑Fi"}</button>`;
  const m = $("#menu");
  m.innerHTML = h;
  m.dataset.id = id;
  m.hidden = false;
  const r = m.getBoundingClientRect();
  m.style.left = Math.max(4, Math.min(x, innerWidth - r.width - 6)) + "px";
  m.style.top = Math.max(4, Math.min(y, innerHeight - r.height - 6)) + "px";
}
$("#menu").onclick = (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>("button");
  const m = $("#menu");
  const id = m.dataset.id || "";
  m.hidden = true;
  const t = tiles.get(id);
  if (!b || !t) return;
  if (b.dataset.link) { send({ t: "link", d: id, s: b.dataset.link }); return; }
  switch (b.dataset.m) {
    case "rename": renameDevice(id); break;
    case "master": openBig(id); break;
    case "closebig": closeBig(); break;
    case "number": renumber(id); break;
    case "sel": toggleSel([id]); break;
    case "full": openFull(t); break;
    case "shot": shoot([t]); break;
    case "remove": if (confirmRemove(t.dev)) send({ t: "remove", d: id }); break;
  }
};
document.addEventListener("pointerdown", (e) => { if (!(e.target as HTMLElement).closest("#menu")) $("#menu").hidden = true; });
window.addEventListener("blur", () => { $("#menu").hidden = true; });

// ---------------------------------------------------------------- tags (number grid)
let tagsKey = "";
function renderTags() {
  const ids = state.devices.map((d) => d.id);
  const key = JSON.stringify([state.devices.map((d) => [d.id, d.no, d.name, d.inSync, d.status === "online"]), state.tags, masterId(), prefs.big, prefs.tag]);
  if (key === tagsKey) return;
  tagsKey = key;
  const byId = new Map(state.devices.map((d) => [d.id, d]));
  const cells = (list: string[]) => {
    const devs = (list.map((i) => byId.get(i)).filter(Boolean) as Dev[]).sort((a, b) => a.no - b.no);
    if (!devs.length) return "";
    return `<div class="cells">${devs.map((d) =>
      `<button class="cell${d.inSync ? " on" : ""}${d.status !== "online" ? " off" : ""}${prefs.big && d.id === masterId() ? " m" : ""}" data-cell="${esc(d.id)}" title="${pad(d.no)} · ${esc(d.name || d.vncName || shortId(d))}">${pad(d.no)}</button>`).join("")}</div>`;
  };
  const row = (key: string, label: string, list: string[], tag?: Tag) => {
    const members = list.filter((i) => byId.has(i));
    const sel = members.filter((i) => byId.get(i)!.inSync).length;
    const all = members.length > 0 && sel === members.length;
    const acts = tag ? `<span class="tg-act"><button class="ib" data-tagupd="${esc(tag.name)}" title="Cập nhật nhãn = các máy đang chọn">${ico("refresh")}</button><button class="ib" data-tagdel="${esc(tag.name)}" title="Xoá nhãn">${ico("trash")}</button></span>` : "";
    const on = tag && prefs.tag === tag.name ? " on" : "";
    const nameBtn = tag
      ? `<button class="tg-name${on}" data-tagf="${esc(tag.name)}" title="Nhấp: chỉ hiện máy của nhãn này · Nhấp đúp: đổi tên nhãn">${esc(label)} <span class="n">(${sel}/${members.length})</span></button>`
      : `<span class="tg-name">${esc(label)} <span class="n">(${sel}/${members.length})</span></span>`;
    return `<div class="tg-row"><input type="checkbox" data-tg="${esc(key)}" ${all ? "checked" : ""} ${members.length ? "" : "disabled"} title="Chọn / bỏ chọn cả nhóm">${nameBtn}${acts}</div>${cells(members)}`;
  };
  const tagged = new Set(state.tags.flatMap((t) => t.ids));
  let h = row("*", "Tất cả máy", ids);
  const untagged = ids.filter((i) => !tagged.has(i));
  if (state.tags.length) h += row("-", "Chưa gắn nhãn", untagged);
  state.tags.forEach((t, i) => { h += row("t" + i, t.name, t.ids, t); });
  $("#tags").innerHTML = h;
}
let cellTimer = 0;
$("#tags").addEventListener("click", (e) => {
  const el = e.target as HTMLElement;
  const cell = el.closest<HTMLElement>("[data-cell]");
  if (cell) {
    clearTimeout(cellTimer);
    cellTimer = window.setTimeout(() => toggleSel([cell.dataset.cell!]), 230);
    return;
  }
  const f = el.closest<HTMLElement>("[data-tagf]");
  if (f) {
    clearTimeout(cellTimer);
    const name = f.dataset.tagf!;
    cellTimer = window.setTimeout(() => {
      prefs.tag = prefs.tag === name ? "" : name;
      tagsKey = ""; layout();
      if (prefs.tag) toast(`Chỉ hiện máy của nhãn “${name}” — nhấp lại để hiện tất cả`);
    }, 230);
    return;
  }
  const upd = el.closest<HTMLElement>("[data-tagupd]");
  if (upd) {
    const sel = state.devices.filter((d) => d.inSync).map((d) => d.id);
    send({ t: "tag", name: upd.dataset.tagupd, ids: sel });
    toast(`Nhãn “${upd.dataset.tagupd}” giờ gồm ${sel.length} máy đang chọn`);
    return;
  }
  const del = el.closest<HTMLElement>("[data-tagdel]");
  if (del && window.confirm(`Xoá nhãn “${del.dataset.tagdel}”? (các máy không bị ảnh hưởng)`)) {
    if (prefs.tag === del.dataset.tagdel) prefs.tag = "";
    send({ t: "tagdel", name: del.dataset.tagdel });
  }
});
$("#tags").addEventListener("dblclick", (e) => {
  clearTimeout(cellTimer);
  const el = e.target as HTMLElement;
  const cell = el.closest<HTMLElement>("[data-cell]");
  if (cell) { openBig(cell.dataset.cell!); return; }
  const f = el.closest<HTMLElement>("[data-tagf]");
  if (f) renameTag(f);
});
$("#tags").addEventListener("contextmenu", (e) => {
  const cell = (e.target as HTMLElement).closest<HTMLElement>("[data-cell]");
  if (!cell) return;
  e.preventDefault();
  openMenu(cell.dataset.cell!, e.clientX, e.clientY);
});
$("#tags").addEventListener("change", (e) => {
  const inp = e.target as HTMLInputElement;
  const k = inp.dataset.tg;
  if (!k) return;
  const all = state.devices.map((d) => d.id);
  const tagged = new Set(state.tags.flatMap((t) => t.ids));
  const list = k === "*" ? all : k === "-" ? all.filter((i) => !tagged.has(i)) : (state.tags[+k.slice(1)]?.ids || []);
  toggleSel(list.filter((i) => tiles.has(i)), inp.checked);
});
function renameTag(btn: HTMLElement) {
  const old = btn.dataset.tagf!;
  const inp = document.createElement("input");
  inp.className = "rename-in"; inp.maxLength = 32; inp.value = old;
  btn.replaceWith(inp); inp.focus(); inp.select();
  let done = false;
  const finish = (ok: boolean) => {
    if (done) return; done = true;
    const v = inp.value.trim();
    if (ok && v && v !== old) {
      if (state.tags.some((t) => t.name === v)) { toast(`Đã có nhãn “${v}”`); tagsKey = ""; renderTags(); return; }
      if (prefs.tag === old) prefs.tag = v;
      send({ t: "tag", name: v, old });
    }
    tagsKey = ""; renderTags();
  };
  inp.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") finish(true); if (e.key === "Escape") finish(false); };
  inp.onblur = () => finish(true);
}
$("#addTag").onclick = () => {
  const f = $("#tagNew");
  f.hidden = !f.hidden;
  if (!f.hidden) { $<HTMLInputElement>("#tagName").value = ""; $("#tagName").focus(); }
};
$("#tagNew").onsubmit = (e) => {
  e.preventDefault();
  const name = $<HTMLInputElement>("#tagName").value.trim();
  if (!name) return;
  if (state.tags.some((t) => t.name === name)) { toast(`Đã có nhãn “${name}”`); return; }
  const sel = state.devices.filter((d) => d.inSync).map((d) => d.id);
  send({ t: "tag", name, ids: sel });
  $("#tagNew").hidden = true;
  toast(`Đã tạo nhãn “${name}” với ${sel.length} máy đang chọn`);
};
$("#tagName").addEventListener("keydown", (e) => { if (e.key === "Escape") $("#tagNew").hidden = true; });

// ---------------------------------------------------------------- input
// Where the phone picture really is on screen. The H.264 canvas fills its box
// with object-fit: contain, so when the stream's shape differs from the tile
// (phone rotated, stream scaled) the picture is letterboxed: using the whole
// box would put every touch off by the black bars.
function screenRect(c: HTMLCanvasElement) {
  const b = c.getBoundingClientRect();
  if (!c.width || !c.height || !b.width || !b.height) return b;
  const s = Math.min(b.width / c.width, b.height / c.height);
  const w = c.width * s, h = c.height * s;
  const left = b.left + (b.width - w) / 2, top = b.top + (b.height - h) / 2;
  return { left, top, width: w, height: h, right: left + w, bottom: top + h };
}

// Normalised (0..1) position of a pointer event on the device screen.
function norm(t: Tile, e: { clientX: number; clientY: number }) {
  const c = t.canvas();
  if (!c) return null;
  const r = screenRect(c);
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
    if (!c || !t.host.isConnected) continue;
    const hr = t.host.getBoundingClientRect(), cr = screenRect(c);
    if (!cr.width) continue;
    const dot = document.createElement("i");
    dot.className = "ripple " + kind;
    dot.style.left = `${cr.left - hr.left + x * cr.width}px`;
    dot.style.top = `${cr.top - hr.top + y * cr.height}px`;
    t.host.appendChild(dot);
    setTimeout(() => dot.remove(), 450);
  }
}

// Mouse on a phone screen (docs/SHORTCUTS.md): left = touch, middle = Home,
// right click = Back, right press-and-hold = app switcher, wheel = scroll.
// Alt held = this phone only (individual control), even in sync mode.
const HOLD_MS = 450;

// A hardware-button action on phone t (no on-screen feedback).
function navPress(t: Tile, b: string, one = false) {
  setActive(t);
  send({ t: "btn", d: t.dev.id, b, one: one || undefined });
}

// Is t shown in its grid slot (small screen), not on the big / full screen?
function isSmall(t: Tile) { return t.host.parentElement === t.slot; }

let twoFingerWarned = false;

function bindInput(t: Tile) {
  const ov = t.overlay;
  let mask = 0;
  let epoch = 0;              // controlEpoch when the current gesture began
  let solo = false;           // gesture started with Alt: this phone only
  let lastWheel = 0;
  let pending: { x: number; y: number } | null = null;
  let raf = 0;
  let holdTimer = 0;          // right button press-and-hold → app switcher
  let holdOne = false;
  const id = () => t.dev.id;
  // Control link changed since this gesture began: drop it. Returns true
  // when there is no live gesture.
  const stale = () => {
    if (mask && epoch !== controlEpoch) { mask = 0; pending = null; }
    return !mask;
  };
  const sendPtr = (x: number, y: number) =>
    send({ t: "p", d: id(), x: +Math.min(1, Math.max(0, x)).toFixed(4), y: +Math.min(1, Math.max(0, y)).toFixed(4), m: mask, one: solo || undefined });
  const flushMove = () => {
    raf = 0;
    if (pending && !stale()) sendPtr(pending.x, pending.y);
    pending = null;
  };
  const cancelHold = () => { clearTimeout(holdTimer); holdTimer = 0; };

  ov.addEventListener("contextmenu", (e) => e.preventDefault());
  ov.addEventListener("pointerenter", () => { hoverId = id(); });
  ov.addEventListener("pointerdown", (e) => {
    const p = norm(t, e);
    const small = isSmall(t);
    if (!p || !p.inside) {
      // right click on the empty area around the big screen: close it
      if (e.button === 2 && !small) { e.preventDefault(); closeBigOrFull(); }
      return;
    }
    e.preventDefault();
    if (small && !prefs.gridCtl) {     // small screens are view-only: click = show it big
      if (e.button === 0) openBig(id());
      return;
    }
    setActive(t);
    if (e.button === 1) { navPress(t, "home", e.altKey); return; }
    if (e.button === 2) {
      cancelHold();
      holdOne = e.altKey;
      ov.setPointerCapture(e.pointerId);
      holdTimer = window.setTimeout(() => { holdTimer = -1; navPress(t, "switcher", holdOne); }, HOLD_MS);
      return;
    }
    if (e.button !== 0) return;
    if (e.shiftKey && !twoFingerWarned) { twoFingerWarned = true; toast("Điều khiển hai ngón (Shift) chưa hỗ trợ — đang chạm một ngón"); }
    ov.setPointerCapture(e.pointerId);
    stale();
    if (!mask) { epoch = controlEpoch; solo = e.altKey; }
    mask |= 1;
    sendPtr(p.x, p.y);
    ripple(solo ? [t] : echoTargets(t), p.x, p.y);
  });
  ov.addEventListener("pointermove", (e) => {
    if (stale()) return;
    const p = norm(t, e);
    if (!p) return;
    pending = { x: p.x, y: p.y };
    // send at display rate; the agent coalesces further if a phone lags
    if (!raf) raf = requestAnimationFrame(flushMove);
  });
  const up = (e: PointerEvent) => {
    if (e.button === 2 && holdTimer) {   // right click released: Back (unless the hold fired)
      const short = holdTimer > 0;
      cancelHold();
      if (short && e.type === "pointerup") navPress(t, "back", holdOne);
      return;
    }
    if (stale()) return;
    if (e.type === "pointerup" && e.button !== 0) return;
    if (raf) { cancelAnimationFrame(raf); flushMove(); }
    const p = norm(t, e) || { x: 0.5, y: 0.5 };
    mask = 0;
    sendPtr(p.x, p.y);
  };
  ov.addEventListener("pointerup", up);
  ov.addEventListener("pointercancel", (e) => { cancelHold(); up(e); });
  ov.addEventListener("lostpointercapture", (e) => { if (holdTimer > 0) cancelHold(); if (mask) up(e as PointerEvent); });
  ov.addEventListener("wheel", (e) => {
    if (isSmall(t) && !prefs.gridCtl) return;    // let the grid scroll
    e.preventDefault();
    const now = performance.now();
    if (now - lastWheel < 35) return;
    lastWheel = now;
    const p = norm(t, e);
    if (!p) return;
    setActive(t);
    stale();
    const bit = e.deltaY < 0 ? 8 : 16;
    const one = e.altKey || undefined;
    send({ t: "p", d: id(), x: +p.x.toFixed(4), y: +p.y.toFixed(4), m: bit | mask, one });
    send({ t: "p", d: id(), x: +p.x.toFixed(4), y: +p.y.toFixed(4), m: mask, one });
  }, { passive: false });
}

function setActive(t: Tile) { activeId = t.dev.id; }

// Keyboard → phone under the mouse, else the full view / main phone / last touched.
function keyTarget() { return hoverId || fullTile?.dev.id || activeId || masterId(); }

// Special (non-text) keys forwarded as a single down+up — never held, so a
// modifier can never get stuck on the phone.
const SPECIAL: Record<string, number> = {
  Enter: 0xff0d, NumpadEnter: 0xff0d, Backspace: 0xff08, Tab: 0xff09,
  Delete: 0xffff, Escape: 0xff1b, ArrowLeft: 0xff51, ArrowUp: 0xff52,
  ArrowRight: 0xff53, ArrowDown: 0xff54, Home: 0xff50, End: 0xff57,
  PageUp: 0xff55, PageDown: 0xff56,
};
function tapKey(d: string, ks: number, one?: boolean) {
  send({ t: "k", d, k: ks, down: true, one });
  send({ t: "k", d, k: ks, down: false, one });
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
  if (e.key === "Alt") { e.preventDefault(); setSolo(true); return; }   // never the window menu
  const combo = comboOf(e);
  const act = actOf(combo);
  if (isTyping(e)) {
    if (act === "notesClose" && notesOpen()) { e.preventDefault(); closeNotes(); }
    else if (act === "notes" || combo === "Ctrl+R") e.preventDefault();    // never reload the window
    return;
  }
  if (document.querySelector("dialog[open]")) return;   // dialogs handle their own keys
  if (e.key === "F4" && !e.altKey) { e.preventDefault(); toggleSync(); return; }
  if (e.key === "F2") { e.preventDefault(); const d = hoverId || activeId || masterId(); if (d) renameDevice(d); return; }
  if (e.key === "Escape") {
    e.preventDefault();
    if (!$("#menu").hidden) { $("#menu").hidden = true; return; }
    const t = tiles.get(keyTarget());
    if (t && !e.repeat) navPress(t, "back", e.altKey);     // Esc = Back on the phone
    return;
  }
  if (e.key === "F5" || e.key === "F11" || e.key === "F12") return;   // leave window keys alone
  // Never forward bare modifier keys to the phone (that left Ctrl/Shift stuck
  // "down" on the device and corrupted the next paste).
  if (["Control", "Shift", "Meta", "CapsLock", "ContextMenu"].includes(e.key)) return;
  if (act) {
    if (!(act === "paste" && combo === "Ctrl+V")) e.preventDefault();   // Ctrl+V: let "paste" fire
    if (!e.repeat) runAct(act, e);
    return;
  }
  if ((e.ctrlKey || e.metaKey) && !e.altKey) {
    const k = e.key.toLowerCase();
    e.preventDefault();                // swallow every other Ctrl/⌘ combo (no reload, print…)
    if (e.repeat && !k.startsWith("arrow")) return;
    if (k === "a") { selectAllPhones(); return; }
    if (k === "arrowleft" || k === "arrowup") { switchBig(-1); return; }
    if (k === "arrowright" || k === "arrowdown") { switchBig(1); return; }
    const d = keyTarget();
    if (!d) return;
    const kt = tiles.get(d);
    if (kt) setActive(kt);
    if (k === "backspace" || k === "delete") { send({ t: "btn", d, b: "clear" }); toast("Đang xoá nội dung ô nhập…"); return; }
    if (["c", "x", "z", "y"].includes(k)) {
      sendCommand(d, k);
      if (k === "c" || k === "x") {
        copyWait = { d, until: performance.now() + 4000 };
        toast("Đang copy từ iPhone…");
      }
    }
    return;
  }
  const d = keyTarget();
  if (!d) return;
  const kt = tiles.get(d);
  if (kt) setActive(kt);
  const one = e.altKey || undefined;   // Alt held: this phone only
  if (SPECIAL[e.code] !== undefined || SPECIAL[e.key] !== undefined) {
    e.preventDefault();
    tapKey(d, SPECIAL[e.code] ?? SPECIAL[e.key], one);
    return;
  }
  if (e.key.length === 1) {
    // Printable character: type it (handles uppercase/symbols via the agent).
    e.preventDefault();
    send({ t: "text", d, s: e.key, one });
  }
});
window.addEventListener("keyup", (e) => { if (e.key === "Alt") { e.preventDefault(); setSolo(false); } });
window.addEventListener("blur", () => setSolo(false));

// Ctrl+V while pointing at a phone: paste the PC clipboard into it (and into
// the whole sync group when sync is on). Only when Ctrl+V is the paste key.
window.addEventListener("paste", (e) => {
  if (isTyping(e) || document.querySelector("dialog[open]") || keys.paste !== "Ctrl+V") return;
  const d = keyTarget();
  if (!d) { toast("Trỏ chuột vào màn hình iPhone rồi bấm Ctrl+V"); return; }
  const text = e.clipboardData?.getData("text/plain") || "";
  e.preventDefault();
  const pt = tiles.get(d);
  if (pt) setActive(pt);
  pasteText(d, text);
});

function pasteText(d: string, text: string, opt: { force?: boolean; one?: boolean } = {}) {
  if (!text) { toast("Clipboard của PC đang trống (chỉ hỗ trợ văn bản)"); return; }
  send({ t: "text", d, s: text, f: opt.force || undefined, one: opt.one || undefined });
  if (opt.force) { toast("Dán cưỡng bức: đang gõ từng phím (bỏ dấu)…"); return; }
  // ASCII is typed straight in; text with dấu tiếng Việt / emoji can't be typed
  // on an iOS HID keyboard, so it goes via clipboard and needs a tap on Paste.
  if (/[^\x00-\x7f]/.test(text)) {
    toast(state.stripAccents
      ? "Đang gõ (đã bỏ dấu tiếng Việt)…"
      : "Văn bản có dấu đã nạp vào clipboard máy — chạm “Paste” trên iPhone để dán");
  } else toast("Đang gõ vào máy…");
}
async function pasteFromPC(d: string, opt: { force?: boolean; one?: boolean } = {}) {
  try { pasteText(d, await navigator.clipboard.readText(), opt); }
  catch { toast("Không đọc được clipboard — trỏ chuột vào iPhone rồi bấm Ctrl+V"); }
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

// ---------------------------------------------------------------- screenshots
async function shoot(list: Tile[]) {
  const live = list.filter((t) => t.canvas() && t.host.classList.contains("live"));
  if (!live.length) { toast("Máy chưa có hình để chụp"); return; }
  let saved = 0, last = "";
  for (const t of live) {
    const c = t.canvas()!;
    const blob = await new Promise<Blob | null>((r) => c.toBlob(r, "image/png"));
    if (!blob) continue;
    try {
      const res = await fetch(`/api/shot?d=${encodeURIComponent(t.dev.id)}`, { method: "POST", body: blob });
      if (res.ok) { saved++; last = (await res.json()).path || ""; }
    } catch { /* ignore */ }
  }
  toast(saved ? (saved === 1 ? `Đã lưu ảnh: ${last}` : `Đã lưu ${saved} ảnh vào thư mục screenshots`) : "Không lưu được ảnh chụp");
}

// ---------------------------------------------------------------- control panel
// Group actions: sync on → whole group (d = ""), off → main / last phone.
function groupTarget(): string | null {
  if (state.syncOn) return "";
  const d = fullTile?.dev.id || activeId || masterId();
  if (!d) { toast("Chọn (chạm) một máy trước, hoặc bật Đồng bộ"); return null; }
  return d;
}
function toggleSync() { state.syncOn = !state.syncOn; send({ t: "sync", on: state.syncOn }); renderButtons(); for (const t of tiles.values()) t.paint(); toast(state.syncOn ? "Đồng bộ: BẬT (F4)" : "Đồng bộ: TẮT (F4)"); }

type Btn = { k: string; label: string; icon?: string; sw?: () => boolean; cls?: string; title: string };
const BTNS: Btn[] = [
  { k: "sync", label: "Đồng bộ", sw: () => state.syncOn, cls: "sync", title: "Bật/tắt đồng bộ thao tác cho các máy đang chọn (F4)" },
  { k: "all", label: "Chọn hết", icon: "all", title: "Đưa tất cả máy vào nhóm đồng bộ" },
  { k: "none", label: "Bỏ chọn", icon: "none", title: "Bỏ tất cả khỏi nhóm" },
  { k: "home", label: "Home", icon: "home", title: "Home — cả nhóm (hoặc máy đang dùng khi tắt đồng bộ)" },
  { k: "switcher", label: "Đa nhiệm", icon: "switcher", title: "Đa nhiệm" },
  { k: "power", label: "Khoá", icon: "power", title: "Khoá / mở màn hình" },
  { k: "voldown", label: "Vol −", icon: "voldown", title: "Giảm âm lượng" },
  { k: "volup", label: "Vol +", icon: "volup", title: "Tăng âm lượng" },
  { k: "paste", label: "Dán", icon: "paste", title: "Gõ nội dung clipboard của PC vào máy (hoặc trỏ chuột vào iPhone rồi Ctrl+V)" },
  { k: "shot", label: "Chụp", icon: "shot", title: "Chụp màn hình các máy đang chọn" },
  { k: "gridctl", label: "Chạm lưới", sw: () => prefs.gridCtl, title: "Điều khiển trực tiếp trên màn nhỏ (Ctrl+S). Tắt: nhấp màn nhỏ = mở lên màn lớn" },
  { k: "pin", label: "Màn lớn", sw: () => prefs.big, title: "Mở / đóng màn lớn (nhấp đúp thanh tên một máy để mở máy đó)" },
  { k: "stats", label: "FPS", sw: () => prefs.stats, title: "Hiện fps / băng thông trên từng máy" },
];
function renderButtons() {
  const el = $("#btns");
  if (!el.children.length) {
    el.innerHTML = BTNS.map((b) => `<button class="btn ${b.cls || ""}" data-b="${b.k}" title="${esc(b.title)}">${b.sw ? `<span class="sw"></span>` : ico(b.icon!)}${b.label}</button>`).join("");
  }
  for (const b of BTNS) if (b.sw) el.querySelector(`[data-b="${b.k}"]`)!.setAttribute("aria-pressed", String(b.sw()));
}
$("#btns").onclick = (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>("[data-b]");
  if (!b) return;
  const k = b.dataset.b!;
  switch (k) {
    case "sync": toggleSync(); return;
    case "gridctl": toggleGridCtl(); return;
    case "all": selectAllPhones(); return;
    case "none": toggleSel(state.devices.map((d) => d.id), false); return;
    case "pin":
      if (prefs.big) closeBig(); else openBig(masterId() || activeId || state.devices[0]?.id || "");
      return;
    case "stats":
      prefs.stats = !prefs.stats; lsSet("iosview.stats", prefs.stats ? "1" : "0");
      renderButtons(); $("#netTotal").hidden = !prefs.stats; return;
    case "shot": {
      const sel = [...tiles.values()].filter((t) => t.dev.inSync && t.dev.status === "online");
      shoot(sel.length ? sel : [masterTile()!].filter(Boolean));
      return;
    }
  }
  const d = groupTarget();
  if (d === null) return;
  if (k === "paste") pasteFromPC(d);
  else send({ t: "btn", d, b: k });
};
$("#textForm").onsubmit = (e) => {
  e.preventDefault();
  const inp = $<HTMLInputElement>("#textInput");
  if (!inp.value) return;
  const d = groupTarget();
  if (d === null) return;
  send({ t: "text", d, s: inp.value });
  inp.value = "";
};

// sliders
const sStage = $<HTMLInputElement>("#sStage"), sTile = $<HTMLInputElement>("#sTile");
const sQ = $<HTMLInputElement>("#sQ"), sFQ = $<HTMLInputElement>("#sFQ");
sStage.value = String(prefs.stage); sTile.value = String(prefs.tileW);
function applyTile() {
  document.documentElement.style.setProperty("--tile-w", sTile.value + "px");
  $("#oTile").textContent = sTile.value;
  $("#oStage").textContent = sStage.value + "%";
}
sTile.oninput = () => { prefs.tileW = +sTile.value; lsSet("iosview.tile", sTile.value); applyTile(); };
sStage.oninput = () => { prefs.stage = +sStage.value; lsSet("iosview.stage", sStage.value); applyTile(); const s = stagedTile(); if (s) sizeStage(s); };
applyTile();
let qDragging = false;
function syncSliders() {
  if (qDragging) return;
  sQ.value = String(state.quality); sFQ.value = String(state.focusQuality);
  $("#oQ").textContent = sQ.value; $("#oFQ").textContent = sFQ.value;
}
function sendSettings(over: Partial<{ pw: string; port: number; q: number; fq: number; strip: boolean }> = {}) {
  send({ t: "settings", pw: state.password, port: state.vncPort, q: state.quality, fq: state.focusQuality, strip: state.stripAccents, ...over });
}
for (const [el, out, key] of [[sQ, "#oQ", "q"], [sFQ, "#oFQ", "fq"]] as const) {
  el.oninput = () => { qDragging = true; $(out).textContent = el.value; };
  el.onchange = () => {
    qDragging = false;
    if (key === "q") state.quality = +el.value; else state.focusQuality = +el.value;
    for (const t of tiles.values()) t.setQuality(desiredQuality(t));
    sendSettings({ [key]: +el.value });
  };
}

// filter tabs
$("#filt").onclick = (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>("[data-f]");
  if (!b) return;
  prefs.filter = b.dataset.f!;
  document.querySelectorAll("#filt button").forEach((x) => x.setAttribute("aria-selected", String(x === b)));
  for (const t of tiles.values()) t.paint();
};

// panel tabs
document.querySelectorAll<HTMLElement>(".tabs [data-tab]").forEach((b) => {
  b.onclick = () => {
    document.querySelectorAll(".tabs [data-tab]").forEach((x) => x.setAttribute("aria-selected", String(x === b)));
    document.querySelectorAll<HTMLElement>("[data-pane]").forEach((p) => { p.hidden = p.dataset.pane !== b.dataset.tab; });
    if (b.dataset.tab === "set") fillSettings();
  };
});

// connection tab
$("#scanBtn").onclick = () => {
  $("#scanBtn").setAttribute("disabled", "");
  toast("Đang quét mạng LAN…");
  send({ t: "scan" });
};
$("#wifiForm").onsubmit = (e) => {
  e.preventDefault();
  const ta = $<HTMLTextAreaElement>("#wifiAddr");
  const list = ta.value.split(/[\s,;]+/).map((s) => s.trim()).filter(Boolean);
  if (!list.length) return;
  for (const a of list) send({ t: "addwifi", s: a });
  ta.value = "";
  toast(`Đã thêm ${list.length} địa chỉ Wi‑Fi`);
};

// settings tab
function fillSettings() {
  $<HTMLInputElement>("#setPw").value = state.password;
  $<HTMLInputElement>("#setPort").value = String(state.vncPort);
  $<HTMLInputElement>("#setH264").checked = state.h264;
  $<HTMLInputElement>("#setStrip").checked = state.stripAccents;
}
$("#setForm").onsubmit = (e) => {
  e.preventDefault();
  const h264 = $<HTMLInputElement>("#setH264").checked;
  if (h264 !== state.h264) send({ t: "h264", on: h264 });
  sendSettings({
    pw: $<HTMLInputElement>("#setPw").value,
    port: +$<HTMLInputElement>("#setPort").value || state.vncPort,
    strip: $<HTMLInputElement>("#setStrip").checked,
  });
  toast("Đã lưu cài đặt");
};

// collapse + theme
$("#collapseBtn").innerHTML = ico("chev");
const ctrl = $("#ctrl");
if (lsGet("iosview.collapsed") === "1") ctrl.classList.add("collapsed");
const paintCollapse = () => { $("#collapseBtn").innerHTML = ico(ctrl.classList.contains("collapsed") ? "chevl" : "chev"); };
paintCollapse();
$("#collapseBtn").onclick = () => {
  ctrl.classList.toggle("collapsed");
  lsSet("iosview.collapsed", ctrl.classList.contains("collapsed") ? "1" : "0");
  paintCollapse();
};
const isDark = () => {
  const t = document.documentElement.dataset.theme;
  return t ? t === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
};
const paintTheme = () => { $("#themeBtn").innerHTML = ico(isDark() ? "sun" : "moon"); };
paintTheme();
$("#themeBtn").onclick = () => {
  const t = isDark() ? "light" : "dark";
  document.documentElement.dataset.theme = t;
  lsSet("iosview.theme", t);
  paintTheme();
};

// ---------------------------------------------------------------- stats & visibility
let lastTick = performance.now();
setInterval(() => {
  const now = performance.now(), dt = (now - lastTick) / 1000;
  lastTick = now;
  let total = 0;
  for (const t of tiles.values()) { t.tickStats(dt); total += t.bps; }
  const el = $("#netTotal");
  el.hidden = !prefs.stats;
  if (prefs.stats) el.textContent = "↓ " + fmtRate(total);
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

// ---------------------------------------------------------------- shortcuts
// Full list: docs/SHORTCUTS.md. The boxed ones can be changed by the user
// (Cài đặt → Phím tắt…) and are stored with the phrases and notes in
// iosview.json ("ui" key) through /api/ui.
type Act = "gridctl" | "force" | "paste" | "phrases" | "settings" | "notes" | "notesClose";
const ACTS: { a: Act; label: string; def: string }[] = [
  { a: "gridctl", label: "Điều khiển màn hình nhỏ", def: "Ctrl+S" },
  { a: "force", label: "Dán cưỡng bức", def: "Ctrl+D" },
  { a: "paste", label: "Dán văn bản", def: "Ctrl+V" },
  { a: "phrases", label: "Cụm từ nhanh", def: "Ctrl+K" },
  { a: "settings", label: "Mở cài đặt", def: "Ctrl+," },
  { a: "notes", label: "Mở ghi chú", def: "Ctrl+R" },
  { a: "notesClose", label: "Đóng ghi chú", def: "Ctrl+Q" },
];
const defaultKeys = () => Object.fromEntries(ACTS.map((x) => [x.a, x.def])) as Record<Act, string>;
let keys = defaultKeys();
// Fixed shortcuts a custom one may not take.
const RESERVED = new Set(["Ctrl+A", "Ctrl+C", "Ctrl+X", "Ctrl+Z", "Ctrl+Y", "Ctrl+Backspace", "Ctrl+Delete",
  "Ctrl+ArrowLeft", "Ctrl+ArrowRight", "Ctrl+ArrowUp", "Ctrl+ArrowDown", "Ctrl+=", "Ctrl+-", "Ctrl+0",
  "F2", "F4", "F5", "F11", "F12", "Escape", "Alt+F4"]);

const CODE_KEYS: Record<string, string> = {
  Comma: ",", Period: ".", Slash: "/", Semicolon: ";", Quote: "'", BracketLeft: "[", BracketRight: "]",
  Backslash: "\\", Backquote: "`", Minus: "-", Equal: "=", Space: "Space",
};
// "Ctrl+Shift+K" style name of a key press, layout-independent for letters/digits.
function comboOf(e: KeyboardEvent) {
  let k = e.key;
  if (/^Key[A-Z]$/.test(e.code)) k = e.code.slice(3);
  else if (/^Digit\d$/.test(e.code)) k = e.code.slice(5);
  else if (CODE_KEYS[e.code]) k = CODE_KEYS[e.code];
  else if (k.length === 1) k = k.toUpperCase();
  const m: string[] = [];
  if (e.ctrlKey || e.metaKey) m.push("Ctrl");
  if (e.altKey) m.push("Alt");
  if (e.shiftKey) m.push("Shift");
  return [...m, k].join("+");
}
const fmtKey = (c: string) => c.split("+").join(" + ").replace("Arrow", "");
function actOf(combo: string): Act | null {
  for (const x of ACTS) if (keys[x.a] === combo) return x.a;
  return null;
}
function validKey(combo: string, act: Act): string {
  const parts = combo.split("+");
  const key = parts[parts.length - 1];
  if (!/^F\d{1,2}$/.test(key) && !parts.includes("Ctrl") && !parts.includes("Alt")) return "Phím tắt phải có Ctrl hoặc Alt (hoặc là phím F1–F12)";
  if (RESERVED.has(combo)) return `${fmtKey(combo)} đang là phím cố định`;
  const other = ACTS.find((x) => x.a !== act && keys[x.a] === combo);
  if (other) return `${fmtKey(combo)} đang dùng cho “${other.label}”`;
  return "";
}

function runAct(act: Act, e: KeyboardEvent) {
  const one = e.altKey && !keys[act].includes("Alt");
  switch (act) {
    case "gridctl": toggleGridCtl(); return;
    case "force":
    case "paste": {
      const d = keyTarget();
      if (!d) { toast("Trỏ chuột vào màn hình iPhone trước"); return; }
      if (act === "paste" && keys.paste === "Ctrl+V") return;   // handled by the paste event
      pasteFromPC(d, { force: act === "force", one });
      return;
    }
    case "phrases": openPhrases(); return;
    case "settings": openSettings(); return;
    case "notes": openNotes(); return;
    case "notesClose": closeNotes(); return;
  }
}

// Alt held: individual control (only the phone acted on).
function setSolo(on: boolean) {
  if (document.body.classList.contains("solo") === on) return;
  document.body.classList.toggle("solo", on);
  $("#soloChip").hidden = !on;
}

function toggleGridCtl() {
  prefs.gridCtl = !prefs.gridCtl;
  lsSet("iosview.gridctl", prefs.gridCtl ? "1" : "0");
  document.body.classList.toggle("grid-view-only", !prefs.gridCtl);
  renderButtons();
  toast(prefs.gridCtl ? `Chạm trực tiếp trên màn nhỏ: BẬT (${fmtKey(keys.gridctl)})` : `Màn nhỏ chỉ để xem — nhấp để mở lên màn lớn (${fmtKey(keys.gridctl)})`);
}
document.body.classList.toggle("grid-view-only", !prefs.gridCtl);

function openSettings() {
  if (ctrl.classList.contains("collapsed")) { ctrl.classList.remove("collapsed"); lsSet("iosview.collapsed", "0"); paintCollapse(); }
  $('[data-tab="set"]').click();
}

function selectAllPhones() {
  toggleSel(state.devices.map((d) => d.id), true);
  toast(`Đã chọn tất cả ${state.devices.length} máy`);
}

// Phones in grid order, as shown (filter applied).
function visibleOrder(): Tile[] {
  return [...tiles.values()].filter((t) => !t.el.hidden).sort((a, b) => +a.el.style.order - +b.el.style.order);
}
// Ctrl + arrows: big screen (or full view) to the previous / next phone.
function switchBig(dir: number) {
  const list = visibleOrder();
  if (!list.length) return;
  const step = (cur: Tile | null) => list[((cur ? list.indexOf(cur) : -1) + dir + list.length) % list.length];
  if (fullTile) { openFull(step(fullTile)); return; }
  if (!prefs.big) { openBig(hoverId || activeId || masterId() || list[0].dev.id); return; }
  const cur = masterTile();
  openBig(step(cur && list.includes(cur) ? cur : null).dev.id);
}
function closeBigOrFull() { if (fullTile) closeFull(); else closeBig(); }

// ---------------------------------------------------------------- selecting phones on the grid
// Shift+click = select a range · Ctrl+click = toggle · Ctrl+drag = box select ·
// click on empty space = deselect all · right click on empty space = close big screen.
let selAnchor = "";
let swallowClick = false;   // the click ending a selection gesture must not also toggle
window.addEventListener("click", (e) => {
  if (!swallowClick) return;
  swallowClick = false;
  e.stopPropagation(); e.preventDefault();
}, true);
function tileOf(el: Element | null): Tile | undefined {
  const te = el?.closest(".tile");
  return te ? [...tiles.values()].find((t) => t.el === te) : undefined;
}
function onGridScrollbar(e: MouseEvent) {
  const r = grid.getBoundingClientRect();
  return e.clientX >= r.left + grid.clientLeft + grid.clientWidth || e.clientY >= r.top + grid.clientTop + grid.clientHeight;
}
grid.addEventListener("pointerdown", (e) => {
  swallowClick = false;
  if (e.button !== 0) return;
  const tg = e.target as HTMLElement;
  if (tg.closest("input, button")) return;
  const t = tileOf(tg);
  if (e.shiftKey && t && !e.ctrlKey && !e.metaKey) {
    e.preventDefault(); e.stopPropagation(); swallowClick = true;
    rangeSelect(t);
    return;
  }
  if ((e.ctrlKey || e.metaKey) && !onGridScrollbar(e)) {
    e.preventDefault(); e.stopPropagation(); swallowClick = true;
    boxSelect(e, t);
    return;
  }
  if (tg === grid && !onGridScrollbar(e)) {
    const x0 = e.clientX, y0 = e.clientY;
    window.addEventListener("pointerup", (u) => {
      if (Math.hypot(u.clientX - x0, u.clientY - y0) > 5) return;
      const sel = state.devices.filter((d) => d.inSync);
      if (!sel.length) return;
      toggleSel(sel.map((d) => d.id), false);
      toast(`Đã bỏ chọn ${sel.length} máy`);
    }, { once: true });
  }
}, true);
grid.addEventListener("contextmenu", (e) => {
  if (e.target !== grid) return;
  e.preventDefault();
  closeBigOrFull();
});
$("#stage").addEventListener("contextmenu", (e) => {
  if ((e.target as HTMLElement).closest(".host")) return;
  e.preventDefault();
  closeBig();
});
$("#focus").addEventListener("contextmenu", (e) => {
  if ((e.target as HTMLElement).closest(".host, .focus-head")) return;
  e.preventDefault();
  closeFull();
});

function rangeSelect(t: Tile) {
  const list = visibleOrder();
  const i = list.indexOf(t), j = list.findIndex((x) => x.dev.id === selAnchor);
  if (j < 0 || i < 0) { selAnchor = t.dev.id; toggleSel([t.dev.id], true); return; }
  const lo = Math.min(i, j), hi = Math.max(i, j);
  const ids = list.slice(lo, hi + 1).map((x) => x.dev.id);
  toggleSel(ids, true);
  toast(`Đã chọn ${ids.length} máy (${pad(list[lo].dev.no)} → ${pad(list[hi].dev.no)})`);
}

function boxSelect(e: PointerEvent, t0?: Tile) {
  const x0 = e.clientX, y0 = e.clientY;
  let box: HTMLElement | null = null;
  let hit: Tile[] = [];
  const move = (ev: PointerEvent) => {
    const x1 = ev.clientX, y1 = ev.clientY;
    if (!box) {
      if (Math.hypot(x1 - x0, y1 - y0) < 6) return;
      box = document.createElement("div");
      box.className = "marquee";
      document.body.appendChild(box);
    }
    const l = Math.min(x0, x1), tp = Math.min(y0, y1), w = Math.abs(x1 - x0), h = Math.abs(y1 - y0);
    Object.assign(box.style, { left: l + "px", top: tp + "px", width: w + "px", height: h + "px" });
    hit = visibleOrder().filter((x) => {
      const r = x.el.getBoundingClientRect();
      return r.right > l && r.left < l + w && r.bottom > tp && r.top < tp + h;
    });
    for (const x of tiles.values()) x.el.classList.toggle("marq", hit.includes(x));
  };
  const end = () => {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", end);
    window.removeEventListener("pointercancel", end);
    for (const x of tiles.values()) x.el.classList.remove("marq");
    if (!box) {                         // Ctrl + click / press-and-hold: toggle this phone
      if (t0) { selAnchor = t0.dev.id; toggleSel([t0.dev.id]); }
      return;
    }
    box.remove();
    if (hit.length) { toggleSel(hit.map((x) => x.dev.id), true); toast(`Đã chọn ${hit.length} máy trong khung`); }
  };
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", end);
  window.addEventListener("pointercancel", end);
}

// Mouse wheel over a slider moves it (size and quality) — but only when the
// mouse is resting on it. A wheel that is scrolling the panel and merely
// passes over a slider must keep scrolling: in 0.10.3 that silently dragged
// the quality sliders down to 0 (blurry, stuttering picture).
let panelScrollUntil = 0;
window.addEventListener("wheel", (e) => {
  if (!(e.target as HTMLElement).matches?.('input[type="range"]')) panelScrollUntil = performance.now() + 500;
}, { capture: true, passive: true });
document.querySelectorAll<HTMLInputElement>('input[type="range"]').forEach((r) => {
  let t = 0;
  let restingSince = Infinity;
  r.addEventListener("pointerenter", () => { restingSince = performance.now(); });
  r.addEventListener("pointerleave", () => { restingSince = Infinity; });
  r.addEventListener("wheel", (e) => {
    if (e.ctrlKey) return;
    const now = performance.now();
    if (now < panelScrollUntil || now - restingSince < 300) { panelScrollUntil = now + 500; return; }
    e.preventDefault();
    const st = +r.step || 1;
    const v = Math.min(+r.max, Math.max(+r.min, +r.value + (e.deltaY < 0 ? st : -st)));
    if (v === +r.value) return;
    r.value = String(v);
    r.dispatchEvent(new Event("input"));
    clearTimeout(t);
    t = window.setTimeout(() => r.dispatchEvent(new Event("change")), 250);
  }, { passive: false });
});

// ---------------------------------------------------------------- UI data (keys, phrases, notes)
type UIData = { keys?: Partial<Record<Act, string>>; phrases?: string[]; notes?: string };
let ui: UIData = {};
let uiTimer = 0;
function putUI(keepalive = false) {
  clearTimeout(uiTimer); uiTimer = 0;
  return fetch("/api/ui", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(ui), keepalive })
    .then((r) => { if (!r.ok) throw new Error(); })
    .catch(() => toast("Không lưu được ghi chú / phím tắt"));
}
function saveUI() { clearTimeout(uiTimer); uiTimer = window.setTimeout(() => putUI(), 500); }
window.addEventListener("pagehide", () => { if (uiTimer) putUI(true); });
function applyKeys() {
  keys = defaultKeys();
  for (const x of ACTS) {
    const k = ui.keys?.[x.a];
    if (k && k !== keys[x.a] && !validKey(k, x.a)) keys[x.a] = k;
  }
}
fetch("/api/ui", { cache: "no-store" }).then((r) => r.json()).then((v: UIData) => {
  ui = v && typeof v === "object" ? v : {};
  applyKeys();
  const ta = $<HTMLTextAreaElement>("#notesText");
  if (!ta.value) ta.value = ui.notes || "";
}).catch(() => { /* defaults */ });

// ---------------------------------------------------------------- notes
function notesOpen() { return !$("#notes").hidden; }
function openNotes() {
  $("#notesHint").textContent = `tự lưu · ${fmtKey(keys.notesClose)} để đóng`;
  $("#notes").hidden = false;
  $("#notesText").focus();
}
function closeNotes() {
  if (!notesOpen()) return;
  $("#notes").hidden = true;
  if (uiTimer) putUI();
  (document.activeElement as HTMLElement)?.blur?.();
}
$("#notesText").addEventListener("input", () => { ui.notes = $<HTMLTextAreaElement>("#notesText").value; saveUI(); });
$("#notesClose").onclick = closeNotes;

// ---------------------------------------------------------------- quick phrases
let phTarget = "";
function phrases(): string[] { return Array.isArray(ui.phrases) ? ui.phrases : []; }
function renderPhrases() {
  const list = phrases();
  $("#phList").innerHTML = list.length
    ? list.map((p, i) => `<li><button class="ph-send" data-ph="${i}">${i < 9 ? `<kbd>${i + 1}</kbd>` : `<kbd></kbd>`}<span>${esc(p)}</span></button><button class="ib" data-phdel="${i}" title="Xoá">${ico("trash")}</button></li>`).join("")
    : `<li class="ph-empty">Chưa có cụm từ nào — thêm ở ô bên dưới.</li>`;
  const t = tiles.get(phTarget);
  $("#phTarget").textContent = !t ? "· trỏ chuột vào một máy rồi mở lại để gửi"
    : state.syncOn && t.dev.inSync ? `· gửi tới cả nhóm (${syncCount()} máy)` : `· gửi tới ${pad(t.dev.no)} · ${t.label()}`;
}
function openPhrases() {
  phTarget = keyTarget();
  renderPhrases();
  const dlg = $<HTMLDialogElement>("#phrases");
  if (!dlg.open) dlg.showModal();
  $<HTMLInputElement>("#phInput").value = "";
  (dlg.querySelector<HTMLElement>(".ph-send") || $("#phInput")).focus();
}
function sendPhrase(i: number) {
  const p = phrases()[i];
  if (p === undefined) return;
  if (!phTarget || !tiles.has(phTarget)) { toast("Trỏ chuột vào màn hình iPhone rồi mở lại cụm từ nhanh"); return; }
  $<HTMLDialogElement>("#phrases").close();
  pasteText(phTarget, p);
}
$("#phList").addEventListener("click", (e) => {
  const el = e.target as HTMLElement;
  const del = el.closest<HTMLElement>("[data-phdel]");
  if (del) { const l = phrases().slice(); l.splice(+del.dataset.phdel!, 1); ui.phrases = l; saveUI(); renderPhrases(); return; }
  const b = el.closest<HTMLElement>("[data-ph]");
  if (b) sendPhrase(+b.dataset.ph!);
});
$("#phrases").addEventListener("keydown", (e) => {
  if (isTyping(e)) return;
  // stopPropagation: once the dialog closes, the window handler must not
  // also type the digit into the phone
  if (/^[1-9]$/.test(e.key) && !e.ctrlKey && !e.altKey) { e.preventDefault(); e.stopPropagation(); sendPhrase(+e.key - 1); }
});
$("#phForm").onsubmit = (e) => {
  e.preventDefault();
  const inp = $<HTMLInputElement>("#phInput");
  const v = inp.value.trim();
  if (!v) return;
  ui.phrases = [...phrases(), v];
  saveUI();
  inp.value = "";
  renderPhrases();
};
$("#phClose").onclick = () => $<HTMLDialogElement>("#phrases").close();

// ---------------------------------------------------------------- shortcut list / editor
const FIXED: [string, [string, string, string?][]][] = [
  ["Chung", [
    ["Chọn tất cả điện thoại", "Ctrl + A"],
    ["Chọn liên tục điện thoại", "Shift + nhấp chuột trái"],
    ["Hủy chọn điện thoại đã chọn", "Nhấp chuột trái vào chỗ trống"],
    ["Điều chỉnh kích thước màn hình nhỏ", "Ctrl + cuộn chuột"],
    ["Điều chỉnh kích thước và chất lượng hình", "Rê chuột lên thanh trượt + cuộn chuột"],
    ["Điều khiển riêng lẻ từng điện thoại", "Giữ Alt"],
    ["Điều khiển bằng hai ngón tay", "Shift", "chưa hỗ trợ"],
    ["Chuyển đổi nhanh màn hình lớn", "Ctrl + phím mũi tên"],
    ["Đóng màn hình lớn", "Nhấp chuột phải vào chỗ trống"],
  ]],
  ["Chế độ điều khiển màn hình lớn", [
    ["Mở đa nhiệm của điện thoại", "Nhấn và giữ chuột phải"],
    ["Quay về trang chủ điện thoại", "Nhấp chuột giữa"],
    ["Quay lại cấp trên của điện thoại", "Nhấp chuột phải"],
    ["Quay lại cấp trên của điện thoại", "Esc"],
  ]],
  ["Chế độ điều khiển màn hình nhỏ", [
    ["Chọn điện thoại", "Ctrl + nhấn chuột trái"],
    ["Chọn khung điện thoại", "Ctrl + kéo chuột trái"],
    ["Mở đa nhiệm của điện thoại", "Nhấn và giữ chuột phải"],
    ["Quay về trang chủ điện thoại", "Nhấp chuột giữa"],
    ["Quay lại cấp trên của điện thoại", "Esc"],
  ]],
  ["Riêng iOSView", [
    ["Đổi tên máy đang trỏ", "F2"],
    ["Bật / tắt đồng bộ", "F4"],
    ["Xoá nội dung ô nhập trên iPhone", "Ctrl + Backspace"],
    ["Hoàn tác / làm lại trên iPhone", "Ctrl + Z / Ctrl + Y"],
    ["Tải lại cửa sổ", "F5"],
  ]],
];
let capturing: Act | null = null;
function renderKeys() {
  const row = (label: string, key: string, note?: string) =>
    `<li><span>${esc(label)}${note ? ` <em>${esc(note)}</em>` : ""}</span><span class="k">${esc(key)}</span></li>`;
  let h = "";
  FIXED.forEach(([title, rows], i) => {
    h += `<h3>${esc(title)}</h3><ul>` + rows.map((r) => row(...r)).join("");
    if (i === 0) {
      h += ACTS.map((x) => `<li><span>${esc(x.label)}</span><button class="kcap${capturing === x.a ? " on" : ""}" data-act="${x.a}">${capturing === x.a ? "Nhấn tổ hợp phím… (Esc huỷ)" : esc(fmtKey(keys[x.a]))}</button></li>`).join("");
    }
    h += "</ul>";
  });
  $("#keysBody").innerHTML = h;
}
function openKeys() { capturing = null; renderKeys(); $<HTMLDialogElement>("#keysDlg").showModal(); }
$("#keysBody").addEventListener("click", (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>("[data-act]");
  if (!b) return;
  capturing = capturing === b.dataset.act ? null : b.dataset.act as Act;
  renderKeys();
});
$("#keysReset").onclick = () => {
  capturing = null;
  delete ui.keys;
  keys = defaultKeys();
  saveUI(); renderKeys(); renderButtons();
  toast("Đã khôi phục phím tắt mặc định");
};
$("#keysClose").onclick = () => $<HTMLDialogElement>("#keysDlg").close();
$("#keysDlg").addEventListener("close", () => { capturing = null; });
$("#keysBtn").onclick = openKeys;
// Recording a new shortcut: grab the next key press before anything else sees it.
window.addEventListener("keydown", (e) => {
  if (!capturing) return;
  e.preventDefault(); e.stopImmediatePropagation();
  if (e.key === "Escape") { capturing = null; renderKeys(); return; }
  if (["Control", "Shift", "Alt", "Meta", "CapsLock"].includes(e.key)) return;   // wait for the key
  const combo = comboOf(e);
  const err = validKey(combo, capturing);
  if (err) { toast(err); return; }
  keys[capturing] = combo;
  ui.keys = Object.fromEntries(ACTS.filter((x) => keys[x.a] !== x.def).map((x) => [x.a, keys[x.a]]));
  toast(`${ACTS.find((x) => x.a === capturing)!.label}: ${fmtKey(combo)}`);
  capturing = null;
  saveUI(); renderKeys(); renderButtons();
}, true);

// ---------------------------------------------------------------- zoom
// Ctrl+wheel (and Ctrl + / - / 0) resizes phone screens: over the big screen it
// resizes the big screen, anywhere else the grid tiles. The app window itself
// never zooms.
function zoomAt(dir: number, x: number, y: number, target: Element | null) {
  if (target?.closest("#stage")) {
    const v = dir === 0 ? 100 : Math.min(100, Math.max(40, prefs.stage + dir * 5));
    sStage.value = String(v); sStage.dispatchEvent(new Event("input"));
    return;
  }
  // keep the phone under the cursor in place while the grid reflows
  const tileEl = target?.closest<HTMLElement>(".tile");
  const before = tileEl?.getBoundingClientRect();
  const v = dir === 0 ? 150 : Math.min(+sTile.max, Math.max(+sTile.min, prefs.tileW + dir * 10));
  if (v === prefs.tileW) return;
  sTile.value = String(v); sTile.dispatchEvent(new Event("input"));
  if (tileEl && before) {
    const after = tileEl.getBoundingClientRect();
    const fy = (y - before.top) / (before.height || 1);
    grid.scrollTop += (after.top + fy * after.height) - y;
  }
}
window.addEventListener("wheel", (e) => {
  if (!e.ctrlKey) return;
  e.preventDefault();
  e.stopPropagation();       // not a scroll on the phone
  if (fullTile) return;
  zoomAt(e.deltaY < 0 ? 1 : -1, e.clientX, e.clientY, e.target as Element);
}, { capture: true, passive: false });
let lastMouse = { x: 0, y: 0 };
window.addEventListener("pointermove", (e) => { lastMouse = { x: e.clientX, y: e.clientY }; }, { passive: true });
window.addEventListener("keydown", (e) => {
  if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
  const k = e.key;
  const dir = k === "+" || k === "=" ? 1 : k === "-" || k === "_" ? -1 : k === "0" ? 0 : null;
  if (dir === null) return;
  e.preventDefault();
  e.stopImmediatePropagation();
  if (fullTile) return;
  zoomAt(dir, lastMouse.x, lastMouse.y, document.elementFromPoint(lastMouse.x, lastMouse.y));
}, { capture: true });

let toastTimer = 0;
function toast(s: string) {
  const el = $("#toast");
  el.textContent = s;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (el.hidden = true), 3200);
}

renderButtons();
connectControl();
fetch("/api/version", { cache: "no-store" }).then((r) => r.json()).then((v) => {
  if (!v.version) return;
  $("#appVer").textContent = "v" + v.version;
  document.title = "iOSView " + v.version;
}).catch(() => { /* ignore */ });

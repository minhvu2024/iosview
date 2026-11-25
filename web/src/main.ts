// iOSView UI — grid of live iPhone screens (noVNC / H.264, view-only) with our
// own input layer. All input goes to the Go agent over one WebSocket; the agent
// fans it out to the sync group with per-device low-latency connections.
//
// Layout: grid of phones · big screen of the main phone · its functions ·
// control panel (buttons, sliders, tags).
import RFB from "@novnc/novnc";
import { H264View, h264Supported, type Profile } from "./h264";

type Dev = {
  id: string; name: string; no: number; vncName: string; transport: "usb" | "wifi"; addr: string;
  kind?: "cd"; ios?: string;   // kind "cd": iOS 17+ without jailbreak (CoreDevice, HEVC)
  status: "connecting" | "online" | "offline" | "error"; error?: string;
  width: number; height: number; inSync: boolean;
  udid?: string; wifi?: string;   // wifi = LAN address of the same phone (fallback path)
};
type Tag = { name: string; ids: string[] };
type State = {
  devices: Dev[]; wifiDevices?: string[]; tags: Tag[]; syncOn: boolean; usbError?: string; password: string;
  quality: number; focusQuality: number; h264: boolean; vncPort: number; stripAccents: boolean;
};

const $ = <T extends HTMLElement = HTMLElement>(s: string) => document.querySelector(s) as T;
const grid = $("#grid");
// Wails build (0.11.0): the page is served from http://wails.localhost, so the
// agent's WebSockets are reached via the real TCP port given as ?ws=host:port.
// In a browser (or the WebView2 build) the page and the sockets share a host.
const PARAMS = new URLSearchParams(location.search);
const WS_HOST = PARAMS.get("ws") || location.host;
const WAILS = location.hostname === "wails.localhost";
let state: State = { devices: [], tags: [], syncOn: true, password: "", quality: 5, focusQuality: 7, h264: true, vncPort: 5901, stripAccents: true };
// Quality for grid tiles hidden behind the full-window view: still streaming
// (so they come back instantly) but as cheap as possible for phone, Wi-Fi, PC.
let pageHidden = false;       // app window minimised / in background
const lsGet = (k: string) => { try { return localStorage.getItem(k) || ""; } catch { return ""; } };
const lsSet = (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* ignore */ } };
const prefs = {
  copyBlank: lsGet("iosview.copyblank") === "1",
  stats: lsGet("iosview.stats") === "1",
  big: lsGet("iosview.big") === "1",   // big screen open (closed by default)
  zoomTap: lsGet("iosview.zoomtap") === "1",   // Ctrl+S: click a small screen = show it big (off = control it)
  tileW: +lsGet("iosview.tile") || 150,
  stage: +lsGet("iosview.stage") || 100,
  master: lsGet("iosview.master"),
  filter: "all",
  tag: "",                    // grid filtered to this tag ("" = none)
};
let activeId = "";            // last device the user touched
let hoverId = "";             // device under the mouse

const pad = (n: number) => String(n || 0).padStart(2, "0");
const esc = (s: string) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]!));

// ---------------------------------------------------------------- icons
const IC: Record<string, string> = {
  back: `<path d="m12 19-7-7 7-7" /><path d="M19 12H5" />`,
  home: `<circle cx="12" cy="12" r="7" />`,
  switcher: `<path d="M2 3v18" /><rect width="12" height="18" x="6" y="3" rx="2" /><path d="M22 3v18" />`,
  power: `<path d="M12 2v10" /><path d="M18.4 6.6a9 9 0 1 1-12.77.04" />`,
  volup: `<path d="M11 4.702a.705.705 0 0 0-1.203-.498L6.413 7.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.383 3.384A.705.705 0 0 0 11 19.298z" /><path d="M16 9a5 5 0 0 1 0 6" /><path d="M19.364 18.364a9 9 0 0 0 0-12.728" />`,
  voldown: `<path d="M11 4.702a.705.705 0 0 0-1.203-.498L6.413 7.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.383 3.384A.705.705 0 0 0 11 19.298z" /><path d="M16 9a5 5 0 0 1 0 6" />`,
  mute: `<path d="M11 4.702a.705.705 0 0 0-1.203-.498L6.413 7.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.383 3.384A.705.705 0 0 0 11 19.298z" /><line x1="22" x2="16" y1="9" y2="15" /><line x1="16" x2="22" y1="9" y2="15" />`,
  paste: `<path d="M11 14h10" /><path d="M16 4h2a2 2 0 0 1 2 2v1.344" /><path d="m17 18 4-4-4-4" /><path d="M8 4H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 1.793-1.113" /><rect x="8" y="2" width="8" height="4" rx="1" />`,
  selectall: `<path d="M14 21h1" /><path d="M14 3h1" /><path d="M19 3a2 2 0 0 1 2 2" /><path d="M21 14v1" /><path d="M21 19a2 2 0 0 1-2 2" /><path d="M21 9v1" /><path d="M3 14v1" /><path d="M3 9v1" /><path d="M5 21a2 2 0 0 1-2-2" /><path d="M5 3a2 2 0 0 0-2 2" /><path d="M7 12h10" /><path d="M7 16h6" /><path d="M7 8h8" /><path d="M9 21h1" /><path d="M9 3h1" />`,
  clear: `<path d="M10 5a2 2 0 0 0-1.344.519l-6.328 5.74a1 1 0 0 0 0 1.481l6.328 5.741A2 2 0 0 0 10 19h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2z" /><path d="m12 9 6 6" /><path d="m18 9-6 6" />`,
  shot: `<path d="M13.997 4a2 2 0 0 1 1.76 1.05l.486.9A2 2 0 0 0 18.003 7H20a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2h1.997a2 2 0 0 0 1.759-1.048l.489-.904A2 2 0 0 1 10.004 4z" /><circle cx="12" cy="13" r="3" />`,
  rename: `<path d="M13 21h8" /><path d="m15 5 4 4" /><path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z" />`,
  expand: `<path d="M15 3h6v6" /><path d="m21 3-7 7" /><path d="m3 21 7-7" /><path d="M9 21H3v-6" />`,
  full: `<path d="M8 3H5a2 2 0 0 0-2 2v3" /><path d="M21 8V5a2 2 0 0 0-2-2h-3" /><path d="M3 16v3a2 2 0 0 0 2 2h3" /><path d="M16 21h3a2 2 0 0 0 2-2v-3" />`,
  hash: `<line x1="4" x2="20" y1="9" y2="9" /><line x1="4" x2="20" y1="15" y2="15" /><line x1="10" x2="8" y1="3" y2="21" /><line x1="16" x2="14" y1="3" y2="21" />`,
  unlink: `<path d="M15 7h2a5 5 0 0 1 0 10h-2m-6 0H7A5 5 0 0 1 7 7h2" />`,
  link: `<path d="M9 17H7A5 5 0 0 1 7 7h2" /><path d="M15 7h2a5 5 0 1 1 0 10h-2" /><line x1="8" x2="16" y1="12" y2="12" />`,
  sync: `<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" /><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" /><path d="M8 16H3v5" />`,
  all: `<path d="M21 10.656V19a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h12.344" /><path d="m9 11 3 3L22 4" />`,
  none: `<rect width="18" height="18" x="3" y="3" rx="2" />`,
  cursor: `<path d="M4.037 4.688a.495.495 0 0 1 .651-.651l16 6.5a.5.5 0 0 1-.063.947l-6.124 1.58a2 2 0 0 0-1.438 1.435l-1.579 6.126a.5.5 0 0 1-.947.063z" />`,
  chev: `<path d="m6 17 5-5-5-5" /><path d="m13 17 5-5-5-5" />`,
  chevl: `<path d="m11 17-5-5 5-5" /><path d="m18 17-5-5 5-5" />`,
  moon: `<path d="M20.985 12.486a9 9 0 1 1-9.473-9.472c.405-.022.617.46.402.803a6 6 0 0 0 8.268 8.268c.344-.215.825-.004.803.401" />`,
  sun: `<circle cx="12" cy="12" r="4" /><path d="M12 2v2" /><path d="M12 20v2" /><path d="m4.93 4.93 1.41 1.41" /><path d="m17.66 17.66 1.41 1.41" /><path d="M2 12h2" /><path d="M20 12h2" /><path d="m6.34 17.66-1.41 1.41" /><path d="m19.07 4.93-1.41 1.41" />`,
  refresh: `<path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" />`,
  x: `<path d="M18 6 6 18" /><path d="m6 6 12 12" />`,
  trash: `<path d="M10 11v6" /><path d="M14 11v6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" /><path d="M3 6h18" /><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />`,
  cc: `<line x1="21" x2="14" y1="4" y2="4" /><line x1="10" x2="3" y1="4" y2="4" /><line x1="21" x2="12" y1="12" y2="12" /><line x1="8" x2="3" y1="12" y2="12" /><line x1="21" x2="16" y1="20" y2="20" /><line x1="12" x2="3" y1="20" y2="20" /><line x1="14" x2="14" y1="2" y2="6" /><line x1="8" x2="8" y1="10" y2="14" /><line x1="16" x2="16" y1="18" y2="22" />`,
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
const REALTIME = new Set(["p", "w", "k", "text", "btn"]);
function send(m: { t: string; [k: string]: unknown }) {
  const s = JSON.stringify(m);
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(s);
    if (REALTIME.has(m.t) && (m.t !== "p" || m.m === 0)) {
      const source = typeof m.d === "string" ? tiles.get(m.d) : undefined;
      const viewers = state.syncOn && !m.one && (!m.d || source?.dev.inSync) ? tiles.values() : source ? [source] : [];
      for (const t of viewers) {
        if ((t === source || t.dev.inSync) && t.rfb instanceof H264View) t.rfb.expectFrame();
      }
    }
    return true;
  }
  if (REALTIME.has(m.t)) {
    if (!offlineWarned) { offlineWarned = true; toast("Mất kết nối tới iOSView — thao tác bị bỏ qua"); }
    return false;
  }
  if (outbox.length < 50) outbox.push(s);
  return false;
}
function connectControl() {
  const sock = new WebSocket(`ws://${WS_HOST}/ws/control`);
  ws = sock;
  sock.onopen = () => {
    controlEpoch++;
    offlineWarned = false;
    sock.send(JSON.stringify({ t: "copyblank", on: prefs.copyBlank }));
    while (outbox.length) sock.send(outbox.shift()!);
    if (wifiDirty) syncWifiFromInput();
  };
  sock.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.t === "state") applyState(m.state);
    else if (m.t === "clipboardCopy") rememberCopy(m.copy);
    else if (m.t === "textAck") textAcks.get(m.seq)?.(m.count);
    else if (m.t === "wifi") finishWifi(m.seq, m.error);
    else if (m.t === "toast") toast(m.s);
  };
  sock.onclose = () => {
    if (ws !== sock) return;   // only one reconnect loop
    controlEpoch++;
    wifiPending = 0;
    for (const done of textAcks.values()) done(null);
    lastGroupCopy = null;
    lastCopySeq = 0;
    setTimeout(connectControl, 1000);
  };
}

// Buttons act on press (pointerdown), not on release, so the phone reacts
// ~100 ms sooner. Keyboard activation (Enter/Space = click with detail 0)
// still works; the mouse's own click after the press is ignored.
function onPress(el: HTMLElement, fn: (e: MouseEvent) => void, stop = false) {
  el.addEventListener("pointerdown", (e) => { if (e.button !== 0) return; if (stop) e.stopPropagation(); fn(e); });
  el.addEventListener("click", (e) => { if (stop) e.stopPropagation(); if (e.detail === 0) fn(e); });
}

// ---------------------------------------------------------------- tiles
const NAV_BUTTONS =
  `<button data-nav="switcher" title="Đa nhiệm">${ico("switcher")}</button>` +
  `<button data-nav="home" title="Home">${ico("home")}</button>` +
  `<button data-nav="back" title="Quay lại (vuốt từ mép trái)">${ico("back")}</button>`;

function bindNav(bar: HTMLElement, id: () => string, onUse?: () => void) {
  bar.querySelectorAll<HTMLElement>("[data-nav]").forEach((b) => {
    onPress(b, (e) => {
      const d = id();
      if (!d) return;
      onUse?.();
      const t = tiles.get(d);
      if (t) navPress(t, b.dataset.nav!, e.altKey);
      else send({ t: "btn", d, b: b.dataset.nav });
    }, true);
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
  occluded = false;       // completely hidden underneath the floating big screen
  wantQuality = state.quality;
  wantProfile: Profile = { fps: VIDEO_DEF.gf, pct: VIDEO_DEF.gs };
  retry = 0;
  retryTimer = 0;
  hasPicture = false;     // true once a frame was drawn during this online session
  pw = "";

  constructor(dev: Dev) {
    this.dev = dev;
    this.el = document.createElement("section");
    this.el.className = "tile";
    tileByEl.set(this.el, this);
    this.el.innerHTML = `
      <div class="t-head" title="Nhấp: chọn / bỏ chọn · Nhấp đúp hoặc ⤢: mở màn lớn · Chuột phải: menu">
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
    // click = toggle selection at once (a double click toggles twice, i.e.
    // leaves it as it was) · double click = big screen / rename / number
    head.onclick = (e) => {
      if ((e.target as HTMLElement).closest("input, .t-big")) return;
      selAnchor = this.dev.id; toggleSel([this.dev.id]);
    };
    onPress(this.el.querySelector<HTMLElement>(".t-big")!, () => openBig(this.dev.id), true);
    head.ondblclick = (e) => {
      if ((e.target as HTMLElement).closest("input")) return;
      const tg = e.target as HTMLElement;
      if (tg.closest(".t-big")) return;
      if (tg.closest(".name")) this.rename();
      else if (tg.closest(".no")) inlineNumber(this.el.querySelector(".no")!, this.dev);
      else openBig(this.dev.id);
    };
    head.oncontextmenu = (e) => { e.preventDefault(); openMenu(this.dev.id, e.clientX, e.clientY); };
    onPress(this.el.querySelector<HTMLElement>(".ctl-ph")!, () => closeBig(), true);
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
      this.hasPicture = false;
      this.host.classList.remove("live");
      this.view.querySelector("canvas.h264")?.remove();
      this.msg.textContent = dev.status === "connecting" ? "Đang kết nối…" : (dev.error || "Mất kết nối");
    } else if (!this.hasPicture) {
      this.msg.textContent = "Đang mở hình…";
    }
    if (dev.status === "online" && was !== "online") this.retry = 0;
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
    // CoreDevice phones only have the HEVC stream (no VNC/JPEG).
    if ((state.h264 || this.dev.kind === "cd") && h264Supported()) this.connectH264();
    else this.connectVNC();
  }

  // Hardware H.264 stream. If no frame arrives quickly (phone runs a TrollVNC
  // without the iOSView H.264 patch), fall back to VNC for this device.
  connectH264() {
    this.wantProfile = desiredProfile(this);
    if (!this.hasPicture) this.msg.textContent = "Đang mở hình…";
    const v = new H264View(this.view, `ws://${WS_HOST}/ws/h264/${encodeURIComponent(this.dev.id)}`, this.wantQuality, this.wantProfile, this.dev.kind === "cd");
    v.onframe = () => { if (this.rfb === v) this.frames++; };
    v.onbytes = (n) => { if (this.rfb === v) this.bytes += n; };
    v.paused = isBackgrounded(this);   // hidden behind full-window or floating big screen
    v.onfirst = () => { clearTimeout(this.firstTimer); this.hasPicture = true; this.host.classList.add("live"); this.retry = 0; };
    v.onclose = () => {
      if (this.rfb !== v) return;
      clearTimeout(this.firstTimer);
      this.rfb = null;
      // Keep the last drawn frame on canvas while the phone itself is still
      // online, so stream hiccups / heavy-video resets never flash black.
      if (this.dev.status !== "online" || !this.hasPicture) {
        this.host.classList.remove("live");
      } else {
        this.host.classList.remove("live");
        this.msg.textContent = "Đang nối lại hình…";
      }
      if (this.dev.status === "online") {
        const wait = Math.min(300 * 2 ** this.retry++, 3000) + Math.round(Math.random() * 250);
        this.retryTimer = window.setTimeout(() => { this.retryTimer = 0; this.sync(); }, wait);
      }
    };
    clearTimeout(this.firstTimer);
    // stream connected (or connecting) but no picture: reconnect it
    this.firstTimer = window.setTimeout(() => { if (this.rfb === v && !v.decodedFrame) v.disconnect(); }, this.dev.kind === "cd" ? 20000 : 6000);
    this.rfb = v;
  }

  connectVNC() {
    this.view.querySelector("canvas.h264")?.remove();
    this.pw = state.password;
    const url = `ws://${WS_HOST}/ws/vnc/${encodeURIComponent(this.dev.id)}`;
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
    const timing = this.rfb instanceof H264View ? this.rfb.timingSummary(!this.stat.hidden && !this.rfb.paused) : "";
    if (this.nameEl.title !== timing) this.nameEl.title = timing;
    if (!this.stat.hidden) {
      const r = this.rfb;
      const p = this.wantProfile;
      this.stat.textContent = `${Math.round(this.fps)} fps · ${fmtRate(this.bps)} · ${r instanceof H264View ? (this.dev.kind === "cd" ? "HEVC" : `H.264 ${p.fps}fps ${p.pct}%`) : "JPEG"}${this.dev.kind === "cd" ? "" : ` q${this.wantQuality}`}` +
        (r instanceof H264View ? ` · trễ ${Math.round(r.lagMs)}ms${r.queue > 1 ? ` · đợi ${r.queue}` : ""}` : "") +
        (r instanceof H264View && r.dropped ? ` · bỏ ${r.dropped}` : "");
    }
  }

  disconnect() {
    clearTimeout(this.firstTimer);
    const r = this.rfb; this.rfb = null;
    if (this.dev.status !== "online" || !this.hasPicture) {
      this.host.classList.remove("live");
    }
    if (r) try { r.disconnect(); } catch { /* already closed */ }
  }

  setQuality(q: number) {
    this.wantQuality = q;
    if (this.rfb) this.rfb.qualityLevel = q;
  }

  setProfile(p: Profile) {
    this.wantProfile = p;
    if (this.rfb instanceof H264View) this.rfb.profile = p;
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
const tileByEl = new WeakMap<Element, Tile>();
const io = new IntersectionObserver((entries) => {
  for (const e of entries) {
    const t = tileByEl.get(e.target);
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
// The big screen can fill the whole window ("toàn cửa sổ"): grid and panel
// hide, the stage grows. fullTile is the staged phone while maximized (kept
// as a variable because quality / pause / key-target logic reads it).
let maxBig = false;
let fullTile: Tile | null = null;

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
  // a light "ghost" shrinks back into the tile while the real screen fades;
  // the stream goes back to the grid when the motion is done
  const t = masterTile();
  const done = stageMotion(false, t);
  if (done) done.then(layout); else layout();
}

// Open / close motion for the big screen. The real stage (video canvas,
// shadows, toolbar) is expensive to scale every frame, so a plain rounded
// rectangle — the "ghost" — flies between the tile and the stage position on
// the compositor, and the stage itself only fades in at the end (open) or out
// at the start (close). Input is not held back: the big screen takes touches
// from the first frame. Returns a promise that resolves when the motion ends,
// or null when there is nothing to animate (reduced motion, hidden tile).
let stageOpen = false;
const MOTION_MS = 240;
function stageMotion(open: boolean, t: Tile | null): Promise<void> | null {
  const st = $("#stage");
  if (!t || st.hidden || matchMedia("(prefers-reduced-motion: reduce)").matches) return null;
  const r0 = t.el.getBoundingClientRect(), r1 = $("#stageScreen").getBoundingClientRect();
  if (!r0.width || !r1.width) return null;
  document.querySelectorAll(".ghost").forEach((g) => g.remove());
  const g = document.createElement("div");
  g.className = "ghost";
  document.body.appendChild(g);
  const box = (r: DOMRect) => ({ transform: `translate(${r.left}px, ${r.top}px) scale(${r.width / 100}, ${r.height / 100})` });
  const a = box(open ? r0 : r1), b = box(open ? r1 : r0);
  const ease = "cubic-bezier(.22,.8,.2,1)";
  const fly = g.animate([{ ...a, opacity: open ? .9 : 1 }, { ...b, opacity: open ? 1 : .6 }], { duration: MOTION_MS, easing: ease, fill: "forwards" });
  const fade = st.animate(open ? [{ opacity: 0 }, { opacity: 0, offset: .6 }, { opacity: 1 }] : [{ opacity: 1 }, { opacity: 0, offset: .3 }, { opacity: 0 }],
    { duration: MOTION_MS, easing: "linear", fill: open ? "none" : "forwards" });
  return Promise.all([fly.finished, fade.finished]).then(() => { g.remove(); }, () => { g.remove(); });
}

// Put every screen where it belongs (slot / big screen / full view), size the
// big screen, and update the main-phone panel.
function layout() {
  const staged = stagedTile();
  if (!staged) maxBig = false;
  fullTile = maxBig ? staged : null;
  $(".app").classList.toggle("max", maxBig);
  for (const t of tiles.values()) {
    const where = staged === t ? $("#stageScreen") : t.slot;
    if (t.host.parentElement !== where) where.appendChild(t.host);
  }
  const st = $("#stage");
  st.hidden = !staged;
  if (staged) sizeStage(staged, false);
  const opening = !!staged && !stageOpen;
  if (opening) st.getAnimations().forEach((a) => a.cancel());
  const motion = opening ? stageMotion(true, staged) : null;
  stageOpen = !!staged;
  for (const t of tiles.values()) {
    t.paint(); t.sync();
    if (motion && t === staged) {   // Avoid a profile-change keyframe during the animation.
      void motion.then(() => { if (stagedTile() === t) { t.setProfile(desiredProfile(t)); t.setQuality(desiredQuality(t)); } });
    } else { t.setQuality(desiredQuality(t)); t.setProfile(desiredProfile(t)); }
    // Covered tiles pause rendering on the PC without retuning the phone.
    if (t.rfb instanceof H264View) t.rfb.paused = isBackgrounded(t);
  }
  updateOcclusion();
  renderStageCap();
  renderTags();
}

function sizeStage(t: Tile, checkOcc = true) {
  const app = $(".app");
  const avail = app.clientHeight - 24 - 6 - 30 - 24 - 34;  // padding, top border, nav bar, caption, toolbar
  const w0 = t.dev.width || 750, h0 = t.dev.height || 1334;
  let h = Math.max(200, Math.round(avail * (maxBig ? 100 : prefs.stage) / 100));
  let w = Math.round(h * w0 / h0);
  const maxW = app.clientWidth - 24 - (maxBig ? 0 : $("#ctrl").offsetWidth + 24);
  if (w > maxW) { w = Math.max(120, maxW); h = Math.round(w * h0 / w0); }   // landscape / narrow window
  const sc = $("#stageScreen");
  sc.style.width = w + "px"; sc.style.height = h + "px";
  const st = $("#stage");
  st.style.width = (w + 12) + "px";
  st.style.right = maxBig ? "" : ($("#ctrl").offsetWidth + 24) + "px";   // floats just left of the control panel
  st.style.left = maxBig ? Math.max(0, Math.round((app.clientWidth - (w + 12)) / 2)) + "px" : "";   // centred when maximized
  if (checkOcc) updateOcclusion();
  renderStageCap();
}
{ const ro = new ResizeObserver(() => { const s = stagedTile(); if (s) sizeStage(s); }); ro.observe($(".app")); ro.observe($("#ctrl")); }

function isBackgrounded(t: Tile) {
  return isSmall(t) && (!!fullTile || t.occluded);
}

// Only pause tiles completely covered by the floating stage. Keep their phone
// encoders at the same profile so revealing them never rebuilds the encoder.
let occRaf = 0;
function updateOcclusion() {
  const floating = prefs.big && !maxBig && !$("#stage").hidden;
  const sr = floating ? $("#stageScreen").getBoundingClientRect() : null;
  for (const t of tiles.values()) {
    let occ = false;
    if (sr && sr.width > 0 && t.visible && !t.el.hidden && isSmall(t)) {
      const r = t.el.getBoundingClientRect();
      occ = r.width > 0 && r.left >= sr.left && r.right <= sr.right && r.top >= sr.top && r.bottom <= sr.bottom;
    }
    if (occ === t.occluded) continue;
    t.occluded = occ;
    if (t.rfb instanceof H264View) t.rfb.paused = isBackgrounded(t);
  }
}
grid.addEventListener("scroll", () => {
  if (!prefs.big || maxBig || occRaf) return;
  occRaf = requestAnimationFrame(() => { occRaf = 0; updateOcclusion(); });
}, { passive: true });

function desiredQuality(t: Tile) {
  return isBig(t) ? state.focusQuality : state.quality;
}

// ---------------------------------------------------------------- H.264 frame rate & size
// Small screens (grid) and the big / full-window screen each have their own
// fps and size (% of the phone screen); a phone can override both. Lower =
// cooler phone. Needs TrollVNC 3.3-1004; older builds ignore it.
type Video = { gf?: number; gs?: number; bf?: number; bs?: number; dev?: Record<string, { f: number; s: number }> };
const VIDEO_DEF = { gf: 30, gs: 50, bf: 60, bs: 100 };
const clampFps = (v: unknown) => Math.max(5, Math.min(60, Math.round(+(v as number) || 30)));
const clampPct = (v: unknown) => Math.max(25, Math.min(100, Math.round(+(v as number) || 100)));
function video() { return { ...VIDEO_DEF, ...(ui.video || {}) }; }
function desiredProfile(t: Tile): Profile {
  const v = video();
  const o = v.dev?.[t.dev.id];
  if (o) return { fps: o.f, pct: o.s };
  return isBig(t) ? { fps: v.bf, pct: v.bs } : { fps: v.gf, pct: v.gs };
}
function retuneAll() { for (const t of tiles.values()) t.setProfile(desiredProfile(t)); }
const vIn = { gf: $<HTMLInputElement>("#vGF"), gs: $<HTMLInputElement>("#vGS"), bf: $<HTMLInputElement>("#vBF"), bs: $<HTMLInputElement>("#vBS") };
function fillVideo() { const v = video(); for (const k of ["gf", "gs", "bf", "bs"] as const) vIn[k].value = String(v[k]); }
for (const k of ["gf", "gs", "bf", "bs"] as const) {
  vIn[k].onchange = () => {
    const n = k.endsWith("f") ? clampFps(vIn[k].value) : clampPct(vIn[k].value);
    vIn[k].value = String(n);
    ui.video = { ...(ui.video || {}), [k]: n };
    saveUI(); retuneAll();
  };
}
function videoOverride(id: string) {
  const t = tiles.get(id);
  if (!t) return;
  const cur = ui.video?.dev?.[id];
  const s = window.prompt(`Hình riêng cho máy ${pad(t.dev.no)} · ${t.label()}\nNhập fps và % độ phân giải, ví dụ: 30 50\nĐể trống = theo cài đặt chung`, cur ? `${cur.f} ${cur.s}` : "");
  if (s === null) return;
  const m = s.trim().match(/^(\d+)\D+(\d+)$/);
  if (s.trim() && !m) { toast("Nhập fps rồi %, ví dụ: 30 50"); return; }
  const dev = { ...(ui.video?.dev || {}) };
  if (m) dev[id] = { f: clampFps(m[1]), s: clampPct(m[2]) }; else delete dev[id];
  ui.video = { ...(ui.video || {}), dev };
  saveUI(); t.setProfile(desiredProfile(t));
  toast(m ? `Máy ${pad(t.dev.no)}: ${dev[id].f} fps · ${dev[id].s}%` : `Máy ${pad(t.dev.no)}: theo cài đặt chung`);
}

// Big screen at window size (toolbar ⛶ or context menu); again = normal size.
function toggleMax(t?: Tile) {
  if (t && stagedTile() !== t) { maxBig = true; openBig(t.dev.id); return; }
  maxBig = !maxBig;
  if (maxBig && !prefs.big) { openBig(t?.dev.id || masterId() || activeId); return; }
  layout();
}
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
  renderTags(); renderFooter(); renderButtons();
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
let receivedState = false;
function applyState(s: State) {
  const first = !receivedState;
  receivedState = true;
  const previous = state;
  const pwChanged = s.password !== state.password || s.h264 !== state.h264;
  s.tags = s.tags || [];
  s.devices = s.devices || [];   // no phone yet: the agent sends null
  state = s;
  const seen = new Set<string>();
  let order = 0;
  let stageChanged = false;
  let countsChanged = first || s.devices.length !== previous.devices.length;
  let tagCellsChanged = countsChanged;
  let needsLayout = s.devices.length !== previous.devices.length ||
    JSON.stringify(s.tags) !== JSON.stringify(previous.tags);
  for (const d of s.devices) {
    seen.add(d.id);
    let t = tiles.get(d.id);
    if (!t) {
      countsChanged = tagCellsChanged = true;
      t = new Tile(d);
      tiles.set(d.id, t);
      io.observe(t.el);
    } else if (Object.keys(d).some((key) => d[key as keyof Dev] !== t?.dev[key as keyof Dev]) ||
               Object.keys(t.dev).length !== Object.keys(d).length) {
      const old = t.dev;
      if (old.inSync !== d.inSync || (old.status === "online") !== (d.status === "online")) countsChanged = true;
      if (old.name !== d.name || old.vncName !== d.vncName || old.no !== d.no) tagCellsChanged = true;
      if (isBig(t)) {
        stageChanged = true;
        if (d.width !== t.dev.width || d.height !== t.dev.height) needsLayout = true;
      }
      if ((old.name !== d.name || old.vncName !== d.vncName) &&
          Object.keys({ ...old, ...d }).every(k => k === "name" || k === "vncName" || old[k as keyof Dev] === d[k as keyof Dev])) {
        t.dev = d;
        if (t.nameEl.isConnected) t.nameEl.textContent = t.label();
      } else t.update(d);
    }
    const index = String(order++);
    if (t.el.style.order !== index) { t.el.style.order = index; needsLayout = true; }
    if (t.el.parentElement !== grid) grid.appendChild(t.el);
    if (pwChanged) { t.disconnect(); t.sync(); }
  }
  for (const [id, t] of tiles) {
    if (seen.has(id)) continue;
    needsLayout = true;
    io.unobserve(t.el);
    t.destroy(); tiles.delete(id);
  }
  // first run: the first phone becomes the main phone
  if (!masterId() && s.devices.length) { prefs.master = s.devices[0].id; lsSet("iosview.master", prefs.master); }
  if (prefs.tag && !s.tags.some((x) => x.name === prefs.tag)) { prefs.tag = ""; needsLayout = true; }

  if (first || previous.devices.length !== s.devices.length) $("#empty").hidden = s.devices.length > 0;
  if (first || previous.usbError !== s.usbError) {
    const warn = $("#usbWarn");
    warn.hidden = !s.usbError;
    warn.textContent = s.usbError ? "USB: " + s.usbError : "";
    const connTab = $('[data-tab="conn"]');
    connTab.classList.toggle("alert", !!s.usbError);
    connTab.title = s.usbError ? "Có lỗi kết nối USB — xem chi tiết" : "";
  }
  if (countsChanged || s.syncOn !== previous.syncOn) renderButtons();
  if (first || s.quality !== previous.quality || s.focusQuality !== previous.focusQuality) syncSliders();
  if (first || s.vncPort !== previous.vncPort || JSON.stringify(s.wifiDevices) !== JSON.stringify(previous.wifiDevices)) fillWifi();
  if (needsLayout || pwChanged) layout();
  else {
    if (s.syncOn !== previous.syncOn) for (const t of tiles.values()) t.paint();
    if (s.quality !== previous.quality || s.focusQuality !== previous.focusQuality)
      for (const t of tiles.values()) t.setQuality(desiredQuality(t));
    if (stageChanged) renderStageCap();
    if (tagCellsChanged || countsChanged) renderTags();
  }
  if (countsChanged) renderFooter();
}

function renderFooter() {
  const online = state.devices.filter((d) => d.status === "online").length;
  const sel = state.devices.filter((d) => d.inSync).length;
  const info = document.getElementById("selinfo");
  if (info) info.textContent = `${sel}/${state.devices.length} chọn · ${online} online`;
  $("#ftInfo").textContent = `${state.devices.length} máy · ${online} online`;
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
  else { const v = window.prompt(`Số máy mới cho ${t.label()} (1–999)`, String(t.dev.no)); if (v && +v) send({ t: "number", d: id, no: +v }); }
}

function renameDevice(id: string) {
  const t = tiles.get(id);
  if (!t) return;
  if (t.nameEl.isConnected && !t.el.hidden && getComputedStyle(t.nameEl).display !== "none") t.rename();
  else { const v = window.prompt(`Tên mới cho máy ${pad(t.dev.no)}`, t.label()); if (v !== null) send({ t: "rename", d: id, name: v.trim() }); }
}

// ---------------------------------------------------------------- big-screen toolbar
// The main phone's own actions sit right under its big screen (where the
// mouse already is); everything else is in the context menu.
type Fn = { k: string; label: string; icon: string; kbd?: string };
const STAGE_FNS: Fn[] = [
  { k: "#rename", label: "Đổi tên", icon: "rename", kbd: "F2" },
  { k: "#paste", label: "Dán từ PC", icon: "paste", kbd: "Ctrl+V" },
  { k: "selectall", label: "Chọn hết chữ", icon: "selectall" },
  { k: "clear", label: "Xoá ô nhập", icon: "clear", kbd: "Ctrl+⌫" },
  { k: "cc", label: "Cài đặt nhanh", icon: "cc" },
  { k: "mute", label: "Tắt / bật tiếng", icon: "mute" },
  { k: "#shot", label: "Chụp màn hình", icon: "shot" },
  { k: "#full", label: "Toàn cửa sổ (bấm lại để về cỡ thường)", icon: "full" },
  { k: "#close", label: "Đóng màn lớn", icon: "x" },
];
$("#stageTools").innerHTML = STAGE_FNS.map((f) =>
  `<button class="ib tb" data-fn="${f.k}" title="${f.label}${f.kbd ? ` (${f.kbd})` : ""}" aria-label="${f.label}">${ico(f.icon)}</button>`).join("");
onPress($("#stageTools"), (e) => {
  const b = (e.target as HTMLElement).closest<HTMLElement>("[data-fn]");
  const t = masterTile();
  if (!b || !t) return;
  setActive(t);
  const k = b.dataset.fn!;
  if (k === "#rename") renameDevice(t.dev.id);
  else if (k === "#paste") pasteFromPC(t.dev.id, { one: e.altKey });
  else if (k === "#shot") shoot([t]);
  else if (k === "#full") toggleMax(t);
  else if (k === "#close") closeBig();
  else {
    send({ t: "btn", d: t.dev.id, b: k });
    if (k === "selectall") toast("Đang chọn hết văn bản trên máy…");
    if (k === "clear") toast("Đang xoá nội dung ô nhập…");
  }
});

// Caption under the big screen; the details (link, size, iOS…) are its tooltip.
function renderStageCap() {
  const t = stagedTile();
  if (!t) return;
  const d = t.dev;
  const via = d.transport === "usb" ? "USB" + (d.wifi ? ` · Wi‑Fi ${d.wifi} dự phòng` : "") : `Wi‑Fi ${d.addr}`;
  const lines = [
    statusText(d), via,
    d.width ? `${d.width}×${d.height}` : "",
    d.vncName && d.vncName !== d.name ? d.vncName : "",
    d.udid ? `UDID ${d.udid.slice(0, 8)}…${d.udid.slice(-6)}` : "",
    d.ios ? `iOS ${d.ios}` + (d.kind === "cd" ? " · CoreDevice (không jailbreak)" : "") : "",
    t.rfb ? (t.rfb instanceof H264View ? (d.kind === "cd" ? "Hình: HEVC" : "Hình: H.264") : "Hình: JPEG") : "",
  ].filter(Boolean);
  const cap = $("#stageCap");
  cap.textContent = `${pad(d.no)} · ${t.label()}` + (state.syncOn && d.inSync ? ` · đồng bộ ${syncCount()} máy` : " · riêng máy này");
  cap.title = lines.join("\n");
  $(".cap-dot").dataset.s = d.status;
}
// Cable-connected phones that have no Wi-Fi address attached yet.
function usbCandidates(): Dev[] {
  return state.devices.filter((d) => d.udid && !d.wifi);
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
  h += `<button data-m="full">${ico("full")}Toàn cửa sổ</button>`;
  h += `<button data-m="cc">${ico("cc")}Mở Cài đặt nhanh</button>`;
  h += `<button data-m="shot">${ico("shot")}Chụp màn hình</button>`;
  const vo = ui.video?.dev?.[id];
  h += `<button data-m="video">${ico("full")}Hình riêng cho máy này…${vo ? `<kbd>${vo.f}fps ${vo.s}%</kbd>` : ""}</button>`;
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
    case "full": toggleMax(t); break;
    case "cc": navPress(t, "cc"); break;
    case "shot": shoot([t]); break;
    case "video": videoOverride(id); break;
    case "remove": send({ t: "remove", d: id }); break;
  }
};
document.addEventListener("pointerdown", (e) => { if (!(e.target as HTMLElement).closest("#menu")) $("#menu").hidden = true; });
window.addEventListener("blur", () => { $("#menu").hidden = true; });

// ---------------------------------------------------------------- tags (number grid)
let tagsKey = "";
function renderTags() {
  const ids = state.devices.map((d) => d.id);
  const key = JSON.stringify([state.devices.map((d) => [d.id, d.no, d.name, d.vncName, d.inSync, d.status === "online"]), state.tags, masterId(), prefs.big, prefs.tag]);
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
// click acts at once; a double click (two toggles = unchanged) then opens /
// renames
$("#tags").addEventListener("click", (e) => {
  const el = e.target as HTMLElement;
  const cell = el.closest<HTMLElement>("[data-cell]");
  if (cell) { toggleSel([cell.dataset.cell!]); return; }
  const f = el.closest<HTMLElement>("[data-tagf]");
  if (f) {
    const name = f.dataset.tagf!;
    prefs.tag = prefs.tag === name ? "" : name;
    tagsKey = ""; layout();
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

function ripple(t: Tile, cr: { left: number; top: number; width: number; height: number }, x: number, y: number) {
  if (!cr.width || !t.host.isConnected) return;
  const hr = t.host.getBoundingClientRect();
  const dot = document.createElement("i");
  dot.className = "ripple";
  dot.style.left = `${cr.left - hr.left + x * cr.width}px`;
  dot.style.top = `${cr.top - hr.top + y * cr.height}px`;
  t.host.appendChild(dot);
  setTimeout(() => dot.remove(), 450);
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
// Cap pointermove fan-out at ~60Hz (iPhone display rate) even on 144/240Hz PC monitors.
const MOVE_INTERVAL_MS = 1000 / 60;

function bindInput(t: Tile) {
  const ov = t.overlay;
  let mask = 0;
  let epoch = 0;              // controlEpoch when the current gesture began
  let solo = false;           // gesture started with Alt: this phone only
  let lastWheel = 0;
  let dragRect: { left: number; top: number; width: number; height: number } | null = null;
  let moveClient: { clientX: number; clientY: number } | null = null;
  let lastMoveAt = 0;
  let nextMoveAt = 0;
  let lastSentX = 0.5;
  let lastSentY = 0.5;
  let edgeSteps = 0;
  let moveTimer = 0;
  let holdTimer = 0;          // right button press-and-hold → app switcher
  let holdOne = false;
  const id = () => t.dev.id;
  // Control link changed since this gesture began: drop it. Returns true
  // when there is no live gesture.
  const stale = () => {
    if (mask && epoch !== controlEpoch) { mask = 0; moveClient = null; dragRect = null; edgeSteps = 0; }
    return !mask;
  };
  const normCached = (ev: { clientX: number; clientY: number }) => {
    if (dragRect && dragRect.width && dragRect.height) {
      return { x: (ev.clientX - dragRect.left) / dragRect.width, y: (ev.clientY - dragRect.top) / dragRect.height };
    }
    return norm(t, ev);
  };
  const sendPtr = (x: number, y: number) => {
    const cx = +Math.min(1, Math.max(0, x)).toFixed(4);
    const cy = +Math.min(1, Math.max(0, y)).toFixed(4);
    lastSentX = cx;
    lastSentY = cy;
    send({ t: "p", d: id(), x: cx, y: cy, m: mask, one: solo || undefined });
  };
  const flushMove = () => {
    moveTimer = 0;
    if (!moveClient || stale()) { moveClient = null; return; }
    const now = performance.now();
    const minInterval = edgeSteps > 0 ? 8 : MOVE_INTERVAL_MS;
    if (now < nextMoveAt) {
      moveTimer = window.setTimeout(flushMove, Math.ceil(nextMoveAt - now));
      return;
    }
    const p = normCached(moveClient);
    moveClient = null;
    if (p) {
      if (edgeSteps > 0) edgeSteps--;
      lastMoveAt = now;
      nextMoveAt = Math.max(nextMoveAt + minInterval, now + minInterval / 2);
      sendPtr(p.x, p.y);
    }
  };
  const cancelHold = () => { clearTimeout(holdTimer); holdTimer = 0; };

  ov.addEventListener("contextmenu", (e) => e.preventDefault());
  ov.addEventListener("pointerenter", () => { hoverId = id(); });
  ov.addEventListener("pointerdown", (e) => {
    // Zoom mode (Ctrl+S): small screens are view-only — a left click anywhere
    // on one (picture or not) shows the phone big at once, a right click
    // closes the big screen. Otherwise every click is a tap, sent at once
    // (never held back to detect a double click).
    if (prefs.zoomTap && isSmall(t) && !e.ctrlKey && !e.shiftKey && !e.metaKey) {
      e.preventDefault();
      if (e.button === 0) openBig(id());
      else if (e.button === 2) closeBig();
      return;
    }
    const c = t.canvas();
    if (!c || (t.hasPicture && !(t.rfb instanceof H264View ? t.rfb.gotFrame : t.rfb))) return;
    const r = screenRect(c);
    if (!r.width || !r.height) return;
    if (e.clientX < r.left - 2 || e.clientX > r.right + 2 || e.clientY < r.top - 2 || e.clientY > r.bottom + 3) return;
    let px = Math.min(1, Math.max(0, (e.clientX - r.left) / r.width));
    let py = Math.min(1, Math.max(0, (e.clientY - r.top) / r.height));
    // Snap presses in the bottom/top 2.8% rim right to the physical edge so
    // swiping up opens Control Center (while a plain tap still hits UITabBar).
    if (py >= 0.972) py = 1;
    else if (py <= 0.018) py = 0;
    if (px <= 0.018) px = 0;
    else if (px >= 0.982) px = 1;
    e.preventDefault();
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
    clearTimeout(moveTimer);
    moveTimer = 0;
    if (!mask) { epoch = controlEpoch; solo = e.altKey; }
    dragRect = r;
    moveClient = null;
    edgeSteps = (px === 0 || px === 1 || py === 0 || py === 1) ? 3 : 0;
    lastMoveAt = performance.now();
    nextMoveAt = lastMoveAt;
    mask |= 1;
    sendPtr(px, py);
    // Ripple only on the touched tile so follower canvases keep their direct GPU overlay path.
    ripple(t, r, px, py);
  });
  ov.addEventListener("pointermove", (e) => {
    if (stale()) return;
    moveClient = { clientX: e.clientX, clientY: e.clientY };
    // Send as soon as the input interval allows, independent of canvas rendering.
    if (performance.now() >= nextMoveAt) {
      clearTimeout(moveTimer);
      flushMove();
    } else if (!moveTimer) {
      flushMove();
    }
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
    clearTimeout(moveTimer);
    moveTimer = 0;
    const now = performance.now();
    const raw = normCached(e) || { x: lastSentX, y: lastSentY };
    const cx = Math.min(1, Math.max(0, raw.x));
    const cy = Math.min(1, Math.max(0, raw.y));
    const rectW = dragRect?.width || 1;
    const rectH = dragRect?.height || 1;
    const twitchPx = Math.hypot((cx - lastSentX) * rectW, (cy - lastSentY) * rectH);
    // If the user paused (>=45ms) before releasing — e.g. resting on a birthday
    // picker wheel row — ignore <=3px button-release micro-twitch so UIScrollView
    // does not launch into an unintended fling.
    if (now - lastMoveAt >= 45 && twitchPx <= 3) {
      moveClient = null;
      dragRect = null;
      edgeSteps = 0;
      mask = 0;
      sendPtr(lastSentX, lastSentY);
      return;
    }
    if (moveClient) {
      const pt = normCached(moveClient);
      moveClient = null;
      if (pt) {
        const movePx = Math.hypot((pt.x - lastSentX) * rectW, (pt.y - lastSentY) * rectH);
        if (movePx > 1.5) sendPtr(pt.x, pt.y);
      }
    }
    dragRect = null;
    edgeSteps = 0;
    mask = 0;
    sendPtr(cx, cy);
  };
  ov.addEventListener("pointerup", up);
  ov.addEventListener("pointercancel", (e) => { cancelHold(); up(e); });
  ov.addEventListener("lostpointercapture", (e) => { if (holdTimer > 0) cancelHold(); if (mask) up(e as PointerEvent); });
  ov.addEventListener("wheel", (e) => {
    if (prefs.zoomTap && isSmall(t)) return;   // zoom mode: the wheel scrolls the grid
    e.preventDefault();
    const now = performance.now();
    if (now - lastWheel < 35) return;
    lastWheel = now;
    const p = norm(t, e);
    if (!p) return;
    setActive(t);
    stale();
    const one = e.altKey || undefined;
    send({ t: "w", d: id(), x: +p.x.toFixed(4), y: +p.y.toFixed(4), m: mask, down: e.deltaY < 0, one });
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
let copyingPhones = false;

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
  if (combo === "Ctrl+C" && document.hasFocus()) {
    e.preventDefault();
    if (!e.repeat) void copyFromPhones();
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
  if (e.key === "F5") { if (WAILS) { e.preventDefault(); location.reload(); } return; }   // Wails: browser keys are off
  if (e.key === "F11" || e.key === "F12") return;   // leave window keys alone
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
    if (k === "s") { toggleZoomTap(); return; }
    if (k === "arrowleft" || k === "arrowup") { switchBig(-1); return; }
    if (k === "arrowright" || k === "arrowdown") { switchBig(1); return; }
    const d = keyTarget();
    if (!d) return;
    const kt = tiles.get(d);
    if (kt) setActive(kt);
    if (k === "backspace" || k === "delete") { send({ t: "btn", d, b: "clear" }); toast("Đang xoá nội dung ô nhập…"); return; }
    if (["c", "x", "z", "y"].includes(k)) {
      sendCommand(d, k);
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
  const one = document.body.classList.contains("solo");   // Alt held: this phone only
  if (!one && splitPaste(text)) return;
  pasteText(d, text, { one });
});

type ClipboardCopy = { seq: number; text: string; parts: { id: string; text: string; read: boolean }[] };
let lastGroupCopy: ClipboardCopy | null = null;
let lastCopySeq = 0;
function rememberCopy(copy: ClipboardCopy) {
  if (copy.seq <= lastCopySeq) return;
  lastCopySeq = copy.seq;
  lastGroupCopy = copy.parts?.length > 1 ? copy : null;
}
let textSeq = 0;
const textAcks = new Map<number, (count: number | null) => void>();

function queueText(d: string, text: string, opt: { force?: boolean; one?: boolean }): Promise<number | null> {
  return new Promise((resolve) => {
    const seq = ++textSeq;
    const timer = window.setTimeout(() => textAcks.get(seq)?.(null), 3000);
    const done = (count: number | null) => {
      clearTimeout(timer);
      textAcks.delete(seq);
      resolve(count);
    };
    textAcks.set(seq, done);
    if (!send({ t: "text", d, s: text, f: opt.force, one: opt.one, seq })) done(0);
  });
}

// Preserve row positions, including blank rows and offline phones. A copy
// made by iOSView also retains the exact device IDs and multiline values.
function splitPaste(text: string, force = false): boolean {
  const normalized = text.replace(/\r\n/g, "\n");
  const group = state.devices.filter((d) => d.inSync).sort((a, b) => a.no - b.no || a.id.localeCompare(b.id));
  const copy = lastGroupCopy;
  if (copy && normalized === copy.text.replace(/\r\n/g, "\n")) {
    const devices = new Map(state.devices.map((d) => [d.id, d]));
    const entries = copy.parts.filter((part) => part.read && part.text !== "");
    void sendPasteRows(entries.map((part) => ({
      id: part.id, text: part.text, device: devices.get(part.id),
    })), force, 0);
    return true;
  }
  if (copy) lastGroupCopy = null;
  const lines = normalized.split("\n");
  if (lines.length < 2 || group.length < 2) return false;
  const rows = group.slice(0, lines.length).flatMap((device, i) => lines[i] === "" ? [] : [
    { id: device.id, text: lines[i], device },
  ]);
  const extra = lines.slice(group.length).filter((line) => line !== "").length;
  void sendPasteRows(rows, force, extra);
  return true;
}

async function sendPasteRows(rows: { id: string; text: string; device?: Dev }[], force: boolean, extra: number) {
  const results = await Promise.all(rows.map(async ({ id, text, device }) => ({
    device, count: device?.status === "online" ? await queueText(id, text, { force, one: true }) : 0,
  })));
  const sent = results.filter(({ count }) => count === 1).length;
  const failed = results.filter(({ count }) => count !== 1);
  const notes = failed.map(({ device, count }) => {
    const name = device ? `Máy ${pad(device.no)}` : "Máy đã bị xoá";
    return `${name} ${count === null ? "chưa xác nhận" : "chưa nhận lệnh"}`;
  });
  if (extra) notes.push(`${extra} dòng không có máy tương ứng`);
  if (!sent && !notes.length) { toast("Không có dữ liệu để dán"); return; }
  toast([`Đã xếp lệnh dán cho ${sent} máy`, ...notes].join(" · "));
}

function pasteText(d: string, text: string, opt: { force?: boolean; one?: boolean } = {}) {
  if (!text) { toast("Clipboard của PC đang trống (chỉ hỗ trợ văn bản)"); return; }
  send({ t: "text", d, s: text, f: opt.force, one: opt.one });
}
async function pasteFromPC(d: string, opt: { force?: boolean; one?: boolean } = {}) {
  try {
    const text = await navigator.clipboard.readText();
    if (!opt.one && splitPaste(text, opt.force)) return;
    pasteText(d, text, opt);
  }
  catch { toast("Không đọc được clipboard — trỏ chuột vào iPhone rồi bấm Ctrl+V"); }
}

// Ctrl+C reads phones on the Go backend and writes directly to the OS clipboard
// (Win32 API) so copies never miss when switching windows.
async function copyFromPhones() {
  if (copyingPhones) return;
  copyingPhones = true;
  try {
    const res = await fetch("/api/copy?blank=" + (prefs.copyBlank ? "1" : "0"), { method: "POST" });
    if (!res.ok) {
      toast((await res.json()).error || "Không copy được từ iPhone");
      return;
    }
    const result = await res.json() as ClipboardCopy;
    rememberCopy(result);
    const missed = result.parts?.filter((part) => !part.read).length ?? 0;
    if (missed) toast(`${missed} máy chưa trả clipboard. Đã giữ vị trí các máy này.`);
  } catch { toast("Không copy được từ iPhone"); }
  finally { copyingPhones = false; }
}

// ---------------------------------------------------------------- screenshots
async function shoot(list: Tile[]) {
  const live = list.filter((t) => t.canvas() && t.host.classList.contains("live") && (t.rfb instanceof H264View ? t.rfb.gotFrame : t.rfb));
  if (!live.length) { toast("Máy chưa có hình để chụp"); return; }
  let saved = 0, last = "";
  await Promise.all(live.map(async (t) => {
    const c = t.canvas();
    if (!c) return;
    const blob = await new Promise<Blob | null>((r) => c.toBlob(r, "image/png"));
    if (!blob) return;
    const ac = new AbortController();
    const timer = window.setTimeout(() => ac.abort(), 5000);
    try {
      const res = await fetch(`/api/shot?d=${encodeURIComponent(t.dev.id)}`, { method: "POST", body: blob, signal: ac.signal });
      if (res.ok) { saved++; last = (await res.json()).path || ""; }
    } catch { /* ignore */ }
    finally { clearTimeout(timer); }
  }));
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
function toggleZoomTap() {
  prefs.zoomTap = !prefs.zoomTap; lsSet("iosview.zoomtap", prefs.zoomTap ? "1" : "0");
  $(".app").classList.toggle("zoom-tap", prefs.zoomTap);
  renderButtons();
  toast(prefs.zoomTap ? "Nhấp màn nhỏ: phóng to (Ctrl+S)" : "Nhấp màn nhỏ: điều khiển (Ctrl+S)");
}
$(".app").classList.toggle("zoom-tap", prefs.zoomTap);
function toggleSync() { state.syncOn = !state.syncOn; send({ t: "sync", on: state.syncOn }); renderButtons(); for (const t of tiles.values()) t.paint(); toast(state.syncOn ? "Đồng bộ: BẬT (F4)" : "Đồng bộ: TẮT (F4)"); }

// Control panel buttons, in blocks: group (who gets the input) · controls
// (what to do) · view (how it is shown, rendered next to the sliders).
type Btn = { k: string; g: "group" | "ctl" | "view"; label: string; icon?: string; sw?: () => boolean; cls?: string; title: string };
const BTNS: Btn[] = [
  { k: "sync", g: "group", label: "Đồng bộ thao tác", sw: () => state.syncOn, cls: "sync", title: "Bật/tắt đồng bộ thao tác cho các máy đang chọn (F4)" },
  { k: "all", g: "group", label: "Chọn hết", icon: "all", title: "Đưa tất cả máy vào nhóm đồng bộ (Ctrl+A)" },
  { k: "none", g: "group", label: "Bỏ chọn", icon: "none", title: "Bỏ tất cả khỏi nhóm" },
  { k: "power", g: "ctl", label: "Khoá", icon: "power", title: "Khoá / mở màn hình" },
  { k: "voldown", g: "ctl", label: "Vol −", icon: "voldown", title: "Giảm âm lượng" },
  { k: "volup", g: "ctl", label: "Vol +", icon: "volup", title: "Tăng âm lượng" },
  { k: "paste", g: "ctl", label: "Dán", icon: "paste", title: "Gõ nội dung clipboard của PC vào máy (hoặc trỏ chuột vào iPhone rồi Ctrl+V)" },
  { k: "clear", g: "ctl", label: "Xoá ô", icon: "clear", title: "Xoá sạch ô nhập đang có con trỏ (Ctrl+Backspace)" },
  { k: "shot", g: "ctl", label: "Chụp", icon: "shot", title: "Chụp màn hình các máy đang chọn" },
  { k: "zoom", g: "view", label: "Nhấp màn nhỏ = phóng to", sw: () => prefs.zoomTap, title: "Bật: nhấp màn nhỏ mở máy đó ở màn lớn, chuột phải đóng, lăn chuột cuộn lưới. Tắt: nhấp / vuốt điều khiển máy (Ctrl+S)" },
  { k: "stats", g: "view", label: "FPS", sw: () => prefs.stats, title: "Hiện fps / băng thông / độ trễ trên từng máy" },
];
const btnHTML = (b: Btn) => `<button class="btn ${b.cls || ""}" data-b="${b.k}" title="${esc(b.title)}">${b.sw ? `<span class="sw"></span>` : ico(b.icon!)}<span>${b.label}</span></button>`;
function renderButtons() {
  const el = $("#btns");
  if (!el.children.length) {
    const grp = (g: Btn["g"]) => BTNS.filter((b) => b.g === g).map(btnHTML).join("");
    el.innerHTML = `<div class="blk"><div class="blk-h">Nhóm máy <span id="selinfo" class="blk-n"></span></div>` +
      `<div class="btns one">${btnHTML(BTNS[0])}</div><div class="btns two">${BTNS.filter((b) => b.g === "group").slice(1).map(btnHTML).join("")}</div></div>` +
      `<div class="blk"><div class="blk-h">Điều khiển <span id="ctlScope" class="blk-n"></span></div><div class="btns">${grp("ctl")}</div></div>`;
    $("#viewBtns").innerHTML = `<div class="rows">${grp("view")}</div>`;
  }
  for (const b of BTNS) if (b.sw) document.querySelector(`[data-b="${b.k}"]`)!.setAttribute("aria-pressed", String(b.sw()));
  $("#ctlScope").textContent = state.syncOn ? `· ${syncCount()} máy` : "· máy đang dùng";
}
function onBtn(e: MouseEvent) {
  const b = (e.target as HTMLElement).closest<HTMLElement>("[data-b]");
  if (!b) return;
  const k = b.dataset.b!;
  switch (k) {
    case "sync": toggleSync(); return;
    case "all": selectAllPhones(); return;
    case "none": toggleSel(state.devices.map((d) => d.id), false); return;
    case "zoom": toggleZoomTap(); return;
    case "stats":
      prefs.stats = !prefs.stats; lsSet("iosview.stats", prefs.stats ? "1" : "0");
      renderButtons(); $("#netTotal").hidden = !prefs.stats; return;
    case "shot": {
      const sel = [...tiles.values()].filter((t) => t.dev.inSync && t.dev.status === "online");
      shoot(sel.length ? sel : [masterTile()!].filter(Boolean));
      return;
    }
  }
  const d = k === "paste" && e.altKey ? keyTarget() || groupTarget() : groupTarget();
  if (d === null || (k === "paste" && e.altKey && !d)) return;
  if (k === "paste") pasteFromPC(d, { one: e.altKey });
  else send({ t: "btn", d, b: k });
}
onPress($("#btns"), onBtn);
onPress($("#viewBtns"), onBtn);
$("#textForm").onsubmit = async (e) => {
  e.preventDefault();
  const inp = $<HTMLInputElement>("#textInput");
  if (!inp.value) return;
  const d = groupTarget();
  if (d === null) return;
  const text = inp.value;
  const count = await queueText(d, text, {});
  if (count) {
    if (inp.value === text) inp.value = "";
    toast(`Đã xếp lệnh gửi văn bản cho ${count} máy`);
  } else toast(count === null ? "Chưa nhận được xác nhận gửi" : "Chưa gửi được văn bản");
};

// sliders
// Filled part of a range slider (CSS --p). Dragging fires "input"; values set
// from code call paintRanges().
function paintRange(el: HTMLInputElement) {
  const min = +el.min || 0, max = +el.max || 100;
  el.style.setProperty("--p", `${((+el.value - min) / (max - min || 1)) * 100}%`);
}
function paintRanges() { document.querySelectorAll<HTMLInputElement>("input[type=range]").forEach(paintRange); }
document.addEventListener("input", (e) => { const t = e.target; if (t instanceof HTMLInputElement && t.type === "range") paintRange(t); }, true);
const sStage = $<HTMLInputElement>("#sStage"), sTile = $<HTMLInputElement>("#sTile");
const sQ = $<HTMLInputElement>("#sQ"), sFQ = $<HTMLInputElement>("#sFQ");
sStage.value = String(prefs.stage); sTile.value = String(prefs.tileW);
function applyTile() {
  document.documentElement.style.setProperty("--tile-w", sTile.value + "px");
  $("#oTile").textContent = sTile.value;
  $("#oStage").textContent = sStage.value + "%";
  paintRanges();
}
sTile.oninput = () => { prefs.tileW = +sTile.value; lsSet("iosview.tile", sTile.value); applyTile(); };
sStage.oninput = () => { prefs.stage = +sStage.value; lsSet("iosview.stage", sStage.value); applyTile(); const s = stagedTile(); if (s) sizeStage(s); };
applyTile();
let qDragging = false;
function syncSliders() {
  if (qDragging) return;
  sQ.value = String(state.quality); sFQ.value = String(state.focusQuality);
  $("#oQ").textContent = sQ.value; $("#oFQ").textContent = sFQ.value;
  paintRanges();
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
    if (b.dataset.tab === "conn") fillWifi(true);
  };
});

// connection tab: ordered Wi-Fi IP list (one IP per line, auto-applied & saved)
function fmtWifiAddr(addr: string) {
  const def = ":" + (state.vncPort || 5901);
  return /^\d+(?:\.\d+){3}:/.test(addr) && addr.endsWith(def) ? addr.slice(0, -def.length) : addr;
}
function parseWifiLines(raw: string): { list: string[]; invalid: number } {
  const list = new Set<string>();
  const lines = raw.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const value = lines[i].trim();
    if (!value) continue;
    const match = /^(?:(\d{1,3}(?:\.\d{1,3}){3})(?::(\d{1,5}))?|([a-zA-Z0-9.-]+):(\d{1,5})|\[([a-fA-F0-9:]+)\]:(\d{1,5}))$/.exec(value);
    const port = match?.[2] ?? match?.[4] ?? match?.[6];
    if (!match || match[1]?.split(".").some((octet) => +octet > 255 || String(+octet) !== octet) ||
        (port !== undefined && (+port < 1 || +port > 65535))) return { list: [], invalid: i + 1 };
    list.add(value);
  }
  return { list: [...list], invalid: 0 };
}
const wifiAddrEl = $<HTMLTextAreaElement>("#wifiAddr");
let wifiTimer = 0;
let wifiPending = 0;
let wifiSeq = 0;
let wifiDirty = false;
let wifiSubmitted = "";
let wifiAckTimer = 0;
function fillWifi(force = false) {
  if (wifiDirty || (!force && document.activeElement === wifiAddrEl)) return;
  const value = (state.wifiDevices || []).map(fmtWifiAddr).join("\n");
  if (wifiAddrEl.value !== value) wifiAddrEl.value = value;
}
function finishWifi(seq: number, error?: string) {
  if (seq !== wifiPending) return;
  wifiPending = 0;
  clearTimeout(wifiAckTimer);
  if (error) { toast(error); return; }
  if (wifiAddrEl.value === wifiSubmitted) wifiDirty = false;
  else syncWifiFromInput();
}
function syncWifiFromInput() {
  if (!wifiDirty || wifiPending) return;
  const { list, invalid } = parseWifiLines(wifiAddrEl.value);
  wifiAddrEl.setAttribute("aria-invalid", String(!!invalid));
  $("#wifiError").textContent = invalid ? `Kiểm tra địa chỉ IP hoặc cổng ở dòng ${invalid}.` : "";
  if (invalid) return;
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    toast("Mất kết nối tới iOSView — danh sách Wi‑Fi chưa lưu");
    return;
  }
  wifiPending = ++wifiSeq;
  wifiSubmitted = wifiAddrEl.value;
  const seq = wifiPending;
  send({ t: "setwifi", ids: list, seq });
  clearTimeout(wifiAckTimer);
  wifiAckTimer = window.setTimeout(() => finishWifi(seq, "Chưa nhận được xác nhận lưu danh sách Wi-Fi"), 8000);
}
wifiAddrEl.addEventListener("input", () => {
  wifiDirty = true;
  clearTimeout(wifiTimer);
  wifiTimer = window.setTimeout(syncWifiFromInput, 350);
});
wifiAddrEl.addEventListener("blur", () => {
  clearTimeout(wifiTimer);
  syncWifiFromInput();
});
$("#clearWifiBtn").onclick = () => {
  const count = (state.wifiDevices || []).length;
  if (!count && !wifiAddrEl.value.trim()) { toast("Chưa có máy Wi‑Fi nào"); return; }
  if (!window.confirm("Ngắt kết nối và xoá toàn bộ địa chỉ Wi‑Fi đã nhập?")) return;
  clearTimeout(wifiTimer);
  wifiAddrEl.value = "";
  wifiDirty = true;
  syncWifiFromInput();
};

// settings tab
const copyBlank = $<HTMLInputElement>("#setCopyBlank");
copyBlank.checked = prefs.copyBlank;
copyBlank.onchange = () => {
 prefs.copyBlank = copyBlank.checked;
 lsSet("iosview.copyblank", prefs.copyBlank ? "1" : "0");
 send({ t: "copyblank", on: prefs.copyBlank });
};
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
// Light theme only (dark dropped in 0.11.0).

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
type Act = "force" | "paste" | "phrases" | "settings" | "notes" | "notesClose";
const ACTS: { a: Act; label: string; def: string }[] = [
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
  "Ctrl+S", "F2", "F4", "F5", "F11", "F12", "Escape", "Alt+F4"]);

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
  if (!prefs.big) { openBig(hoverId || activeId || masterId() || list[0].dev.id); return; }
  const cur = masterTile();
  openBig(step(cur && list.includes(cur) ? cur : null).dev.id);
}

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
  return te ? tileByEl.get(te) : undefined;
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
grid.addEventListener("contextmenu", (e) => { if (e.target === grid) e.preventDefault(); });
// Right click outside the big screen (empty grid, window background, around
// the big screen) closes it — on press, no context menu.
function outsideBig(tg: HTMLElement) {
  if (tg === grid || tg.classList.contains("app") || tg === document.body) return true;
  const st = tg.closest("#stage");
  return !!st && !tg.closest(".host, button");
}
window.addEventListener("pointerdown", (e) => {
  if (e.button !== 2 || !prefs.big || !outsideBig(e.target as HTMLElement)) return;
  e.preventDefault();
  closeBig();
}, true);
window.addEventListener("contextmenu", (e) => { if (outsideBig(e.target as HTMLElement)) e.preventDefault(); }, true);
$("#stage").addEventListener("contextmenu", (e) => { if (!(e.target as HTMLElement).closest(".host")) e.preventDefault(); });

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
type UIData = { keys?: Partial<Record<Act, string>>; phrases?: string[]; notes?: string; video?: Video };
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
  fillVideo(); retuneAll();
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
    ["Mở màn hình lớn", "Nút ⤢ trên ô máy / nhấp đúp thanh tên"],
    ["Đóng màn hình lớn", "Nhấp chuột phải ra ngoài màn lớn"],
  ]],
  ["Chế độ điều khiển màn hình lớn", [
    ["Mở đa nhiệm của điện thoại", "Nhấn và giữ chuột phải"],
    ["Quay về trang chủ điện thoại", "Nhấp chuột giữa"],
    ["Quay lại cấp trên của điện thoại", "Nhấp chuột phải"],
    ["Quay lại cấp trên của điện thoại", "Esc"],
  ]],
  ["Chế độ điều khiển màn hình nhỏ", [
    ["Bật / tắt: nhấp màn nhỏ = phóng to", "Ctrl + S"],
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

// ---------------------------------------------------------------- theme (Graphite & Paper / Carbon Dark)
const themeBtn = $("#themeBtn");
let curTheme: "light" | "dark" = lsGet("iosview.theme") === "dark" ? "dark" : "light";
function applyTheme(next: "light" | "dark", save = true) {
  curTheme = next;
  document.documentElement.dataset.theme = next;
  if (save) lsSet("iosview.theme", next);
  themeBtn.innerHTML = ico(next === "dark" ? "sun" : "moon");
  themeBtn.title = next === "dark" ? "Chuyển sang Graphite & Paper (sáng)" : "Chuyển sang Carbon Dark (tối)";
  themeBtn.setAttribute("aria-label", themeBtn.title);
}
applyTheme(curTheme, false);
onPress(themeBtn, () => applyTheme(curTheme === "dark" ? "light" : "dark"), true);

renderButtons();
connectControl();
// ---------------------------------------------------------------- Wails window chrome
// Frameless Wails window: our own title bar (drag area + min/max/close via the
// Wails runtime, which only exists on the wails.localhost origin).
if (WAILS) {
  document.documentElement.classList.add("wails");
  if (PARAMS.get("mica") === "1") document.documentElement.classList.add("mica");
  const rt = document.createElement("script");
  rt.type = "module"; rt.src = "/wails/runtime.js";
  document.head.appendChild(rt);
  const tb = document.createElement("div");
  tb.id = "titlebar"; tb.className = "titlebar";
  tb.innerHTML =
    `<img class="tb-mk" src="tb-20.png" srcset="tb-20.png 1x, tb-40.png 1.25x, tb-48.png 1.5x, tb-64.png 2x" alt=""><span class="tb-title"><b>iOSView</b><span id="tbVer"></span></span>` +
    `<div class="tb-btns">` +
    `<button id="tbMin" title="Thu nhỏ"><svg viewBox="0 0 10 10"><path d="M1 5h8"/></svg></button>` +
    `<button id="tbMax" title="Phóng to / khôi phục"><svg viewBox="0 0 10 10"><rect x="1.5" y="1.5" width="7" height="7" rx="1"/></svg></button>` +
    `<button id="tbClose" class="tb-close" title="Đóng"><svg viewBox="0 0 10 10"><path d="M2 2l6 6M8 2l-6 6"/></svg></button>` +
    `</div>`;
  document.body.prepend(tb);
  // Move theme toggle and panel collapse controls up to the Wails title bar
  const cb = $("#collapseBtn");
  cb.classList.add("tb-panel");
  themeBtn.classList.add("tb-theme");
  const tbBtns = tb.querySelector(".tb-btns")!;
  tbBtns.prepend(cb);
  tbBtns.prepend(themeBtn);
  type WailsWin = { Minimise(): void; ToggleMaximise(): void; Close(): void };
  const win = () => (window as unknown as { wails?: { Window: WailsWin } }).wails?.Window;
  $("#tbMin").onclick = () => win()?.Minimise();
  $("#tbMax").onclick = () => win()?.ToggleMaximise();
  $("#tbClose").onclick = () => win()?.Close();
  tb.ondblclick = (e) => { if (!(e.target as HTMLElement).closest(".tb-btns")) win()?.ToggleMaximise(); };
}

fetch("/api/version", { cache: "no-store" }).then((r) => r.json()).then((v) => {
  if (!v.version) return;
  $("#appVer").textContent = "v" + v.version;
  document.title = "iOSView " + v.version;
  const tv = document.getElementById("tbVer");
  if (tv) tv.textContent = v.version;
}).catch(() => { /* ignore */ });

// ---------------------------------------------------------------- internal updates
// The agent compares this exe with the one published on Cloudflare Pages (at
// start, every 30 min, or on demand). A new build shows a small "Cập nhật"
// button; clicking it downloads the exe, then the program closes, swaps the
// file and opens again.
type Upd = { current: string; latest?: string; available: boolean; downloading: boolean; progress: number; error?: string };
const updBtn = $<HTMLButtonElement>("#updBtn");
let updPoll = 0;
function showUpd(u: Upd) {
  updBtn.hidden = !u.available && !u.downloading;
  updBtn.disabled = u.downloading;
  updBtn.textContent = u.downloading ? `Đang tải ${u.progress}%` : `Cập nhật${u.latest ? " " + u.latest : ""}`;
  updBtn.title = u.downloading ? "Tải xong chương trình tự đóng và mở lại bản mới"
    : `Có bản mới ${u.latest || ""} (đang dùng ${u.current}) — nhấp để tải và thay`;
}
async function getUpd(check = false): Promise<Upd | null> {
  try { return await (await fetch("/api/update" + (check ? "?check=1" : ""), { cache: "no-store" })).json(); } catch { return null; }
}
async function refreshUpd() { const u = await getUpd(); if (u) showUpd(u); }
async function checkUpdNow() {
  toast("Đang kiểm tra cập nhật…");
  const u = await getUpd(true);
  if (!u) { toast("Không kiểm tra được cập nhật"); return; }
  showUpd(u);
  toast(u.error ? u.error : u.available ? `Có bản mới ${u.latest || ""} — bấm "Cập nhật" ở góc dưới bảng điều khiển` : `Đang dùng bản mới nhất (${u.current})`);
}
updBtn.onclick = async () => {
  const r = await fetch("/api/update", { method: "POST" }).catch(() => null);
  const u = r ? await r.json().catch(() => null) : null;
  if (!r || !r.ok) { toast(u?.error || "Không bắt đầu cập nhật được"); return; }
  showUpd(u);
  clearInterval(updPoll);
  updPoll = window.setInterval(async () => {
    const s = await getUpd();
    if (!s) return;   // the program is restarting
    showUpd(s);
    if (!s.downloading) { clearInterval(updPoll); if (s.error) toast(s.error); }
  }, 400);
};
$("#updCheck").onclick = checkUpdNow;
$("#appVer").onclick = checkUpdNow;
refreshUpd();
setInterval(refreshUpd, 60_000);   // local call only; the agent itself checks online every 30 min

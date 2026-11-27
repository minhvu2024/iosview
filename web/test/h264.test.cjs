const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const { runInNewContext } = require("node:vm");
const { equal, deepEqual } = require("node:assert/strict");
const { test } = require("node:test");
const { transformSync } = require("esbuild");

const source = transformSync(readFileSync(join(__dirname, "../src/h264.ts"), "utf8"), {
  loader: "ts", format: "cjs",
}).code;

function fixture(connected = true) {
  let now = 1000, nextTimer = 0;
  const timers = new Map();
  const requests = [];
  const setTimer = (callback, delay, repeat = 0) => {
    timers.set(++nextTimer, { callback, at: now + delay, repeat });
    return nextTimer;
  };
  const clearTimer = id => timers.delete(id);
  const advance = ms => {
    const end = now + ms;
    for (;;) {
      const next = [...timers].filter(([, timer]) => timer.at <= end)
        .sort((a, b) => a[1].at - b[1].at)[0];
      if (!next) break;
      const [id, timer] = next;
      now = timer.at;
      if (timer.repeat) timer.at += timer.repeat;
      else timers.delete(id);
      timer.callback();
    }
    now = end;
  };
  class Socket {
    static OPEN = 1;
    readyState = 1;
    constructor(id) { this.id = id; }
    send(bytes) { requests.push({ id: this.id, code: bytes[0], at: now }); }
    close() { this.readyState = 3; }
  }
  class Decoder {
    decodeQueueSize = 0;
    constructor({ output }) { this.output = output; }
    configure(config) { this.state = "configured"; this.config = config; }
    decode(chunk) {
      if (this.stalled && this.config.hardwareAcceleration !== "prefer-software") return;
      this.output({ timestamp: chunk.timestamp, displayWidth: 188, displayHeight: 334, close() {} });
    }
    close() { this.state = "closed"; }
  }
  const module = { exports: {} };
  runInNewContext(source, {
    module, WebSocket: Socket, VideoDecoder: Decoder, EncodedVideoChunk: Object,
    performance: { now: () => now },
    window: { setTimeout: setTimer, setInterval: (cb, ms) => setTimer(cb, ms, ms) },
    clearTimeout: clearTimer, clearInterval: clearTimer,
    requestAnimationFrame: cb => setTimer(cb, 16),
  });
  const canvas = () => ({ getContext: () => ({ setTransform() {}, drawImage() {} }) });
  const views = Array.from({ length: 20 }, (_, id) => new module.exports.H264View({
    querySelector: canvas,
  }, id, 5, { fps: 30, pct: 50 }));
  const packet = (view, key = false, native = false, token = 0) => {
    const frame = new ArrayBuffer(12);
    const header = new DataView(frame);
    header.setUint16(4, 188);
    header.setUint16(6, 334);
    header.setUint8(8, Number(key) | (native ? 16 : 0));
    header.setUint16(10, token);
    view.ws.onmessage({ data: frame });
  };
  if (connected) views.forEach(view => packet(view, true));
  return { views, requests, advance, packet };
}

test("a native refresh only recovers after a matching captured frame is painted", () => {
  const { views, advance, packet, requests } = fixture();
  const [stale, live, lost] = views;
  for (const v of [stale, live, lost]) {
    packet(v, true, true);
    v.expectFrame();
  }
  advance(1400);
  deepEqual(requests.map(r => r.code), [70, 70, 70]);
  packet(stale, true, true);
  packet(stale, true, true, 65535);
  packet(live, true, true, live.probeToken);
  lost.dec.stalled = true;
  packet(lost, true, true, lost.probeToken);
  lost.dec.stalled = false;
  packet(lost, true, true);
  advance(20);
  equal(live.probeAt, 0);
  advance(2000);
  equal(stale.closed, true);
  equal(lost.closed, true);
  equal(live.closed, false);
});

test("a queued USB handshake can deliver its first keyframe after the decoder timeout", () => {
  const { views, advance, packet } = fixture(false);
  const view = views[0];
  advance(3500);
  equal(view.closed, false);
  packet(view);
  advance(500);
  equal(view.closed, false);
  packet(view, true);
  advance(20);
  equal(view.gotFrame, true);
});

test("healthy group input cancels the recovery deadline", () => {
  const { views, requests, advance, packet } = fixture();
  advance(500);
  views.forEach(view => view.expectFrame());
  advance(50);
  views.forEach(view => packet(view));
  advance(1500);
  deepEqual(requests, []);
});

test("forced captures are staggered and skip devices that recovered in the queue", () => {
  const { views, requests, advance, packet } = fixture();
  advance(500);
  views.forEach(view => view.expectFrame());
  advance(1200);
  deepEqual(requests.map(request => request.id), [0, 1]);
  views.slice(2).forEach(view => packet(view));
  advance(1500);
  equal(requests.length, 2);
  equal(requests[1].at - requests[0].at, 100);
  equal(requests[0].code, 82);
  equal(views.slice(2).some(view => view.closed), false);
});

test("quiet views defer captures and start gradually after the idle interval", () => {
  const { views, requests, advance, packet } = fixture();
  advance(12000);
  deepEqual(requests, []);
  advance(300);
  deepEqual(requests.map(request => request.id), [0, 1, 2]);
  views.forEach(view => packet(view));
  advance(2000);
  equal(requests.length, 3);
});

test("a lone stalled H.264 decoder switches while other devices keep rendering", () => {
  const { views, requests, advance, packet } = fixture();
  const [bad, ...healthy] = views;
  bad.dec.stalled = true;
  let switched = 0, paints = 0;
  bad.onSoftwareFrame = () => { switched++; };
  healthy.forEach(view => { view.onframe = () => { paints++; }; });
  for (let i = 0; i < 20; i++) {
    views.forEach(view => packet(view));
    advance(50);
  }
  packet(bad, true);
  advance(20);
  equal(switched, 1);
  equal(bad.dec.config.hardwareAcceleration, "prefer-software");
  equal(paints, 19 * 20);
  deepEqual(requests.map(request => [request.id, request.code]), [[0, 75]]);
});

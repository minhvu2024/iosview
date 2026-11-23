"""Fake iosview-cd for tests: same stdout protocol and HTTP API, records every
POST to $FAKE_CD_LOG (one JSON line per request). /stream.bin sends synthetic
frames (not decodable video: for checking the plumbing only)."""
import argparse, json, os, struct, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ap = argparse.ArgumentParser()
ap.add_argument("--udid"); ap.add_argument("--port", type=int)
a = ap.parse_args()
log_path = os.environ["FAKE_CD_LOG"]
seq = {"n": 1}

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *x): pass
    def reply(self, code, body=b"ok", ctype="text/plain"):
        self.send_response(code); self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n).decode() if n else ""
        with open(log_path, "a", encoding="utf-8") as log:
            log.write(json.dumps({"path": self.path, "body": body}, ensure_ascii=False) + "\n")
        self.reply(200, b'{"ok":true}', "application/json")
    def do_GET(self):
        if self.path == "/codec":
            return self.reply(200, json.dumps({"codec": "hvc1.1.6.L153.B0", "description": "AQIDBA=="}).encode(), "application/json")
        if self.path == "/stream.bin":
            self.send_response(200); self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Transfer-Encoding", "chunked"); self.end_headers()
            try:
                for i in range(100000):
                    typ = 0 if i % 30 == 0 else 1
                    data = bytes([typ]) + b"F%06d" % i
                    msg = struct.pack(">I", len(data)) + data
                    self.wfile.write(b"%x\r\n" % len(msg) + msg + b"\r\n"); self.wfile.flush()
                    time.sleep(1 / 30)
            except OSError:
                pass
            return
        if self.path.startswith("/clipboard/events"):
            if "since" not in self.path:
                return self.reply(200, json.dumps({"seq": 1, "text": None}).encode(), "application/json")
            if seq["n"] == 1:  # one copy made "on the phone"
                seq["n"] = 2
                return self.reply(200, json.dumps({"seq": 2, "text": "từ iPhone"}).encode(), "application/json")
            threading.Event().wait(30)
            return self.reply(200, json.dumps({"seq": 2, "text": None}).encode(), "application/json")
        self.reply(404, b"no")

srv = ThreadingHTTPServer(("127.0.0.1", a.port), H)
print("STATUS starting", flush=True)
print("READY " + json.dumps({"port": a.port, "width": 1179, "height": 2556, "name": "Fake 15", "ios": "17.5"}), flush=True)
threading.Thread(target=srv.serve_forever, daemon=True).start()
try:
    while sys.stdin.read(1):
        pass
except Exception:
    pass
os._exit(0)

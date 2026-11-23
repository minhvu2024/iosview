"""iosview-cd: CoreDevice bridge for one iPhone (iOS 17.4+, no jailbreak).

iOSView starts one copy per phone:  iosview-cd --udid <UDID> --port <PORT>

It opens pymobiledevice3's no-admin userspace RSD tunnel over the cable,
mounts the Developer Disk Image if the display service is missing, and serves
pymobiledevice3's ScreenStreamServer on 127.0.0.1:<PORT> (HEVC video, touch,
buttons, virtual keyboard, pasteboard). iOSView talks to that HTTP API.

Lines on stdout (read by iOSView):
  STATUS <text>   progress while starting (shown on the phone's tile)
  READY <json>    serving: {"port", "width", "height", "name", "ios"}
  ERROR <text>    could not start (then exits)
The process exits when iOSView closes stdin.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import os
import sys
import threading

DISPLAY_SERVICE = "com.apple.coredevice.displayservice"


def emit(kind: str, text: str) -> None:
    print(kind, text.replace("\n", " "), flush=True)


def watch_parent() -> None:
    """Exit as soon as iOSView goes away (it holds our stdin open)."""
    try:
        while sys.stdin.read(1):
            pass
    except Exception:
        pass
    os._exit(0)


def _ints(d, keys):
    """First int found under any key containing one of `keys` (nested dicts)."""
    if isinstance(d, dict):
        for k, v in d.items():
            if isinstance(v, int) and any(s in k.lower() for s in keys):
                return v
        for v in d.values():
            r = _ints(v, keys)
            if r:
                return r
    if isinstance(d, list):
        for v in d:
            r = _ints(v, keys)
            if r:
                return r
    return 0


async def screen_size(rsd) -> tuple[int, int]:
    try:
        from pymobiledevice3.remote.core_device.device_info import DeviceInfoService

        async with DeviceInfoService(rsd) as s:
            info = await s.get_display_info()
        w, h = _ints(info, ("width",)), _ints(info, ("height",))
        if w and h:
            return (min(w, h), max(w, h))  # portrait
    except Exception:
        logging.getLogger("iosview-cd").debug("display info failed", exc_info=True)
    return (0, 0)


async def announce_when_listening(port: int, ready: dict) -> None:
    """serve() first starts the device streams (up to ~25 s), then binds the
    port: say READY only once iOSView can actually connect."""
    while True:
        try:
            _, w = await asyncio.open_connection("127.0.0.1", port)
            w.close()
            break
        except OSError:
            await asyncio.sleep(0.2)
    emit("READY", json.dumps(ready))


def plain(error: BaseException) -> str:
    name, text = type(error).__name__, (str(error).strip() or type(error).__name__)
    if name in {"DeveloperModeIsNotEnabledError", "DeveloperModeError"}:
        return "Chưa bật Developer Mode trên iPhone (Cài đặt › Quyền riêng tư & Bảo mật › Chế độ nhà phát triển)"
    if name == "UserspaceTunnelUnavailableError" or "CoreDeviceProxy" in text:
        return "iPhone không mở được kết nối CoreDevice: cần iOS 17.4+, bấm Tin cậy và bật Developer Mode"
    if "PasswordProtected" in text or "locked" in text.lower():
        return "Mở khoá iPhone rồi thử lại"
    if "PairingDialog" in text or "UserDeniedPairing" in text or "InvalidHostID" in text:
        return "Mở khoá iPhone và bấm Tin cậy máy tính này"
    if name == "ConnectionFailedToUsbmuxdError":
        return "Dịch vụ Apple Mobile Device chưa chạy — cài Apple Devices (Microsoft Store) rồi cắm lại iPhone"
    if name == "DeviceNotFoundError":
        return "Không thấy iPhone trên cáp USB"
    return f"{name}: {text}"[:300]


async def run(udid: str, port: int) -> None:
    from pymobiledevice3.remote.core_device.screen_stream import ScreenStreamServer
    from pymobiledevice3.remote.rsd_tunnel import PreferredRsdTunnel
    from pymobiledevice3.services.mobile_image_mounter import auto_mount

    for attempt in range(2):
        emit("STATUS", "Đang mở kết nối CoreDevice…")
        async with PreferredRsdTunnel(serial=udid, autopair=True) as rsd:
            services = (rsd.peer_info or {}).get("Services", {})
            if DISPLAY_SERVICE in services:
                w, h = await screen_size(rsd)
                props = (rsd.peer_info or {}).get("Properties", {})
                server = ScreenStreamServer(rsd, bind="127.0.0.1", http_port=port, audio_default_on=False)
                ready = {
                    "port": port,
                    "width": w,
                    "height": h,
                    "name": props.get("DeviceName") or props.get("ProductType") or "",
                    "ios": rsd.product_version,
                }
                emit("STATUS", "Đang khởi động luồng hình…")
                announce = asyncio.create_task(announce_when_listening(port, ready))
                try:
                    await server.serve()
                finally:
                    announce.cancel()
                return
            if attempt:
                raise RuntimeError("Đã gắn Developer Disk Image nhưng iPhone vẫn chưa có dịch vụ màn hình — rút cáp cắm lại")
            emit("STATUS", "Đang gắn Developer Disk Image (lần đầu sẽ tải về)…")
            await auto_mount(rsd)


def main() -> None:
    ap = argparse.ArgumentParser(prog="iosview-cd")
    ap.add_argument("--udid", required=True)
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--verbose", action="store_true")
    a = ap.parse_args()
    logging.basicConfig(level=logging.INFO if a.verbose else logging.WARNING, stream=sys.stderr,
                        format="%(levelname)s %(name)s: %(message)s")
    threading.Thread(target=watch_parent, daemon=True).start()
    try:
        asyncio.run(run(a.udid, a.port))
        emit("ERROR", "Luồng CoreDevice đã dừng")
    except KeyboardInterrupt:
        pass
    except BaseException as e:  # noqa: BLE001 - report everything to iOSView
        logging.getLogger("iosview-cd").warning("stopped", exc_info=True)
        emit("ERROR", plain(e))
    sys.stdout.flush()
    os._exit(1)


if __name__ == "__main__":
    main()

# iosview-cd — iPhone iOS 17.4+ không jailbreak

iOSView điều khiển iPhone **không jailbreak, không TrollStore** (iOS 17.4 trở lên) qua dịch vụ CoreDevice của Apple (cùng dịch vụ Xcode dùng cho cửa sổ thiết bị). Phần này cần `pymobiledevice3` (Python), nên chạy thành một chương trình phụ `iosview-cd`: iOSView tự mở một bản cho mỗi iPhone như vậy.

## Trên iPhone

1. Cắm cáp USB vào PC, mở khoá, bấm **Tin cậy**.
2. Bật **Cài đặt › Quyền riêng tư & Bảo mật › Chế độ nhà phát triển** (máy khởi động lại, mở khoá, bấm **Bật**). Nếu chưa thấy mục này: cắm máy vào iOSView một lần rồi xem lại.
3. Lần đầu iOSView tự tải và gắn Developer Disk Image (cần mạng, mất một lúc).

Không cần cài app nào trên iPhone. Hiện chỉ chạy qua **cáp USB**.

## Trên PC

Cách 1 (máy nhân viên): build một lần rồi chép thư mục `iosview-cd\` cạnh `iosview-<phiên bản>.exe`:

> Cần **Python 3.13, 3.12 hoặc 3.11** (python.org, 64-bit). Python 3.14 chưa dùng được: `pyimg4` ghim `pylzss==0.3.4`, bản này chưa có bản dựng sẵn cho 3.14. Cài song song với 3.14 được, script tự chọn qua `py -3.13`.

```powershell
cd D:\CODE\iosview
.\sidecar\build-sidecar.ps1
```

Cách 2 (máy có Python): `pip install -U pymobiledevice3` rồi chép `sidecar\iosview_cd.py` cạnh `iosview-<phiên bản>.exe`.

iOSView tìm theo thứ tự: biến môi trường `IOSVIEW_CD` → `iosview-cd.exe` → `iosview-cd\iosview-cd.exe` → `iosview_cd.py` (chạy bằng `py -3`/`python`).

## Hoạt động thế nào

- iOSView thấy iPhone cắm cáp, đọc phiên bản iOS. Cổng TrollVNC đóng **và** iOS ≥ 17.4 → máy đó dùng CoreDevice (`kind: "cd"`). Máy có TrollVNC vẫn dùng TrollVNC như cũ.
- Hình: HEVC từ bộ mã hoá của iPhone, giải mã bằng GPU qua WebCodecs (cần card đồ hoạ giải mã được HEVC). Không chỉnh bitrate/fps từ PC được (iPhone tự quyết, khoảng 6 Mbps).
- Chạm, vuốt, nút cứng, bàn phím ảo HID, đồng bộ nhóm: giống máy TrollVNC.
- **Dán (Ctrl+V / ô Gõ văn bản):** nạp clipboard iPhone rồi bấm ⌘V, giữ nguyên dấu tiếng Việt, tiếng Nhật, emoji. Dán cưỡng bức (Ctrl+D) vẫn gõ từng phím.
- Copy trên iPhone tự về clipboard PC.
- Nhật ký của iosview-cd nằm trong `iosview.log` (dòng `cd <udid>:`).

## Giao thức (iOSView ⇄ iosview-cd)

`iosview-cd --udid <UDID> --port <PORT>`, in ra stdout: `STATUS <chữ>` khi đang khởi động, `READY {"port","width","height","name","ios"}` khi sẵn sàng, `ERROR <chữ>` khi lỗi. Thoát khi iOSView đóng stdin. HTTP trên `127.0.0.1:<PORT>` là `ScreenStreamServer` của pymobiledevice3: `/touch`, `/button`, `/key`, `/clipboard`, `/clipboard/events`, `/codec`, `/stream.bin`, `/pli`.

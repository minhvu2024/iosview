# TrollVNC 3.3 (H.264) build sẵn cho iOSView (máy jailbreak)

Bản này **thêm luồng H.264 nén bằng chip video của iPhone** (cổng 5911). Hình nét hơn (độ phân giải gốc),
máy mát hơn, băng thông thấp hơn JPEG nhiều lần. VNC cổng 5901 vẫn giữ nguyên để điều khiển và dự phòng.

Cấu hình sẵn: cổng 5901, H.264 cổng 5911 (2500 kbps, iOSView tự chỉnh theo ô), không mật khẩu, Scale 0.5,
FPS 30-60, Keep-Alive 30s, Clipboard bật, tắt thông báo.

| Máy của bạn | File cài |
|---|---|
| Dopamine RootHide | `TrollVNC-iosview_3.3-1003_roothide.deb` |
| Dopamine, palera1n rootless | `TrollVNC-iosview_3.3-1003_rootless.deb` |
| unc0ver, checkra1n, Taurine, palera1n rootful | `TrollVNC-iosview_3.3-1003_rootful.deb` |

Cách cài: gửi file .deb sang iPhone → mở bằng Sileo (hoặc Filza) → Install (cài đè lên bản 3.2 được).
Server tự chạy lại sau khi cài, không cần respring.

Kiểm tra: mở iOSView, bật ⚙ → "Hiện fps / băng thông". Ô hiện **H.264** là đã chạy luồng mới;
ô hiện **JPEG** là máy đó chưa có bản mới (hoặc H.264 lỗi) → iOSView tự dùng JPEG như cũ.

**Bản 3.3-1003**: máy tự báo mã UDID cho iOSView, nên cùng một iPhone cắm USB + bật Wi‑Fi được gộp thành **một ô**
(ưu tiên USB). Máy đang ở 3.3-1002 vẫn chạy bình thường, chỉ cần bấm ⛓ để gộp thủ công (hoặc cài 3.3-1003).

**Bản 3.3-1004**: iOSView chỉnh được **fps (5–60) và độ phân giải (25–100 %)** của luồng H.264 ngay trên PC
(⚙ Cài đặt → Hình H.264, hoặc chuột phải từng máy → Hình riêng cho máy này…). Máy ở lưới mặc định 30 fps · 50 %,
máy ở màn lớn 60 fps · 100 % → iPhone mát hơn nhiều. Từ bản này, đổi thông số không cần cài lại iPhone nữa.
Máy còn ở 3.3-1003 vẫn chạy bình thường (luôn 100 %, fps theo Frame Rate trong TrollVNC).

File cài sẵn: `TrollVNC-iosview_3.3-1004_roothide.deb` (Dopamine RootHide), `TrollVNC-iosview_3.3-1004_rootless.deb`. Máy rootful dùng tiếp 3.3-1003. Hoặc thêm kho Sileo https://iosview.pages.dev/ (sau khi đẩy bằng `deploy-repo.bat`).

Tự build 3.3-1004 trong repo TrollVNC đã fork (đang ở trạng thái 1003):
1. Chép `trollvnc-h264/H264Streamer.mm` và `H264Streamer.h` đè vào `src/`.
2. Áp `trollvnc-h264/update-1003-to-1004.patch` (`git apply`), hoặc sửa tay đúng khối `TVH264Configure(...)` trong `src/trollvncserver.mm` như trong patch.
3. Đổi `PACKAGE_VERSION` trong `Makefile` thành `3.3-1004`, chạy workflow **Build TrollVNC**, cài `.deb` roothide.
(Repo mới từ commit gốc: áp `trollvnc-h264.patch` thay cho bước 2.)

Muốn quay lại bản cũ: cài lại `TrollVNC-iosview_3.2_*.deb`.

Mã nguồn phần thêm: thư mục `trollvnc-h264/` (H264Streamer.mm + patch cho TrollVNC, commit gốc trong BASE_COMMIT).

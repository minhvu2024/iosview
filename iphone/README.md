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

Muốn quay lại bản cũ: cài lại `TrollVNC-iosview_3.2_*.deb`.

Mã nguồn phần thêm: thư mục `trollvnc-h264/` (H264Streamer.mm + patch cho TrollVNC, commit gốc trong BASE_COMMIT).

# TrollVNC 3.3 (H.264) build sẵn cho iOSView (máy jailbreak)

Bản này **thêm luồng H.264 nén bằng chip video của iPhone** (cổng 5911). Hình nét hơn (độ phân giải gốc),
máy mát hơn, băng thông thấp hơn JPEG nhiều lần. VNC cổng 5901 vẫn giữ nguyên để điều khiển và dự phòng.

Cấu hình sẵn: cổng 5901, H.264 cổng 5911 (2500 kbps, iOSView tự chỉnh theo ô), không mật khẩu, Scale 0.5,
FPS 30-60, Keep-Alive 30s, Clipboard bật, tắt thông báo.

| Máy của bạn | File cài |
|---|---|
| Dopamine RootHide | `TrollVNC-iosview_3.3-1008_roothide.deb` |
| Dopamine, palera1n rootless | `TrollVNC-iosview_3.3-1008_rootless.deb` |
| unc0ver, checkra1n, Taurine, palera1n rootful | `TrollVNC-iosview_3.3-1008_rootful.deb` |

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

**Bản 3.3-1005**: tách luồng encode H.264 sang hàng đợi ưu tiên cao riêng (`trollvnc.h264.enc`, không tranh Main Thread với cảm ứng trên iPhone 6s/7), chống bão Keyframe khi vuốt đồng bộ nhiều máy qua Wi‑Fi (`kMaxQueuedPerClient = 8`, `SO_SNDBUF = 128 KB`), zero-copy `CMBlockBuffer`, làm lệch nhịp frame làm nét (`refine`) giữa các máy.

**Bản 3.3-1006**: sửa lỗi màn hình đen trên **iPhone 8 (chip A11)** khi gặp video nặng — bỏ trần cứng `DataRateLimits` 1 giây làm rớt Keyframe/lỗi `-12911`, tự động phục hồi tức thì khi `VTCompressionSession` báo lỗi ở cả `vtOutput` lẫn `EncodeFrame` và tự chuyển sang `Main Profile` + bộ điều khiển bitrate chuẩn ổn định, đồng thời gắn cờ reset decoder cho PC.

**Bản 3.3-1007**: chuẩn hoá ánh xạ điểm chạm sát biên màn hình (`vncPointToDevicePoint` + chuẩn hoá `[0..1]`), tự nhiên hoá thông số vật lý cảm ứng (`majorRadius` / `minorRadius` elip, `twist`, sub-pixel), và hãm quán tính cuộn chuột (`scrollDampedWithStartPoint`).

**Bản 3.3-1008**: tối ưu đường chạm và phím. Patch hiện có vẫn giữ 46 ms ở cuối cử chỉ cuộn bằng con lăn; đánh thức `gEncQ` phát ngay Keyframe trên màn hình tĩnh khi PC xin `'K'` hoặc khi drop backlog (chống đơ hình 5.5 giây) và thêm `gSessionGen` chặn `recoverFromEncoderError` khởi tạo lại encoder trùng 2 lần. Đã build sẵn cả 3 gói `roothide`, `rootless`, `rootful` trong `iphone/` và cập nhật chỉ mục `iphone/repo/`.

Muốn quay lại bản cũ: cài lại `TrollVNC-iosview_3.2_*.deb`.

Mã nguồn thử nghiệm sau 3.3-1008 đã thêm nhịp thử lại tăng dần khi VideoToolbox lỗi liên tiếp. Gói 3.3-1008 trong kho vẫn là bản đã build trước thay đổi này. Giới hạn hàng đợi gửi hiện là 8 khung. Cần đo A/B với 3 khung trên cụm iPhone qua Wi‑Fi trước khi đổi mặc định.

Mã nguồn thử nghiệm cũng bỏ qua các khung cũ chưa bắt đầu gửi khi một kết nối đầy hàng đợi. Gói TCP đang ghi được gửi hết trước khi đồng bộ lại bằng keyframe. Nhật ký chi tiết của TrollVNC có dòng `H264: capture=… copy=… superseded=… cpuCopy=… encodeCalls=…` tổng hợp khoảng một giây khi đang nhận hình. `superseded` đếm các bản sao đang chờ encode bị khung mới thay thế, `cpuCopy` đếm lượt dùng đường sao chép CPU, và `encodeCalls` gồm cả khung làm nét sau khi dừng cuộn.

Thử RootHide trên một máy với cùng video, FPS, độ phân giải và bitrate. So USB rồi Wi‑Fi, mỗi lần 10–15 phút, lần lượt với gói 1008 và gói thử. Trên PC 0.11.14, bật FPS rồi trỏ vào tên máy để xem thời gian từ lần nhận gói, giải mã và vẽ gần nhất, cùng p95/p99 khoảng cách giữa hai lần vẽ trong kỳ thống kê. Đọc các khoảng cách này trong lúc video đang phát vì màn hình tĩnh cũng có khoảng nghỉ dài.

Mã nguồn phần thêm: thư mục `trollvnc-h264/` (H264Streamer.mm + patch cho TrollVNC, commit gốc trong BASE_COMMIT).

### Bản thử phím tắt iPhone

Trong cây nguồn TrollVNC đã có phần H.264 của iOSView, chép `trollvnc-h264/PhoneActions.h` vào `src/`, áp `phone-actions.patch` và đặt `PACKAGE_VERSION` thành `3.3-1009` khi build. Bản thử cho phép iOSView 0.11.20 lấy danh sách app, mở app theo mã định danh và yêu cầu iOS mở Trung tâm điều khiển qua phiên VNC đang điều khiển. Khi thử trên 6s/7, mở khóa máy rồi kiểm tra cả nút trên màn lớn và hai tổ hợp mặc định, từ màn hình chính và từ trong một ứng dụng.

## Bản thử 3.3-1010 cho RootHide

Gói `TrollVNC-iosview_3.3-1010_roothide.deb` dùng với iOSView 0.11.38. Khi mất hình, PC gửi yêu cầu chụp ngay trên luồng H.264 đang mở. Bit 4 trong byte cờ báo hỗ trợ lệnh `F + u16 token`. TrollVNC trả token ở byte 10–11 của khung lấy từ lần chụp sau yêu cầu. PC chỉ xác nhận khi vẽ được khung tương ứng hoặc khung mới hơn đã giải mã. PC vẫn dùng cách nối phụ với gói cũ. Khi cả hai khung đang mã hóa quá 3 giây và có khung mới đang chờ, TrollVNC thử dựng lại phiên mã hóa.


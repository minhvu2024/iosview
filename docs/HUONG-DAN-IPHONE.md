# Hướng dẫn chuẩn bị iPhone cho iOSView

Làm lần lượt 4 bước: **(A)** chọn đúng bản TrollVNC → **(B)** cài → **(C)** cấu hình → **(D)** kiểm tra kết nối với PC.
Có nhiều máy thì đọc thêm mục **(E)**: cấu hình sẵn một lần rồi cài cho cả loạt máy.

---

## A. Chọn đúng bản TrollVNC

TrollVNC cần **iOS 14 trở lên**. Bạn cần đúng bản cho môi trường trên máy:

| iPhone đang dùng | Bản cần cài | Định dạng file |
|---|---|---|
| **Dopamine** (iOS 15.0–16.6.1) | rootless | `.deb` |
| **Dopamine RootHide** | roothide | `.deb` |
| **palera1n** rootless (A8–A11, iOS 15–18) | rootless | `.deb` |
| palera1n rootful, unc0ver, checkra1n, Taurine (iOS 14) | default (rootful) | `.deb` |
| **Chỉ có TrollStore**, không jailbreak (iOS 14.0–16.6.1, 16.7 RC, 17.0) | bootstrap | `.tipa` |

> Không biết máy rootless hay roothide? Xem tên app jailbreak trên màn hình chính: `Dopamine` là rootless, `Dopamine-roothide` (hoặc có chữ RootHide) là roothide.

### Lấy file cài ở đâu

**Cách 1: mua bản dựng sẵn trên Havoc (đơn giản nhất).** Từ bản 2.0, TrollVNC dựng sẵn được bán trên kho **Havoc** (khoảng 7 USD), có cập nhật.
- Máy jailbreak: mở **Sileo** (hoặc Zebra), tìm `TrollVNC`, mua và cài trực tiếp. Sileo tự chọn đúng bản rootless/roothide.

**Cách 2: tự build miễn phí bằng GitHub Actions** (không cần Mac):
1. Đăng nhập GitHub, mở `github.com/OwnGoalStudio/TrollVNC` và bấm **Fork**.
2. Trong repo vừa fork, vào tab **Actions** và bấm **I understand my workflows, go ahead and enable them**.
3. Chọn workflow **Build TrollVNC**, bấm **Run workflow** rồi **Run workflow** lần nữa (để mặc định, hoặc xem mục E để build bản cấu hình sẵn).
4. Đợi chạy xong (dấu ✓ xanh, thường vài chục phút). Bấm vào lần chạy đó, kéo xuống phần **Artifacts** và tải gói đúng loại máy: `packages-rootless`, `packages-roothide`, `packages-default` hoặc `packages-bootstrap` (bản cho TrollStore).
5. Giải nén file `.zip` ra để lấy file `.deb` hoặc `.tipa`.

---

## B. Cài lên iPhone

### Đưa file từ PC sang iPhone
Chọn một trong các cách:
- Gửi file qua **Telegram / Zalo / iCloud Drive**. Trên iPhone mở file, bấm **Chia sẻ** rồi chọn app cài (bên dưới).
- Hoặc tạo web server tạm trên PC: mở PowerShell trong thư mục chứa file và chạy `python -m http.server 8000`. Trên iPhone mở Safari, vào `http://<IP-của-PC>:8000`, bấm vào file để tải.

### Máy jailbreak (`.deb`)
1. Mở file `.deb`, bấm **Chia sẻ**, chọn **Sileo** (hoặc Zebra / Filza).
2. Bấm **Get / Install**, sau đó **Respring** khi được hỏi.
3. Server tự chạy nền và tự bật lại sau mỗi lần respring hoặc mở lại jailbreak.

### Máy chỉ có TrollStore (`.tipa`)
1. Mở file `.tipa`, bấm **Chia sẻ**, chọn **TrollStore**, rồi **Install**.
   (Hoặc mở TrollStore, bấm **+**, chọn **Install IPA File** và chọn file.)
2. Trên màn hình chính xuất hiện app **TrollVNC**. Mở app một lần để server chạy.
3. **Sau mỗi lần khởi động lại iPhone, phải mở app TrollVNC một lần** để bật lại server.

---

## C. Cấu hình TrollVNC

- Máy jailbreak: mở **Cài đặt (Settings) → TrollVNC**.
- Máy TrollStore: mở **app TrollVNC**. Giao diện giống hệt bên Cài đặt.

Đặt từng mục theo bảng dưới (tên mục giữ nguyên tiếng Anh như trên máy):

| Mục trên iPhone | Đặt thành | Giải thích |
|---|---|---|
| **Enabled** | BẬT | Bật server VNC |
| Desktop Name | để nguyên, hoặc đặt tên như `May-01` | Tên này hiện trên ô trong iOSView (đổi tên được ngay trong iOSView) |
| Bind Address | **để trống** | Nghe trên mọi kết nối: USB và Wi‑Fi |
| **TCP Port** | `5901` | Phải trùng với ⚙ Cài đặt của iOSView. Mọi máy nên dùng cùng một cổng |
| Full-access Password | để trống, **hoặc** đặt tối đa 8 ký tự | Nếu đặt thì **mọi máy phải cùng một mật khẩu**, và nhập mật khẩu đó vào ⚙ trong iOSView |
| View-only Password | để trống | Không cần |
| **Clipboard Sync** | BẬT | **Bắt buộc** để dùng ô gửi văn bản tiếng Việt |
| **View-only** | TẮT | Nếu bật thì máy sẽ không nhận thao tác |
| Silence Notifications | TẮT | Cả 2 công tắc thông báo đều nên **TẮT** để iPhone không hiện thông báo mỗi khi PC kết nối. (Công tắc này thực chất *bật* thông báo lần kết nối đầu, nên tắt nó mới là im lặng) |
| Connect/Disconnect Notifications | TẮT | Như trên |
| **Keep-Alive (sec)** | `30` | Giữ màn hình sáng khi PC đang kết nối. Cho phép 15–300; 0 là tắt |
| **Output Scale** | `0.5` khi xem nhiều máy; `1.0` khi chỉ vài máy | 0.5 giảm khoảng 75% dữ liệu hình, mượt hơn và đỡ nóng. iOSView tự quy đổi tọa độ nên chạm vẫn chính xác |
| **Frame Rate** | `30-60` | Gõ đúng dạng `30-60`. Máy yếu hoặc nhiều máy thì để `30` |
| Sync Interface Orientation | BẬT | Hình tự xoay theo máy |
| Orientation Offset | Disabled | Chỉ dùng cho iPad lỗi xoay |
| Natural Scroll Direction | tuỳ thích | Đảo chiều lăn chuột |
| **Modifier Mapping** | **Standard** | **Bắt buộc** là Standard, vì iOSView dùng phím Super làm ⌘ để dán văn bản (⌘V) |
| Assistive Touch Auto-Activation | TẮT | Không cần |
| Server-Side Cursor | TẮT | Không cần, tốn hiệu năng |
| Wheel Step (px) | `48` (mặc định) | Tăng lên nếu muốn lăn chuột cuộn nhanh hơn |
| HTTP Port | `0` | iOSView không dùng web client có sẵn |
| Reverse Connection → Mode | **None** | Nếu bật thì server tắt cổng 5901 và iOSView không kết nối được |
| Enable Auto-Discovery | BẬT (tuỳ chọn) | Không ảnh hưởng tới iOSView |

**Sau khi chỉnh xong, bấm nút `Apply` ở góc trên bên phải rồi chọn `Restart`** để khởi động lại server. Thay đổi chỉ có hiệu lực sau bước này.

### Cài đặt iOS nên chỉnh trên mọi máy (để thao tác đồng bộ chuẩn)
- **Màn hình & Độ sáng → Tự động khoá: Không bao giờ** (với máy chạy liên tục). Keep-Alive chỉ giữ sáng khi đang có kết nối.
- **Màn hình & Độ sáng → Thu phóng màn hình: Mặc định** trên mọi máy.
- Các máy chạy đồng bộ nên **cùng dòng máy, cùng ngôn ngữ, cùng bố cục icon, cùng phiên bản app**. Thao tác được gửi theo vị trí trên màn hình, nên giao diện lệch là chạm lệch.
- Tắt **Chế độ nguồn điện thấp** để máy không bị giảm FPS.
- Mỗi iPhone cần một IP cố định trong mạng (đặt IP tĩnh hoặc giữ IP trên router) nếu dùng Wi‑Fi.

---

## D. Kết nối với PC và kiểm tra

### Qua USB (nhanh và ổn định nhất)
1. Trên Windows cài **Apple Devices** (Microsoft Store) hoặc **iTunes**.
2. Cắm cáp và mở khoá iPhone. Bấm **Tin cậy (Trust)** và nhập mật mã máy.
3. Mở app Apple Devices để kiểm tra iPhone đã hiện ra (sau đó có thể đóng app).
4. Chạy `iosview.exe`. Máy tự xuất hiện trong lưới với nhãn **USB**.
   - Nhiều máy: dùng **hub USB có nguồn riêng**. Cổng USB của PC thường không đủ điện cho nhiều iPhone.

### Qua Wi‑Fi
1. Trên iPhone: **Cài đặt → Wi‑Fi**, bấm ⓘ cạnh mạng đang dùng và ghi lại **Địa chỉ IP** (ví dụ `192.168.1.23`).
2. PC và iPhone phải **cùng một mạng**. Wi‑Fi khách và mạng có bật "AP isolation" sẽ chặn kết nối.
3. Trong iOSView bấm **+ Wi‑Fi** và nhập IP (mỗi dòng một máy), hoặc bấm **Quét LAN** để tự tìm.

### Kiểm tra
- Ô của máy hiện **chấm xanh** và hình màn hình là đã kết nối thành công.
- Trong TrollVNC (mục **Clients**, góc trên phải) mỗi máy sẽ có **2 kết nối từ PC**: một để xem hình, một để điều khiển. Đây là bình thường. **Đừng bấm chặn (block) các kết nối này.** Nếu lỡ chặn thì vào lại TrollVNC, bấm **Apply → Restart** để xoá danh sách chặn.
- Nếu đặt mật khẩu: trong iOSView bấm **⚙**, nhập mật khẩu và bấm **Lưu**.

### Lỗi thường gặp

| Thông báo trên ô | Nguyên nhân và cách sửa |
|---|---|
| *không kết nối được usbmuxd* | Chưa cài Apple Devices/iTunes, hoặc dịch vụ *Apple Mobile Device Service* đang tắt (mở `services.msc` để kiểm tra) |
| *cổng 5901 trên iPhone đang đóng* | TrollVNC chưa chạy (máy TrollStore: mở app TrollVNC), hoặc **Enabled** đang tắt, hoặc cổng khác 5901 |
| *TrollVNC yêu cầu mật khẩu* / *Sai mật khẩu VNC* | Nhập đúng mật khẩu ở ⚙. Mọi máy phải cùng mật khẩu |
| Hình hiện nhưng chạm không ăn | **View-only** đang bật, hoặc đã nhập view-only password thay vì full-access |
| Gửi văn bản không dán được | **Clipboard Sync** đang tắt, **Modifier Mapping** không phải Standard, hoặc chưa chạm vào ô nhập chữ trên máy |
| Wi‑Fi không thấy máy | PC và iPhone khác mạng, router chặn thiết bị nói chuyện với nhau, hoặc tường lửa Windows chặn (cho phép `iosview.exe` khi Windows hỏi) |
| Hình giật hoặc trễ khi nhiều máy | Giảm **Output Scale** xuống 0.5, Frame Rate `30`, hạ chất lượng ở ⚙ iOSView, ưu tiên USB |

---

## E. Triển khai hàng loạt: cấu hình sẵn một lần, cài cho mọi máy

Không muốn chỉnh tay từng máy thì build TrollVNC kèm cấu hình sẵn (**Managed**). Cài xong là chạy đúng cấu hình, và người dùng trên máy không sửa được.

1. Trong repo TrollVNC đã fork, tạo file `prefs/TrollVNCPrefs/Resources/Managed.plist` với nội dung lấy từ file mẫu [`iphone/Managed.plist`](../iphone/Managed.plist) trong thư mục iOSView (sửa mật khẩu nếu muốn).
   - Trên GitHub: vào đúng thư mục đó, bấm **Add file → Create new file**, dán nội dung vào và **Commit**.
2. Vào **Actions → Build TrollVNC → Run workflow**. **Để ô `is_managed` KHÔNG tick**, vì tick sẽ ghi đè file của bạn bằng bản có bật thông báo.
3. Tải gói đúng loại máy trong **Artifacts** và cài như mục B.
4. Mở **Cài đặt → TrollVNC** trên máy: thấy dòng *"This TrollVNC instance is managed by your organization"* là cấu hình sẵn đã được áp dụng.

## F. Luồng H.264 nén phần cứng (TrollVNC 3.3-h264)

Từ bản `TrollVNC-iosview_3.3-h264_*.deb`, iPhone gửi thêm một luồng **H.264** ở cổng **5911**
(= cổng VNC + 10), nén bằng chip video riêng nên CPU gần như rảnh, máy mát hơn.
iOSView tự dùng luồng này khi có, máy nào chưa cài bản mới thì tự quay về JPEG.

- Cài: giống mục B, chọn đúng bản roothide/rootless/rootful, cài đè lên bản cũ.
- Xem đang chạy kiểu nào: iOSView → ⚙ → bật "Hiện fps / băng thông" → góc mỗi ô hiện `H.264` hoặc `JPEG`.
- Tắt H.264 (để so sánh): ⚙ → bỏ chọn "Dùng H.264 nén phần cứng".
- Thanh "Chất lượng" trong ⚙ điều chỉnh bitrate H.264: lưới mặc định ~1.6 Mbps/máy, khi phóng to ~2.8 Mbps.
- Khi chỉ xem bằng H.264, TrollVNC bỏ qua toàn bộ bước thu nhỏ + nén JPEG bằng CPU → máy nhẹ hơn rõ.
- Cấu hình trong `Managed.plist`: `H264Port` (0 = tắt), `H264BitrateKbps`.

### Một máy = một ô (cắm USB + bật Wi‑Fi cùng lúc)

Cùng một iPhone vừa cắm cáp vừa thêm địa chỉ Wi‑Fi vẫn chỉ hiện **một ô**, nhãn `USB+Wi‑Fi`.
iOSView luôn **ưu tiên cáp USB**; rút cáp thì tự chuyển sang Wi‑Fi, cắm lại thì quay về USB (không phải làm gì).
Ô chỉ có một kết nối điều khiển nên không còn 2 màn hình tranh nhau hay gửi thao tác 2 lần.

Cách iOSView biết hai kết nối là cùng một máy (tự làm, theo thứ tự):
1. Bản **TrollVNC 3.3-1003 trở lên** tự báo mã UDID của máy qua cổng 5911 → khớp với máy cắm cáp.
2. Máy đang bật "đồng bộ Wi‑Fi" trong iTunes/Apple Devices: Windows báo luôn địa chỉ IP của máy.
3. Máy còn bản cũ (chưa báo được UDID): ô Wi‑Fi có nút **⛓** → chọn máy USB tương ứng, chỉ cần làm một lần (được nhớ lại).

Nút **✕** trên ô có Wi‑Fi = bỏ địa chỉ Wi‑Fi đó (máy USB vẫn giữ).

Lỗi thường gặp:
- Ô vẫn hiện `JPEG`: máy chưa cài bản 3.3-h264, hoặc cổng 5911 bị chặn (Wi‑Fi). Thử cắm USB.
- Hình đứng vài giây rồi chạy lại: mạng Wi‑Fi nghẽn, iOSView đã xin khung hình mới để đồng bộ lại; hạ thanh Chất lượng.
- Mở iOSView từ máy khác qua địa chỉ IP (không phải 127.0.0.1): trình duyệt chỉ cho giải mã H.264 trên localhost/HTTPS, nên sẽ dùng JPEG.


# iOSView

Xem và điều khiển **nhiều iPhone cùng lúc** trên Windows, **đồng bộ thao tác**: chạm/vuốt/gõ phím trên một máy thì cả nhóm làm theo. Bản đầu tiên dành cho **iPhone đã jailbreak hoặc có TrollStore** (dùng TrollVNC trên máy).

- Một file `iosview.exe` (khoảng 2–3 MB, nén UPX, có icon riêng, không hiện cửa sổ console). Không cần cài thêm gì, giao diện mở bằng Edge/Chrome dạng cửa sổ app.
- Tự nhận iPhone cắm **USB** (qua dịch vụ Apple Mobile Device), hoặc thêm máy qua **Wi‑Fi** bằng IP hay **Quét LAN**.
- Thao tác chạm được truyền theo thời gian thực (nhấn, kéo, nhả). Tọa độ được quy về tỉ lệ nên máy khác độ phân giải vẫn chạm đúng chỗ.
- **Copy/paste hai chiều** PC ⇄ iPhone (Ctrl+C / Ctrl+V). Chữ không dấu gõ thẳng không cần bấm Paste; chữ có dấu nạp clipboard rồi chạm Paste. Dán đồng bộ cả nhóm.
- Phím cứng: Home, Đa nhiệm, Khoá, Vol±.

## 1. Chuẩn bị trên iPhone

Hướng dẫn chi tiết từng bước (chọn đúng bản TrollVNC, cài, chỉnh từng mục, kiểm tra kết nối, xử lý lỗi, triển khai hàng loạt): **[docs/HUONG-DAN-IPHONE.md](docs/HUONG-DAN-IPHONE.md)**.

Tóm tắt các mục cần chỉnh trong TrollVNC:
- **Enabled** BẬT, **TCP Port** `5901`, **View-only** TẮT, **Reverse Connection** None
- **Clipboard Sync** BẬT, **Modifier Mapping** Standard (cần cho tính năng gửi văn bản tiếng Việt)
- **Output Scale** `0.5`, **Frame Rate** `30-60`, **Keep-Alive** `30`
- Tắt cả 2 công tắc thông báo
- Bấm **Apply → Restart**

Có nhiều máy: dùng [`iphone/Managed.plist`](iphone/Managed.plist) để build TrollVNC đã cấu hình sẵn, cài một lần cho tất cả.

## 2. Chạy trên Windows

1. Cài **Apple Devices** (Microsoft Store) hoặc **iTunes** để có driver USB của iPhone.
2. Cắm iPhone và bấm **Tin cậy** trên máy.
3. Chạy `iosview.exe`. Cửa sổ iOSView tự mở (không có cửa sổ đen console). **Đóng cửa sổ là thoát chương trình.** Chạy lại khi đang mở sẵn thì chỉ hiện thêm cửa sổ. Nhật ký lỗi nằm ở `iosview.log` cạnh file exe.

Máy không cắm USB: bấm **+ Wi‑Fi** để nhập IP (mỗi dòng một máy), hoặc **Quét LAN**.

## 3. Thao tác

| Trên màn hình máy | Tác dụng |
|---|---|
| Chuột trái | Chạm, giữ, kéo, vuốt (theo thời gian thực) |
| Chuột phải | Nút Home |
| Chuột giữa | Nút nguồn (khoá màn hình) |
| Lăn chuột | Cuộn |
| Gõ phím khi đang trỏ vào một máy | Bàn phím PC gõ thẳng vào máy: chữ hoa/thường, số, ký hiệu, phím mũi tên, Enter, Backspace. Chữ có dấu thì dùng ô "Gõ văn bản". Nên tắt Unikey khi gõ kiểu này |
| **Ctrl+V** khi trỏ chuột vào máy | Gõ thẳng clipboard của PC vào ô đang nhập (không cần bấm Paste). Chữ có dấu tiếng Việt được **gõ bỏ dấu** (Nguyễn → Nguyen) — tắt trong ⚙ nếu muốn giữ dấu (khi đó nạp clipboard máy, chạm Paste). Cần con trỏ đang ở ô nhập. Đồng bộ BẬT thì dán vào **mọi máy trong nhóm**. Hỗ trợ tiếng Việt có dấu và emoji |
| **Ctrl+C / Ctrl+X** | *Tạm gác*: iOS 15 bỏ qua phím tắt ⌘ từ TrollVNC nên chưa chạy. Cách thay thế: chọn chữ trên iPhone bằng chuột, bấm **Copy** trên menu iPhone, nội dung tự về clipboard PC |
| **Ctrl+A** | Chọn hết văn bản trong ô (mô phỏng bằng phím mũi tên, không cần ⌘). Bấm Ctrl+V ngay sau đó để **thay thế** nội dung |
| **Ctrl+Backspace** | Xoá sạch ô nhập (chọn hết + Backspace) |
| Nút **Dán (Ctrl+V)** | Dán clipboard của PC vào máy đang chọn (hoặc cả nhóm khi bật đồng bộ) |
| Ô "Gõ văn bản…" + Enter | Gõ nhanh một đoạn văn bản rồi dán vào máy |
| Thanh nút dưới mỗi máy: **‹ Back · ○ Home · ▭ Đa nhiệm** | Back = vuốt từ mép trái (hoạt động trong app có nút quay lại, Safari, Cài đặt…). Đồng bộ BẬT và máy có ☑ thì cả nhóm cùng bấm |
| Nhấp đúp tiêu đề ô / ⤢ | Phóng to một máy (hình nét hơn); Esc để thoát |
| Nhấp đúp tên máy | Đổi tên |
| **F2** | Bật/tắt đồng bộ |

**Đồng bộ hoạt động thế nào:**
- Đồng bộ **BẬT**: thao tác trên một máy có ☑ thì mọi máy có ☑ cùng làm theo. Máy không có ☑ chỉ nhận thao tác của chính nó. Các nút trên thanh công cụ gửi tới cả nhóm.
- Đồng bộ **TẮT**: chỉ máy bạn đang thao tác nhận lệnh. Nút trên thanh công cụ gửi tới máy được chọn gần nhất (viền xanh).
- Máy mới cắm vào tự được thêm vào nhóm. Bỏ ☑ nếu không muốn máy đó chạy theo.

> Mẹo: để thao tác đồng bộ chính xác, các máy nên có **cùng tỉ lệ màn hình** (cùng dòng máy), cùng ngôn ngữ, cùng bố cục icon và cùng phiên bản app.

## 4. Kiến trúc

```
iPhone (TrollVNC :5901) ──USB/usbmuxd hoặc Wi‑Fi──▶ iosview.exe (Go)
                                                     ├─ 1 kết nối điều khiển/máy (chỉ gửi input, luôn mở)
                                                     │    └─ fan-out đồng bộ, gộp move khi máy lag
                                                     └─ proxy WebSocket ⇄ VNC cho trình xem
Trình duyệt (UI) ── noVNC (chỉ xem, Tight/JPEG) + lớp bắt chuột/phím riêng ──▶ /ws/control
```

- **Hình:** noVNC chỉ dùng để xem. Ô nằm ngoài màn hình sẽ tự ngắt luồng sau 3 giây để tiết kiệm CPU. Chế độ phóng to tự nâng chất lượng.
- **Input:** đi qua kết nối điều khiển riêng, không qua luồng hình, nên không bị nghẽn khi hình nặng. Lệnh tới từng máy chạy song song; lệnh di chuyển dồn lại được gộp, còn nhấn/nhả phím và nút không bao giờ bị bỏ.
- **USB:** client usbmuxd tự viết (giống `iproxy`), không cần cài libimobiledevice.
- File cấu hình `iosview.json` nằm cạnh file exe (tên máy, nhóm đồng bộ, danh sách máy Wi‑Fi, mật khẩu).

```
cmd/iosview         chương trình chính (mở UI, đọc cấu hình)
internal/usbmux     client usbmuxd (Windows TCP 27015, macOS/Linux unix socket)
internal/rfb        client VNC tối giản: auth, pointer, key, clipboard UTF-8, theo dõi xoay màn hình
internal/device     quản lý máy, kết nối lại, nhóm đồng bộ, fan-out, quét LAN
internal/server     HTTP + WebSocket (điều khiển, proxy VNC)
web/src             giao diện (TypeScript + noVNC), build ra web/dist rồi nhúng vào exe
tools/fakemux       usbmuxd giả để test không cần iPhone
```

## 5. Build từ mã nguồn

Cần Go ≥ 1.24 và Node ≥ 18.

```powershell
.\build.ps1           # build UI rồi iosview.exe
```

`build.ps1` / `build.sh` luôn nén exe bằng **UPX** (cần có `upx` trong PATH).

Hoặc build thủ công:

```bash
cd web && npm install && node build.mjs && cd ..
go build -ldflags "-s -w" -o iosview.exe ./cmd/iosview
upx --best --lzma iosview.exe
go test ./internal/...
```

Tham số dòng lệnh: `-listen 0.0.0.0:7788` (mở cho máy khác trong LAN; chỉ dùng trong mạng tin cậy), `-config path`, `-no-browser`, `-keep-alive` (không tự thoát khi đóng cửa sổ).

Icon nằm ở `assets/` (`icon.svg` là bản gốc). Muốn đổi icon thì tạo lại `iosview.ico` rồi chạy `rsrc -ico assets/iosview.ico -arch amd64 -o cmd/iosview/rsrc_windows_amd64.syso`.

## 6. Xử lý sự cố

- **"không kết nối được usbmuxd"**: chưa cài Apple Devices/iTunes, hoặc dịch vụ *Apple Mobile Device Service* chưa chạy.
- **"cổng 5901 trên iPhone đang đóng"**: TrollVNC chưa bật, hoặc port khác với ⚙ Cài đặt.
- **"Sai mật khẩu VNC"**: mật khẩu ở ⚙ phải khớp với TrollVNC (VNC chỉ dùng 8 ký tự đầu).
- **Một máy hiện 2 lần** (USB và Wi‑Fi): xoá ô Wi‑Fi (✕) và chỉ dùng USB.
- **Windows Defender cảnh báo exe**: exe chưa được ký số và nén UPX nên dễ bị báo nhầm. Chọn *More info → Run anyway*, hoặc tự build từ mã nguồn.

## 7. Lộ trình

- [ ] Ghi lại thao tác và phát lại (macro) cho cả nhóm
- [ ] Driver cho máy **không jailbreak** (WebDriverAgent + app Broadcast H.264), dùng chung giao diện và cơ chế đồng bộ
- [ ] Phát hiện máy qua Bonjour (mDNS)

# iOSView

Xem và điều khiển **nhiều iPhone cùng lúc** trên Windows, **đồng bộ thao tác**: chạm/vuốt/gõ phím trên một máy thì cả nhóm làm theo. Bản đầu tiên dành cho **iPhone đã jailbreak hoặc có TrollStore** (dùng TrollVNC trên máy).

- Một file `iosview-<phiên bản>.exe`, ví dụ `iosview-0.10.2.exe` (khoảng 2–3 MB, nén UPX, có icon riêng, không hiện cửa sổ console). Số phiên bản luôn nằm trong tên file, trên thanh tiêu đề cửa sổ và ở góc dưới bảng điều khiển.
- Tự nhận iPhone cắm **USB** (qua dịch vụ Apple Mobile Device), hoặc thêm máy qua **Wi‑Fi** bằng danh sách IP.
- Thao tác chạm được truyền theo thời gian thực (nhấn, kéo, nhả). Tọa độ được quy về tỉ lệ nên máy khác độ phân giải vẫn chạm đúng chỗ.
- **Copy/paste hai chiều** PC ⇄ iPhone (Ctrl+C / Ctrl+V). Chữ không dấu gõ thẳng không cần bấm Paste; chữ có dấu nạp clipboard rồi chạm Paste. Dán đồng bộ cả nhóm.
- Phím cứng: Home, Đa nhiệm, Khoá, Vol±.

> **iPhone không jailbreak (iOS 17.4+):** không cần TrollVNC. Bật Chế độ nhà phát triển, cắm cáp USB, đặt `iosview-cd` cạnh exe — xem [sidecar/README.md](sidecar/README.md). Dán giữ nguyên dấu tiếng Việt.

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
3. Chạy `iosview-<phiên bản>.exe` mới nhất. Giao diện mở trong **cửa sổ app riêng** (WebView2 có sẵn trên Windows 10/11, không cần trình duyệt). **Đóng cửa sổ là thoát chương trình.** Máy nào thiếu WebView2 Runtime thì tự dùng cửa sổ Edge/Chrome như trước. Chạy lại khi đang mở sẵn thì chỉ hiện thêm cửa sổ. Nhật ký lỗi nằm ở `iosview.log` cạnh file exe.

Máy không cắm USB: mở tab **Kết nối** ở bảng bên phải để nhập IP (mỗi dòng một máy).

### Bố cục

| Vùng | Nội dung |
|---|---|
| Lưới máy (trái) | Mỗi ô có **số máy** (01, 02… giữ nguyên khi khởi động lại, đổi được), **tên**, nhãn USB / Wi‑Fi, chấm trạng thái, nút ⤢ mở màn lớn. Lưới luôn xếp theo số từ nhỏ tới lớn. Viền **xanh** = đang chọn (thuộc nhóm đồng bộ), viền **tím** = máy đang mở ở màn lớn |
| Màn lớn | Nhấp vào **hình** là chạm, gửi ngay xuống máy. Mở: nút **⤢** trên ô máy, nhấp đúp thanh tên, nhấp đúp số trong lưới số, hoặc bật **Ctrl+S** (nhấp màn nhỏ = phóng to; tắt = nhấp/vuốt điều khiển). Đóng: **nhấp phải ra ngoài màn lớn**, Màn lớn **nổi** trên lưới. **✕** trên thanh công cụ hoặc nhấp ô "Đang ở màn lớn". **Ctrl + ←/→** chuyển sang máy trước/sau; ⤢ ở máy khác cũng chuyển, không đóng-mở. Nút **⛶ Toàn cửa sổ** trên thanh công cụ: màn lớn chiếm cả cửa sổ (ẩn lưới + bảng), bấm lại để về cỡ thường |
| Thanh công cụ màn lớn | Trung tâm điều khiển, sao chép ảnh máy đang xem, toàn cửa sổ và đóng. Menu chuột phải của từng máy có các lệnh văn bản và âm lượng. |
| Bảng điều khiển | Chọn nhóm, đồng bộ, khóa, gửi văn bản, cỡ màn hình và lọc máy. FPS và độ nét nằm trong Cài đặt → Hình H.264. |

### Đổi tên, chọn máy, nhãn

- **Đổi tên:** nhấp đúp vào tên trên ô, hoặc trỏ chuột vào máy rồi bấm **F2**, hoặc chuột phải → **Đổi tên…**, hoặc ✎ ở bảng chức năng. Enter lưu, Esc huỷ, để trống = về tên mặc định. Tên lưu trong `iosview.json` theo từng iPhone (UDID): cùng một máy cắm USB hay dùng Wi‑Fi vẫn giữ một tên và một số.
- **Đổi số máy:** nhấp đúp vào số (01, 02…) trên ô, hoặc chuột phải → **Đổi số máy…**, nhập 1–999 rồi Enter. Nếu số đó đang thuộc máy khác thì hai máy **đổi số cho nhau**. Lưới tự xếp lại theo số.
- **Chọn / bỏ chọn:** nhấp vào thanh tiêu đề của ô, hoặc nhấp số máy trong lưới số.
- **Phóng to / thu nhỏ màn hình máy:** **Ctrl + lăn chuột** (hoặc Ctrl + / Ctrl −) trên lưới đổi cỡ các ô; trên màn lớn đổi cỡ màn lớn. **Ctrl+0** về cỡ mặc định. Cửa sổ chương trình không bị zoom nữa.
- **Nhãn máy:** chọn các máy rồi bấm **Thêm nhãn**. Tick ô vuông của nhãn = chọn cả nhóm; nhấp tên nhãn = chỉ hiện máy của nhãn đó; nhấp đúp = đổi tên nhãn; ↻ = cập nhật nhãn theo các máy đang chọn.
- **Fps / độ phân giải (iPhone nóng, lag):** tab **Cài đặt → Hình H.264**. Mặc định màn nhỏ 30 fps · 50 %, màn lớn 60 fps · 100 %. Chuột phải → **Hình riêng cho máy này…**, nhập `60 100` để máy chính giữ hình ở độ phân giải gốc khi chuyển giữa lưới và màn lớn, hoặc thử `20 40` cho máy yếu. Thiết lập áp dụng ngay và cần TrollVNC 3.3‑1004 trở lên.
- **Chụp màn hình:** bấm biểu tượng máy ảnh ở màn lớn hoặc chọn **Sao chép ảnh máy này** trong menu chuột phải. Nhấn **Ctrl+V** ở ứng dụng đích để dán ảnh.

## Cập nhật cho nhân viên (bản nội bộ)

- **Anh (người build):** build xong `iosview-<phiên bản>.exe` → nhấp đúp `iphone\deploy-repo.bat`. Script lấy exe có số phiên bản cao nhất, đẩy lên `https://iosview.pages.dev/pc/` kèm `latest.json` (hash, kích thước), cùng lúc với kho Sileo.
- **Nhân viên:** iOSView tự so sánh với bản trên mạng (lúc mở, mỗi 30 phút, hoặc nhấp vào số phiên bản / Cài đặt → **Kiểm tra cập nhật**). Có bản mới: góc dưới bảng điều khiển hiện nút **Cập nhật x.y.z** → bấm → tải xong chương trình tự đóng, thay file và mở lại. Tên máy, số máy, cài đặt giữ nguyên.
- So sánh bằng mã hash nên không cần quản lý phiên bản. Mất mạng thì bỏ qua, chạy bản đang có. Nên để exe trong thư mục có quyền ghi (không để trong Program Files).

## 3. Thao tác

Danh sách phím tắt đầy đủ: [`docs/SHORTCUTS.md`](docs/SHORTCUTS.md) (trong app: tab **Cài đặt → Phím tắt…**, đổi được một số phím).

| Trên màn hình máy | Tác dụng |
|---|---|
| Chuột trái | Chạm, giữ, kéo, vuốt (theo thời gian thực) |
| Chuột phải | Quay lại (vuốt từ mép trái) |
| Giữ chuột phải | Đa nhiệm |
| Chuột giữa | Nút Home (khoá màn hình: nút **Khoá**) |
| **Esc** | Quay lại |
| Giữ **Alt** khi chạm / gõ / bấm | Chỉ máy đang trỏ nhận lệnh, kể cả khi đang đồng bộ |
| Lăn chuột | Cuộn |
| Gõ phím khi đang trỏ vào một máy | Bàn phím PC gõ thẳng vào máy: chữ hoa/thường, số, ký hiệu, phím mũi tên, Enter, Backspace. Chữ có dấu thì dùng ô "Gõ văn bản". Nên tắt Unikey khi gõ kiểu này |
| **Ctrl+V** khi trỏ chuột vào máy | Gõ thẳng clipboard của PC vào ô đang nhập (không cần bấm Paste). Chữ có dấu tiếng Việt được **gõ bỏ dấu** (Nguyễn → Nguyen) — tắt trong tab Cài đặt nếu muốn giữ dấu (khi đó nạp clipboard máy, chạm Paste). Cần con trỏ đang ở ô nhập. Đồng bộ BẬT thì dán vào **mọi máy trong nhóm**. Hỗ trợ tiếng Việt có dấu và emoji |
| **Ctrl+V nhiều dòng** | Copy **N dòng** (mỗi dòng một tài khoản) rồi Ctrl+V khi đang chọn **từ 2 máy trở lên**: dòng 1 → máy số nhỏ nhất, dòng 2 → máy kế… Các máy gõ **song song**, không bấm Enter cuối dòng. Luôn chia như vậy dù Đồng bộ bật hay tắt và đang trỏ vào máy nào; giữ **Alt** thì gõ nguyên khối vào một máy. Máy offline vẫn giữ chỗ nên không lệch dòng. **Mẹo:** trong một dòng dùng phím **Tab** để nhảy ô, ví dụ `tendangnhap<Tab>matkhau` điền cả tên đăng nhập lẫn mật khẩu trong một lần dán |
| **Ctrl+C** | Khi cửa sổ app đang focus và không focus ô nhập: lấy clipboard các máy đang chọn, hoặc tất cả máy online nếu không chọn. Chỉ lấy dữ liệu, không thêm số hoặc tên máy; giữ nguyên xuống dòng; đọc song song, timeout 800 ms/máy, lỗi để rỗng. Hỗ trợ TrollVNC trước; CoreDevice để rỗng. Không hiện thông báo. |
| **Ctrl+A** | Chọn **tất cả máy** (đưa vào nhóm đồng bộ). Chọn hết chữ trong ô: nút **Chọn hết chữ** ở thanh công cụ dưới màn lớn |
| **Shift + nhấp** / **Ctrl + nhấp** / **Ctrl + kéo** | Chọn liên tục / chọn một máy / chọn khung nhiều máy. Nhấp chỗ trống = bỏ chọn tất cả |
| **Ctrl+D** | Dán cưỡng bức: luôn gõ từng phím (bỏ dấu), cho ô không nhận Paste |
| **Ctrl+K** · **Ctrl+R** / **Ctrl+Q** · **Ctrl+,** | Cụm từ nhanh · mở / đóng ghi chú · mở cài đặt |
| **Ctrl+Backspace** | Xoá sạch ô nhập (chọn hết + Backspace) |
| Nút **Dán (Ctrl+V)** | Dán clipboard của PC vào máy đang chọn (hoặc cả nhóm khi bật đồng bộ) |
| Nút **FPS** | Hiện trên từng máy: fps, băng thông, chế độ hình, **trễ Nms** (từ lúc WebView nhận khung hình đến khi gọi xong lệnh vẽ canvas), `đợi N` khi decoder tồn đọng, `bỏ N` frame đã bỏ qua |
| Ô "Gõ văn bản…" + Enter | Gõ nhanh một đoạn văn bản rồi dán vào máy |
| Thanh nút dưới mỗi máy: **▭ Đa nhiệm · ○ Home · ‹ Back** | Back = vuốt từ mép trái (hoạt động trong app có nút quay lại, Safari, Cài đặt…). Đồng bộ BẬT và máy đang chọn thì cả nhóm cùng bấm |
| Nút ⤢ / nhấp đúp tiêu đề ô | Mở máy đó ở màn lớn; ✕ trên thanh công cụ để đóng. Toàn cửa sổ: nút ⛶ trên thanh công cụ hoặc chuột phải → Toàn cửa sổ |
| Nhấp đúp số máy | Đổi số máy |
| **Ctrl + lăn chuột** | Phóng to / thu nhỏ màn hình máy (không cuộn trên iPhone, không zoom cửa sổ) |
| Nhấp đúp tên máy / **F2** | Đổi tên máy đang trỏ chuột |
| **F4** | Bật/tắt đồng bộ (trước đây là F2) |

**Đồng bộ hoạt động thế nào:**
- Đồng bộ **BẬT**: thao tác trên một máy đang chọn (viền xanh) thì mọi máy đang chọn cùng làm theo. Máy không được chọn chỉ nhận thao tác của chính nó. Các nút trên thanh công cụ gửi tới cả nhóm.
- Đồng bộ **TẮT**: chỉ máy bạn đang thao tác nhận lệnh. Các nút nhóm gửi tới máy bạn vừa thao tác (hoặc máy chính).
- Máy mới cắm vào tự được thêm vào nhóm. Nhấp thanh tiêu đề để bỏ chọn nếu không muốn máy đó chạy theo.

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
internal/device     quản lý máy, kết nối lại, nhóm đồng bộ, fan-out
internal/server     HTTP + WebSocket (điều khiển, proxy VNC)
web/src             giao diện (TypeScript + noVNC), build ra web/dist rồi nhúng vào exe
tools/fakemux       usbmuxd giả để test không cần iPhone
```

## 5. Build từ mã nguồn

Cần Go ≥ 1.24 và Node ≥ 18.

```powershell
.\build.ps1           # build UI rồi iosview-<phiên bản>.exe
```

`build.ps1` / `build.sh` luôn nén exe bằng **UPX** (cần có `upx` trong PATH) và đặt tên file theo phiên bản (biến `VERSION` / `$Version` ở đầu script).

Hoặc build thủ công:

```bash
cd web && npm install && node build.mjs && cd ..
go build -ldflags "-s -w" -o iosview.exe ./cmd/iosview
upx --best --lzma iosview.exe
go test ./internal/...
```

Tham số dòng lệnh: `-listen 0.0.0.0:7788` (mở cho máy khác trong LAN; chỉ dùng trong mạng tin cậy), `-config path`, `-no-browser`, `-keep-alive` (không tự thoát khi đóng cửa sổ).

Icon nằm ở `assets/` (`icon.svg` là bản gốc, cũng dùng cho `web/src/icon.svg|png`). Muốn đổi icon: render SVG ra PNG 16–256 → gộp thành `iosview.ico` → `go run github.com/akavel/rsrc@v0.10.2 -ico assets/iosview.ico -arch amd64 -o cmd/iosview/rsrc_windows_amd64.syso`. Icon trong giao diện lấy từ bộ **Lucide** (bảng `IC` trong `web/src/main.ts`).

## 6. Xử lý sự cố

- **So độ mượt USB và Wi‑Fi.** iOSView ưu tiên cáp, tiếp đến IP Wi‑Fi đã liên kết, cuối cùng là kết nối Wi‑Fi qua dịch vụ Apple nếu có. Trỏ chuột vào nhãn Wi‑Fi để xem đường đang dùng. Để đo trên Windows, thoát iOSView rồi chạy trong PowerShell:

  ```powershell
  $env:IOSVIEW_H264_TIMING = "1"
  .\iosview-0.11.38.exe
  ```

  Khi có hình, `iosview.log` ghi khoảng cách lớn nhất giữa hai khung nhận đủ tại PC và thời gian ghi WebSocket lâu nhất theo từng đợt khoảng 5 giây. Lúc xin chụp lại, log ghi cả thời gian mở kết nối phụ và kết quả. So cùng một đoạn video đang chuyển động, cùng chế độ hình và FPS. Màn hình tĩnh cũng có thể tạo khoảng nghỉ dài. Để tắt đo, thoát ứng dụng, chạy `Remove-Item Env:IOSVIEW_H264_TIMING` trong cửa sổ PowerShell đó rồi mở lại.

- **"không kết nối được usbmuxd"**: chưa cài Apple Devices/iTunes, hoặc dịch vụ *Apple Mobile Device Service* chưa chạy.
- **"cổng 5901 trên iPhone đang đóng"**: TrollVNC chưa bật, hoặc port khác với tab Cài đặt.
- **"Sai mật khẩu VNC"**: mật khẩu ở tab Cài đặt phải khớp với TrollVNC (VNC chỉ dùng 8 ký tự đầu).
- **Một máy hiện 2 lần** (USB và Wi‑Fi): xoá ô Wi‑Fi (✕) và chỉ dùng USB.
- **Windows Defender cảnh báo exe**: exe chưa được ký số và nén UPX nên dễ bị báo nhầm. Chọn *More info → Run anyway*, hoặc tự build từ mã nguồn.

## 7. Lộ trình

- [ ] Ghi lại thao tác và phát lại (macro) cho cả nhóm
- [x] Driver cho máy **không jailbreak, iOS 17.4+** (CoreDevice qua `iosview-cd`, 0.10.14) — xem [sidecar/README.md](sidecar/README.md)
- [ ] Máy không jailbreak iOS 16.7–17.3 (TrollStore không cài được): WebDriverAgent
- [ ] Phát hiện máy qua Bonjour (mDNS)

Thử copy vùng chữ đang chọn trên iPhone: chạy `iosview-0.11.12.exe -copy-command` để gửi Cmd+C trước khi đọc (mặc định tắt). TrollVNC phải bật Clipboard. Clipboard chưa từng được TrollVNC gửi trong phiên có thể để rỗng; có thể bấm Copy trên menu iPhone rồi Ctrl+C trong app. Cờ Cmd+C cần được xác nhận riêng trên iOS 15.8.

Copy bằng nút trong app iPhone: bật Đồng bộ, mở cùng màn hình trên các máy và nhấp nút Copy. iOSView gom các sự kiện clipboard mới của nhóm trong tối đa 1,2 giây rồi ghi một lần lên clipboard PC theo số máy; máy không trả dữ liệu để rỗng. Không chọn máy nào thì chạm chỉ điều khiển máy đang trỏ; Ctrl+C vẫn lấy tất cả máy online. Ctrl+C yêu cầu clipboard mới trên máy hỗ trợ Extended Clipboard. Máy dùng RFB cũ có thể trả clipboard đã nhận trong phiên. Thời gian chờ tối đa 800 ms/máy. Khi dán lại nội dung vừa copy nhóm trong iOSView, dữ liệu được gửi theo ID của máy gốc. Với văn bản thường, dòng trống giữ chỗ và được bỏ qua khi gửi. Đọc clipboard không tự bấm nút Copy trong ứng dụng iPhone. Cờ `-copy-command` vẫn là thử nghiệm.

Cài đặt → **Dòng trống giữa dữ liệu copy**: tắt (mặc định) ghép dữ liệu bằng một ký tự xuống dòng; bật ghép bằng hai ký tự xuống dòng. Áp dụng cho Ctrl+C và sự kiện Copy từ iPhone, giữ nguyên xuống dòng trong từng dữ liệu.

# iOSView — hướng dẫn cho coding agent

Đọc hết file này trước khi sửa code. Chủ dự án giao tiếp bằng **tiếng Việt**: trả lời, chữ trên giao diện và tài liệu dùng tiếng Việt; comment trong code dùng tiếng Anh (giữ như hiện tại).

## 1. Dự án là gì

Ứng dụng Windows xem và **điều khiển đồng bộ nhiều iPhone cùng lúc** (kiểu phần mềm "group control" như Xiaowei/效卫投屏 cho Android). Chạm/vuốt/gõ trên một máy thì cả nhóm làm theo.

- Máy của chủ dự án: iPhone 6s / iPhone 7, iOS 15.8, jailbreak **Dopamine RootHide**. Trên iPhone chạy **TrollVNC** bản vá riêng (thêm luồng H.264 phần cứng).
- iPhone **không jailbreak, iOS 17.4+** (0.10.14): driver CoreDevice qua chương trình phụ `iosview-cd` (Python, pymobiledevice3), xem `sidecar/README.md`.
- PC: Windows, cấu hình mạnh. Điểm nghẽn hiệu năng là **phía iPhone** (nóng, lag), không phải PC.
- Repo: https://github.com/minhvu2024/iosview.git — thư mục làm việc: `D:\CODE\iosview`.

## 2. Kiến trúc

```
iPhone (TrollVNC vá)                      PC: iosview-<ver>.exe (Go)                    UI (WebView2)
 VNC :5901  ── điều khiển (chạm/phím/clipboard) ─▶ internal/device + internal/rfb ◀── /ws/control (JSON)
 H.264 :5911 ── luồng video phần cứng ──────────▶ internal/server h264Proxy ───────▶ /ws/h264/<id> → WebCodecs
            (USB qua usbmuxd hoặc Wi‑Fi)
```

| Thư mục / file | Vai trò |
|---|---|
| `cmd/iosview/` | main, cửa sổ WebView2 (`window_windows.go`, tag `windows && !wails`), không console. `window.go`: `winOpts`. `window_wails_windows.go` (tag `windows && wails`): cửa sổ **Wails v3** frameless + Mica cho bản 0.11.0 (xem mục 5) |
| `internal/device/cd.go` | driver **CoreDevice** (iOS 17.4+ không jailbreak): chạy `iosview-cd` cho từng máy, chuyển chạm/phím/nút/clipboard thành HTTP. `session.go`: interface chung cho `rfb.Client` và `cdClient` |
| `internal/server/cd.go` | chuyển luồng HEVC của `iosview-cd` sang cùng khung 12 byte như H.264 (gói đầu flags bit3 = cấu hình hvcC) |
| `sidecar/` | `iosview_cd.py` (chương trình phụ), `build-sidecar.ps1` (PyInstaller → `iosview-cd\iosview-cd.exe`), `README.md` |
| `internal/device/` | `manager.go`: danh sách máy, gộp USB+Wi‑Fi theo UDID (ưu tiên USB), số máy/tên/nhãn, fan‑out input cho nhóm đồng bộ. `controller.go`: hàng đợi input, gộp move. `typing.go`: gõ văn bản |
| `internal/rfb/client.go` | client VNC chỉ để **điều khiển** (không giải mã hình) |
| `internal/server/server.go` | HTTP + WebSocket: `/ws/control`, `/ws/h264/`, `/ws/vnc/`, `/api/ui`, `/api/shot` |
| `internal/usbmux/` | client usbmuxd (Apple Mobile Device Service) |
| `web/src/main.ts` | toàn bộ UI (lưới máy, màn lớn, bảng điều khiển, phím tắt, cài đặt) |
| `web/src/h264.ts` | `H264View`: nhận frame, giải mã WebCodecs, vẽ canvas theo rAF |
| `web/dist/` | UI đã build, nhúng vào exe (`web/embed.go`) — **phải commit cùng src** |
| `iphone/trollvnc-h264/` | mã thêm vào TrollVNC: `H264Streamer.mm/.h`, `trollvnc-h264.patch` (từ commit gốc trong `BASE_COMMIT`), `update-1003-to-1004.patch` |
| `iphone/repo/` | kho Sileo tĩnh (Packages, Release, debs/) đẩy lên Cloudflare Pages |
| `iphone/deploy-repo.bat` | đẩy `iphone/repo` lên https://iosview.pages.dev/ (Cloudflare Pages, project `iosview`) bằng wrangler; trước đó chép exe có số phiên bản cao nhất vào `repo/pc/iosview.exe` + tạo `repo/pc/latest.json` và `repo/_headers` (no-cache) |
| `internal/update/` | tự cập nhật nội bộ: so hash exe đang chạy với `https://iosview.pages.dev/pc/latest.json` (lúc mở, mỗi 30 phút, hoặc thủ công); nút "Cập nhật" → tải `.new`, kiểm tra sha256, đổi tên exe → `.old`, `.new` → exe, mở lại với `-after-update` (chờ cổng UI), xoá `.old` lần chạy sau. API `GET/POST /api/update`. Biến môi trường `IOSVIEW_UPDATE_URL` để thử với máy chủ khác. Bản `dev` không cập nhật |
| `iosview.json` | cấu hình runtime cạnh exe (không commit). Khoá `ui` lưu dữ liệu UI (phím tắt, cụm từ, ghi chú, `video`) |

### Giao thức luồng H.264 (cổng VNC + 10)

- Phone → PC, mỗi frame: `u32 len · u16 w · u16 h · u8 flags (bit0 keyframe, bit1 = gói UDID) · u8 rotation · u16 0 · Annex‑B`.
- PC → phone: `'K'` xin keyframe · `'B'+u32 kbps` bitrate · `'I'` hỏi UDID · **`'P' + (fps|0x80) + (pct|0x80)`** đổi fps (5–60) và độ phân giải encode (25–100 %), có từ TrollVNC **3.3‑1004**. Bit cao để bản cũ (bỏ qua byte lạ) không hiểu nhầm thành K/B/I. Hết client thì phone tự về mặc định.
- UI: lưới mặc định 30 fps · 50 %, màn lớn 60 fps · 100 % (Cài đặt → Hình H.264); ghi đè từng máy qua chuột phải → "Hình riêng cho máy này…" (lưu `ui.video.dev[deviceId]`). Thanh "Nét màn nhỏ/lớn" = bitrate.

### Máy CoreDevice (iOS 17.4+ không jailbreak)

- Chọn tự động: máy cắm cáp, cổng TrollVNC đóng, iOS ≥ 17.4 (đọc `ProductVersion` qua lockdownd) → `kind: "cd"`. Máy có TrollVNC giữ nguyên đường cũ. Chỉ qua USB.
- Hình HEVC (WebCodecs + `description` hvcC), không có VNC/JPEG, không chỉnh bitrate/fps. Chạm: toạ độ 0..65535. Dán: clipboard + ⌘V (giữ dấu). Test: `go test ./internal/...` có `cd_test.go` (sidecar giả `internal/device/testdata/fake_cd.py`, cần python3).
- **Chưa thử trên iPhone thật.**

## 3. Quy tắc bắt buộc

1. **Đơn giản trước hết.** Không thêm kiến trúc/thư viện mới khi không cần. Bản 0.9.x xem rất mượt; mọi sửa đổi không được làm phức tạp thêm hoặc làm chậm đường hình/đường input.
2. **Ưu tiên độ trễ thấp**: không thêm buffer frame, không nội suy chuyển động.
3. **Build exe luôn nén UPX** và **tên file luôn có số phiên bản**: `iosview-<VERSION>.exe` (ví dụ `iosview-0.10.12.exe`). Tăng `VERSION` trong cả `build.sh` và `build.ps1`.
4. UI tối giản: không thêm chữ/thông báo thừa (ví dụ không hiện nhãn khi bấm Home/Back/Đa nhiệm).
5. Mọi iPhone đều có H.264 → **không** làm fallback JPEG tự động.
6. Dán văn bản: không cần giữ dấu tiếng Việt (gõ ASCII là đủ). Tính năng Ctrl+C (copy từ iPhone) đang **tạm gác**.
7. Cùng một iPhone cắm USB và có Wi‑Fi phải là **một** máy, ưu tiên USB.
8. Với thay đổi lớn hoặc nhiều hướng: **đề xuất trước, chờ chủ dự án duyệt rồi mới sửa code.**
9. Chỉ commit/push khi được yêu cầu.
10. Giữ hình học giao diện ổn định khi thao tác. Cấm đổi kích thước hoặc vị trí hộp thoại, hàng và nút do chọn máy, nội dung dài, ghi phím, tải dữ liệu hay mở danh sách. Dành sẵn vị trí, rút gọn bằng dấu … và cuộn bên trong vùng nội dung. Kiểm tra kích thước trước/sau trên trình duyệt.

## 4. Build & kiểm tra

- Linux/macOS (cross‑compile): `./build.sh` — cần Go ≥1.22, Node, `upx`. Windows: `.\build.ps1`.
- Bản **Wails** (0.11.0 PoC, tag `wails,production`, cần Go ≥ 1.26 — `go` tự tải toolchain): `.\build.ps1 -Wails` hoặc `WAILS=1 ./build.sh` → `iosview-0.11.0.exe`. Bản thường vẫn build như cũ.
- Test: `go test ./internal/...` · Type‑check UI: `cd web && npx tsc --noEmit -p .`
- UI build riêng: `cd web && node build.mjs` (ra `web/dist/app.js`).
- Mã iPhone không build được ở đây (cần Theos/iOS SDK). Build .deb bằng **GitHub Actions** trên bản fork `OwnGoalStudio/TrollVNC` của chủ dự án (workflow "Build TrollVNC", artifact `packages-roothide` / `-rootless` / `-default`). Kiểm tra cú pháp nhanh: clang `-target arm64-apple-ios14.0 -fsyntax-only` với SDK của `theos/sdks` + header IOKit của `theos/headers`.

## 5. Trạng thái hiện tại (31/10/2026)

- **0.11.0** (31/10, chưa commit) — **bản phát hành cho nhân viên**: cùng mã nguồn với 0.11.0-poc3, chỉ đổi số phiên bản (Minh duyệt đẩy lên máy chủ cập nhật Cloudflare). `build.ps1 -Wails` / `WAILS=1 ./build.sh` giờ ra `iosview-0.11.0.exe`. `iphone/deploy-repo.bat` chỉ chọn exe tên đúng dạng `iosview-x.y.z.exe` (bỏ qua `-poc*`, `-noupx`; trước đây ép `[version]` sẽ lỗi với các tên đó). Lưu ý khi nhân viên lên 0.11.0: trang chạy ở origin `wails.localhost` thay vì `127.0.0.1:7788` nên **localStorage cũ không theo sang** (cỡ ô, máy chính, công tắc FPS/Ctrl+S… về mặc định); phím tắt tự đặt, cụm từ, ghi chú vẫn giữ (nằm trong `iosview.json`). Bản thiết kế lại giao diện (Vite + Tailwind, giai đoạn 2–3 của kế hoạch) sẽ là 0.12.x.
- **0.11.0-poc3** (31/10, chưa commit): góp ý của Minh sau poc2. (1) **Zoom màn lớn phải mượt**: khôi phục chuyển động ghost (`stageMotion`, `MOTION_MS` = 240 ms, `.ghost`) và hoãn lệnh fps/% tới hết chuyển động; màn lớn vẫn hiện và nhận chạm ngay từ khung đầu (luật: mượt nhưng không giữ thao tác lại). (2) Khôi phục **nhấp phải ra ngoài màn lớn = thu nhỏ** (bị bỏ ở 0.10.21): `outsideBig()` — chỗ trống lưới, nền cửa sổ, quanh màn lớn (không phải `.host`/nút); chạy lúc nhấn chuột phải, chặn menu chuột phải ở đó. Nhấp phải trên màn lớn vẫn là Quay lại. Bảng phím tắt trong app thêm dòng "Mở màn hình lớn". (3) **Ctrl+S = "Nhấp màn nhỏ = phóng to"** (`prefs.zoomTap`, localStorage `iosview.zoomtap`, công tắc trong khối Hiển thị, `.app.zoom-tap` con trỏ zoom-in, thêm vào `RESERVED`): bật → nhấp trái bất kỳ đâu trên màn nhỏ (kể cả chưa có hình) mở máy đó ở màn lớn ngay khi nhấn, không gửi chạm; chuột phải đóng màn lớn; lăn chuột cuộn lưới; Ctrl/Shift+nhấp vẫn chọn máy. Tắt (mặc định) → nhấp/vuốt điều khiển như cũ. (4) **F5 tải lại** chạy lại trong bản Wails (Wails tắt phím của trình duyệt nên `main.ts` tự `location.reload()`). (5) **Icon cửa sổ = icon exe**: Wails tìm icon ở resource ID 3, `.syso` của mình để ID 1 → taskbar trước đây là PNG 256 thu nhỏ, icon nhỏ là icon mặc định Windows. `setWindowIcons()` trong `window_wails_windows.go` nạp khung `.ico` (ID 1) theo DPI cửa sổ (`SM_CXICON`/`SM_CXSMICON` qua `GetSystemMetricsForDpi`) và `WM_SETICON` lúc cửa sổ hiện + khi đổi DPI (`WndProcInterceptor`, `WM_DPICHANGED`). Logo thanh tiêu đề dùng **đúng khung .ico** taskbar dùng ở cùng tỉ lệ: `web/src/tb-{20,40,48,64}.png` (trích từ `assets/iosview.ico`, `srcset` 1x/1.25x/1.5x/2x) — 100 % là hình 1 máy, ≥ 125 % là hình 3 máy, như taskbar. Đã thử Playwright với máy giả: các ý trên đều đạt, không lỗi JS.
- **0.11.0-poc2** (31/10, chưa commit): Minh chạy PoC thấy **chạy bình thường**, duyệt làm tiếp; sửa theo góp ý. (1) Thanh tiêu đề: tên + phiên bản chung đường chân chữ (`.tb-title`, `align-items: baseline`); khối thương hiệu `.brand` trong bảng phải bị ẩn khi chạy Wails (trùng thanh tiêu đề), nút thu gọn bảng chuyển lên thanh tiêu đề (`.tb-panel`), thu gọn = ẩn hẳn bảng. (2) Bỏ **Home / Đa nhiệm / Quay lại** khỏi khối Điều khiển của bảng (mỗi máy đã có thanh nút riêng + màn lớn có `#stageNav`). (3) **Bỏ nhấp đúp vào hình để mở màn lớn**: lần nhấp đầu luôn là cú chạm thật nên hay mở nhầm app; nhấp vào hình giờ **luôn là chạm**, không chờ, không nuốt. Mở màn lớn: nút ⤢, nhấp đúp thanh tên, nhấp đúp số trong lưới số, Ctrl+←/→. (4) **Mọi thao tác phản hồi tức thì**: (poc2 bỏ chuyển động ghost của màn lớn — poc3 khôi phục theo yêu cầu, xem trên); bỏ các hẹn giờ 230 ms phân biệt nhấp/nhấp đúp ở thanh tên ô máy và chip nhãn (nhấp chọn/lọc ngay, nhấp đúp = hai lần đảo nên giữ nguyên rồi mới mở/đổi tên); nút thanh điều hướng, ⤢, ô "Đang ở màn lớn", thanh công cụ màn lớn và nút bảng điều khiển chạy khi **nhấn xuống** (`onPress()` trong `main.ts`, Enter/Space vẫn dùng được); trạng thái nhấn không chuyển dần, hover 80 ms. (5) Bỏ theme tối: xoá nút `#themeBtn` + script đọc `iosview.theme` (token tối trong CSS còn đó, dọn ở giai đoạn 3). Đã thử bằng Playwright với máy giả: lệnh Home/Back/Đa nhiệm/Khoá đi lúc pointerdown, đúng một lần; nhấp đúp vào hình không mở màn lớn; ⤢ mở màn lớn ngay khi nhấn, không animation. README/SHORTCUTS/ghi chú trong `index.html` đã sửa theo.
- **0.11.0-poc** (31/10, chưa commit, **chưa chạy thử trên máy thật**): PoC giai đoạn 0 của kế hoạch chuyển vỏ cửa sổ sang **Wails v3.0.0** (bản ổn định, phát hành 2026; cần Go ≥ 1.26). Kế hoạch đầy đủ + các quyết định đã chốt (vanilla TS + Vite + Tailwind, không React/shadcn; frameless + title bar tự vẽ; Mica Win11, Win10 nền đặc; bỏ theme tối; Win10 tối thiểu; giữ UPX) nằm trong Project doc `claude/plan-wails-v3.md` (Claude). Cách làm: build tag `wails` để bản cũ không đổi — `cmd/iosview/window_wails_windows.go` (`application.New` + `app.Window.NewWithOptions`), `window_windows.go` thành `windows && !wails`, `winOpts` trong `window.go`, `main.go` thêm cờ `-mica` (mặc định bật). Trang UI do **chính `srv.Handler()`** phục vụ qua asset server của Wails từ origin `http://wails.localhost` (fetch `/api/...` cùng origin, `/wails/runtime.js` do Wails chèn); **WebSocket không đi qua asset server**: cửa sổ mở `/?ws=127.0.0.1:7788[&mica=1]`, `main.ts` nối `ws://WS_HOST/...` thẳng cổng TCP; `internal/server` thêm `OriginPatterns: wails.localhost` (origin khác vẫn bị 403). Cửa sổ thứ hai (agent đang chạy) dùng reverse proxy tới cổng. UI: khi `WAILS` (hostname `wails.localhost`) `main.ts` chèn `#titlebar` 40 px (kéo bằng `--wails-draggable`, nút thu/phóng/đóng gọi `window.wails.Window.*`, nhấp đúp = phóng to), `html.wails .app` cao `calc(100% - 40px)`, `html.mica body` nền 72 % để Mica lộ ở nền (ô máy/panel vẫn đục). Mica chỉ Win11 22H2+ (`w32.IsWindowsVersionAtLeast(10,0,22621)`), còn lại `BackgroundTypeSolid` màu `--canvas`. `go.mod`: `go 1.26.0`, thêm `github.com/wailsapp/wails/v3 v3.0.0` (kéo thêm `adrg/xdg`, `go-ole`); `third_party/sys` nâng lên x/sys **v0.48.0** (chỉ `windows/`, bỏ test). Kích thước exe: 13.3 MB → **4.2 MB** sau UPX (bản WebView2: 8.6 → 2.8 MB); `upx -t` qua. Tiêu chí PoC cần đo trên máy thật: fps / `trễ Nms` / `đợi N` với 8–10 máy không tệ hơn 0.10.21; kéo cửa sổ, kéo mép đổi cỡ, Mica, thu/phóng/đóng; dán, phím tắt, chụp màn hình; tự cập nhật không mời hạ cấp (`0.11.0-poc` > kho `0.10.x`); so `-mica=false`. Qua thì sang giai đoạn 1–2 (khung app + Vite/Tailwind), không qua thì xoá tag `wails` là về như cũ.
- **0.10.21** (31/10, chưa commit): **gộp logic màn lớn** theo đề xuất đã duyệt. Bỏ chế độ "Nhấp màn nhỏ" (`prefs.gridCtl`, Ctrl+S, `.grid-view-only`): màn nhỏ **luôn điều khiển**. Mở màn lớn = **nhấp đúp vào hình** (2 lần nhấp < 320 ms, lệch < 8 px; lần 2 bị nuốt, không gửi chạm/nhả — `lastDown`/`swallowUp` trong `bindInput`), ⤢, nhấp đúp thanh tên, Ctrl+←/→. Bỏ công tắc "Màn lớn" (`pin`). Bỏ "nhấp phải chỗ trống = đóng". **Gộp "Phóng to toàn cửa sổ" vào màn lớn**: `maxBig` + `.app.max` (ẩn `.grid`/`.ctrl`, stage căn giữa, cao 100 %, giới hạn theo bề rộng), `toggleMax()` từ nút ⛶ thanh công cụ / menu chuột phải; `fullTile` giờ = máy trên màn lớn khi đang toàn cửa sổ (giữ cho logic chất lượng/pause/phím). Xoá `#focus` (DOM, CSS, handler). Khối Hiển thị chỉ còn FPS + 4 thanh trượt.
- **0.10.20** (31/10, chưa commit): con trỏ trên màn hình máy = **mũi tên thường** (`cursor: default`), chế độ Phóng to = bàn tay. Chuyển động mở/đóng màn lớn làm lại kiểu **ghost**: một khối `.ghost` (chỉ transform/opacity, position fixed) bay giữa ô máy và vị trí màn lớn trong 260 ms, màn lớn thật chỉ **fade** vào 30 % cuối (mở) / ra 30 % đầu (đóng) — không scale canvas video + bóng đổ từng frame nữa. Khi mở, lệnh đổi fps/% (và keyframe kèm theo) cho máy chính được **hoãn tới hết chuyển động** (`setTimeout(MOTION_MS)`) để cú giật keyframe không rơi vào giữa animation.
- **0.10.19** (31/10, chưa commit): công tắc "Chạm lưới" khó hiểu → bộ chọn **"Nhấp màn nhỏ: Điều khiển | Phóng to"** đầu khối Hiển thị (`.seg`, vẫn là `prefs.gridCtl`, Ctrl+S đảo); ở chế độ Phóng to nhấp bất kỳ đâu trên ô máy đều mở màn lớn (kể cả khi chưa có hình). Con trỏ trên màn hình máy đổi từ crosshair sang **vòng điểm chạm** (SVG data-URI, hotspot giữa, `--cur-touch`); chế độ Phóng to giữ `zoom-in`.
- **0.10.18** (31/10, chưa commit): **skin v3** theo review với skill UI/UX Pro Max. Font **Inter** nhúng (`web/src/inter.woff2`, 66 KB, cắt Latin + tiếng Việt bằng `pyftsubset`), thang chữ qua token `--fs-xs/sm/md/lg/xl`; màu máy chính **về xanh** (`--master` = `--green`), không còn tím; **bỏ cột "Chức năng máy"** → thanh công cụ `#stageTools` dưới màn lớn (`STAGE_FNS` trong `main.ts`), thông tin máy là tooltip của chú thích; bảng phải (292 px) chia **5 khối** `.blk` có nhãn (`renderButtons()` dựng khối Nhóm/Điều khiển, công tắc Hiển thị dạng hàng `.rows`); **màn lớn nổi** trên lưới (`.stage` position absolute, `right` = bề rộng bảng + 24, lưới không xê dịch) với chuyển động phóng từ ô máy (`stageMotion()`, Web Animations, 240 ms, tôn trọng reduced-motion); hover ô máy chỉ đổi viền, không dịch chuyển; nhấn nút không xê dịch. **Theme: mặc định luôn sáng** (bỏ `prefers-color-scheme`), tối = xám đậm không đen. Icon app v3: squircle phủ kín, bản vẽ riêng cho 16–32 px (`assets/icon.svg` là bản lớn). Header 52 px, nút theme/thu gọn bên phải.
- **0.10.17** (31/10, chưa commit): **skin v2** — chỉ CSS + icon, không Wails/Tailwind (đã phân tích: Wails trên Windows cũng là WebView2, không đẹp hơn; Tailwind chỉ thêm bước build). `web/src/style.css` viết lại token + rule: nền có ánh xanh, panel kiểu kính (viền mảnh, vạch sáng, bóng sâu), tab segmented, nút nổi mềm có glow, ô máy như viền iPhone bo 16 px phát sáng khi chọn, slider có phần đã kéo tô màu (`--p` do `paintRanges()` trong `main.ts` đặt), bộ lọc pill, số máy dạng chip. Icon UI đổi sang **Lucide** (ISC; bảng `IC` trong `main.ts`, lấy từ gói `lucide-static`). **Icon app/taskbar mới** (`assets/icon.svg` → `iosview.ico` 16–256 → `rsrc` → `cmd/iosview/rsrc_windows_amd64.syso`; `web/src/icon.svg|png` dùng cùng ảnh). Hiệu suất: ô FPS thêm **`trễ Nms`** (nhận frame → vẽ xong, trung bình trượt) và `đợi N` (hàng đợi decoder) trong `h264.ts`; `window_windows.go` đặt `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--enable-gpu-rasterization --enable-zero-copy` nếu chưa có (đặt biến này — kể cả thành một dấu cách — để tắt). Còn chờ số đo: hạ `MAX_BACKLOG` 10 → 5 và tạm dừng giải mã ô cuộn khuất (chỉ khi > 12 máy).
- **0.10.16** (31/10, chưa commit): sửa tự cập nhật mời **hạ cấp** (bản 0.10.15 chạy trên PC bị mời "Cập nhật 0.10.13" vì kho chỉ so hash): `internal/update` chỉ báo có bản mới khi phiên bản trên kho ≥ bản đang chạy (`cmpVersion`), kèm test.
- **0.10.15** (31/10, chưa commit): dán nhiều dòng chia máy **chỉ cần ≥ 2 dòng và ≥ 2 máy đang chọn** (bỏ điều kiện Đồng bộ bật / máy trỏ phải trong nhóm — `splitPaste` trong `web/src/main.ts`); nhịp gõ phím 35/25 → **25/20 ms** (`internal/device/typing.go`, cần xác nhận không rớt ký tự trên 6s/7); README/SHORTCUTS ghi mẹo `Tab` trong dòng để điền nhiều ô.

### Trước đó (24/10/2026)

- **0.10.14** (24/10, chưa commit): driver CoreDevice cho iPhone iOS 17.4+ không jailbreak (`internal/device/cd.go`, `session.go`, `internal/server/cd.go`, `internal/usbmux` thêm `ProductVersion`, `web/src/h264.ts` nhận HEVC, `sidecar/`). Test Go + test UI đầu‑cuối với máy giả qua. Cần: build `iosview-cd` trên Windows (`sidecar\build-sidecar.ps1`) và thử với iPhone thật.

### Trước đó (20/10/2026)

- Commit mới nhất trên repo: **0.10.8**. Trên máy có thay đổi **chưa commit** cho **0.10.9 → 0.10.12** (đã build `iosview-0.10.11.exe`, test Go qua, test đầu‑cuối với iPhone giả qua):
  - 0.10.9–0.10.10: dán nhiều dòng chia cho từng máy trong nhóm (dòng 1 → máy số nhỏ nhất…), tạm dừng vẽ các ô bị che, chống xin keyframe dồn dập.
  - 0.10.12: tự cập nhật nội bộ cho nhân viên (xem `internal/update/`, README mục "Cập nhật cho nhân viên"); test Go + test đầu‑cuối thay exe và mở lại đã qua.
  - 0.10.11: fps + % độ phân giải tuỳ chỉnh từ PC (lệnh `P`), ghi đè từng máy, ngưỡng backlog decoder về 10 (như 0.9.x), vẽ frame theo `requestAnimationFrame`, sửa lỗi UI khi chưa có máy (`devices: null`).
  - TrollVNC **3.3‑1004 đã build**: `iphone/TrollVNC-iosview_3.3-1004_roothide.deb` và `_rootless.deb` (gói 1003 thay đúng file `trollvncserver` mới + số phiên bản; các file khác giữ nguyên). **Chưa test trên máy thật.** Bản rootful (iOS 14) chưa build được do toolchain Linux thiếu `___isOSVersionAtLeast` (compiler-rt) → kho vẫn để rootful 1003.
- Kho Sileo `iphone/repo/` đã cập nhật: roothide/rootless 1004 + rootful 1003, **chưa đẩy** lên Cloudflare.

## 6. Việc cần làm (theo thứ tự)

1. **Rà lại và commit** các thay đổi 0.10.9–0.10.11 (một commit, kèm `web/dist/app.js`; không commit exe/.deb lớn ngoài `iphone/` theo `.gitignore`). Hỏi chủ dự án trước khi push.
2. ~~Build TrollVNC 3.3‑1004~~ (đã xong, xem mục 5). Cách build lần sau trên Linux: Theos `roothide/theos` + toolchain `L1ghtmann/llvm-project` (iOSToolchain-x86_64) + `iPhoneOS16.5.sdk` (theos/sdks), cần `rsync`, `fakeroot`, shim `/usr/libexec/PlistBuddy`; `make FINALPACKAGE=1` với `THEOS_PACKAGE_SCHEME=roothide|rootless` cho ra `.theos/obj/trollvncserver` (bundle Cài đặt lỗi link nhưng không đổi nên tái dùng từ gói cũ). Hoặc dùng GitHub Actions: trong fork TrollVNC (đang ở trạng thái 1003) chép `H264Streamer.mm/.h` vào `src/`, áp `update-1003-to-1004.patch`, đổi `PACKAGE_VERSION := 3.3-1004` trong `Makefile`, chạy workflow, lấy 3 file .deb, đặt tên `TrollVNC-iosview_3.3-1004_<roothide|rootless|rootful>.deb` vào `iphone/`. (Chi tiết: `iphone/README.md`.)
3. **Kho Sileo** `iphone/repo/` (đã cập nhật 1004; lần sau làm lại như sau): chép .deb mới vào `debs/`, tạo lại chỉ mục trên Linux:
   ```sh
   cd iphone/repo
   dpkg-scanpackages -m debs /dev/null > Packages && bzip2 -kf Packages && gzip -kf9 Packages
   # Release: giữ các dòng đầu (Origin, Label, Suite, Version, Codename,
   # Architectures: iphoneos-arm iphoneos-arm64 iphoneos-arm64e, Components, Description)
   # rồi ghi lại MD5Sum/SHA256 (+ kích thước) của Packages, Packages.bz2, Packages.gz.
   ```
   Đẩy lên: chạy `iphone\deploy-repo.bat` trên Windows (cần Node; lần đầu `wrangler login`), hoặc `npx wrangler pages deploy iphone/repo --project-name iosview --branch main`. Địa chỉ kho: **https://iosview.pages.dev/**.
4. **Thử trên iPhone thật** (6s/7) với 3.3‑1004 + iOSView 0.10.11, bật Cài đặt/nút "FPS":
   - ô thống kê hiện `H.264 30fps 50%` ở lưới, `60fps 100%` khi mở màn lớn; fps đo được khớp;
   - mở/đóng màn lớn đổi độ phân giải mượt (chỉ chớp ngắn, có keyframe mới), không treo hình;
   - máy bớt nóng, bớt lag so với 1003; thử fps lẻ (45/50) — nhịp sẽ hơi không đều do màn 60 Hz, đó là bình thường;
   - máy còn 1003 vẫn xem được bình thường (bỏ qua lệnh `P`).
   Báo lại số liệu rồi mới chỉnh mặc định.
5. Sau khi ổn định: cập nhật `README.md` / `docs/` nếu mặc định thay đổi; ghi chú phiên bản.

## 7. Lưu ý / hạn chế đã biết

- Gói TrollVNC dùng chung mã `com.82flex.trollvnc` với bản chính thức trên kho Havoc → máy dùng iOSView không nên thêm kho Havoc (Sileo có thể "nâng cấp" sang bản không có H.264).
- Ghi đè "Hình riêng cho máy này" lưu theo device id; máy Wi‑Fi sau khi gộp với USB đổi id (`wifi:…` → `usb:<udid>`) nên phải đặt lại.
- Chụp màn hình lấy từ canvas → ở lưới sẽ theo độ phân giải đang dùng (ví dụ 50 %).
- Cấu hình của chủ dự án đang để bitrate 9/9 (4500 kbps mỗi máy); nên khuyên hạ lưới về 5–6.
- Môi trường cloud của agent có thể bị chặn `api.cloudflare.com` → việc đẩy kho chạy trên Windows của chủ dự án.

# iOSView — Bộ phím tắt

Tham khảo bộ phím tắt của công cụ điều khiển nhóm (ảnh chụp ngày 19/10/2026) và áp dụng vào iOSView từ bản **0.10.3**.
Trong app: tab **Cài đặt → Phím tắt…** để xem và đổi phím.

Ký hiệu cột **iOSView**: ✅ đã làm · ⚠️ làm khác bản gốc (xem ghi chú) · ❌ chưa hỗ trợ.

## Chung

| Chức năng | Phím tắt | iOSView | Ghi chú |
|---|---|---|---|
| Chọn tất cả điện thoại | `Ctrl + A` | ✅ | Đưa mọi máy vào nhóm đồng bộ. *Trước đây Ctrl+A là “chọn hết chữ trên iPhone” — giờ dùng nút **Chọn hết chữ** ở bảng chức năng máy.* |
| Chọn liên tục điện thoại | `Shift + nhấp trái` | ✅ | Nhấp một máy, rồi Shift+nhấp máy khác: chọn mọi máy ở giữa (theo thứ tự trên lưới). |
| Hủy chọn điện thoại đã chọn | Nhấp trái vào chỗ trống | ✅ | Chỗ trống trong lưới máy. |
| Điều chỉnh kích thước màn hình giám sát nhỏ | `Ctrl + cuộn chuột` | ✅ | Trên màn lớn thì đổi cỡ màn lớn. `Ctrl + + / − / 0` cũng được. |
| Điều chỉnh kích thước và chất lượng hình | Rê chuột lên thanh trượt + cuộn chuột | ✅ | 4 thanh trượt: cỡ màn lớn, cỡ màn nhỏ, nét màn nhỏ, nét màn lớn. Dừng chuột trên thanh trượt khoảng 0,3 giây rồi mới lăn; lăn khi đang cuộn bảng thì vẫn là cuộn bảng (tránh đổi nhầm độ nét). |
| Điều khiển riêng lẻ từng điện thoại | Giữ `Alt` | ✅ | Giữ Alt khi chạm / gõ / bấm nút: chỉ máy đang trỏ nhận lệnh, cả khi đang đồng bộ. |
| Điều khiển bằng hai ngón tay | Giữ `Shift` | ❌ | TrollVNC (giao thức VNC) chỉ có **một** điểm chạm, chưa giả lập được hai ngón. |
| Chuyển đổi nhanh màn hình lớn | `Ctrl + phím mũi tên` | ✅ | ←/↑ máy trước, →/↓ máy sau. Màn lớn đang đóng thì mở màn lớn cho máy đang trỏ. |
| Mở màn hình lớn | Nút ⤢ trên ô máy, hoặc nhấp đúp thanh tên | ✅ | Hoặc bật `Ctrl + S` rồi nhấp màn nhỏ. Nhấp đúp vào **hình** không mở màn lớn (lần nhấp đầu là chạm thật, hay mở nhầm app). |
| Đóng màn hình lớn | Nhấp phải ra ngoài màn lớn | ✅ | Chỗ trống trong lưới hoặc quanh màn lớn (nhấp phải **trên** màn lớn vẫn là Quay lại). Nút **✕** và ô "Đang ở màn lớn" vẫn dùng được. |

### Phím tắt đổi được (Cài đặt → Phím tắt…)

| Chức năng | Mặc định | iOSView | Ghi chú |
|---|---|---|---|
| Dán cưỡng bức | `Ctrl + D` | ✅ | Luôn **gõ từng phím** nội dung clipboard PC (bỏ dấu), kể cả khi cài đặt đang để “nạp vào clipboard máy”. Dùng cho ô không nhận Paste. |
| Dán văn bản | `Ctrl + V` | ✅ | Theo cài đặt “Bỏ dấu tiếng Việt khi dán”. **Nhiều dòng + chọn ≥ 2 máy** → mỗi máy một dòng theo số máy (song song, không Enter). Trong dòng có thể dùng **Tab** để nhảy ô: `tendangnhap<Tab>matkhau`. |
| Cụm từ nhanh | `Ctrl + K` | ✅ | Danh sách câu hay dùng; bấm số 1–9 hoặc nhấp để gửi vào máy đang trỏ (cả nhóm khi đồng bộ). |
| Mở cài đặt | `Ctrl + ,` | ✅ | |
| Mở ghi chú | `Ctrl + R` | ✅ | Ô ghi chú nổi, tự lưu. *Ctrl+R không còn tải lại cửa sổ (dùng F5).* |
| Đóng ghi chú | `Ctrl + Q` | ✅ | |

Phím tắt tự đặt phải có `Ctrl` hoặc `Alt` (hoặc là phím F), không được trùng phím cố định. Nút **Khôi phục mặc định** trả lại bảng trên.
Phím đã đổi, cụm từ nhanh và ghi chú được lưu trong `iosview.json` (khoá `ui`).

## Chế độ điều khiển màn hình lớn

| Chức năng | Phím tắt | iOSView | Ghi chú |
|---|---|---|---|
| Mở nhiệm vụ hiện tại (đa nhiệm) | Nhấn và giữ chuột phải | ✅ | Giữ ≥ 0,45 giây. |
| Quay về trang chủ | Nhấp chuột giữa | ✅ | *Trước đây chuột giữa = Khoá màn hình — Khoá giờ chỉ ở nút **Khoá**.* |
| Quay lại cấp trên | Nhấp chuột phải | ✅ | Vuốt từ mép trái. *Trước đây chuột phải = Home.* |
| Quay lại cấp trên | `Esc` | ✅ | *Trước đây Esc = đóng màn lớn.* |

## Chế độ điều khiển màn hình nhỏ

| Chức năng | Phím tắt | iOSView | Ghi chú |
|---|---|---|---|
| Nhấp màn nhỏ = phóng to | `Ctrl + S` | ✅ | Bật/tắt (công tắc cùng tên ở khối Hiển thị). **Bật:** nhấp màn nhỏ mở máy đó ở màn lớn, nhấp máy khác thì chuyển, chuột phải đóng, lăn chuột cuộn lưới. **Tắt:** nhấp / vuốt màn nhỏ điều khiển máy. |
| Chọn điện thoại | `Ctrl + nhấn (giữ) chuột trái` | ✅ | Bật/tắt chọn máy đó. Nhấp thanh tên máy vẫn chọn như cũ. |
| Chọn khung điện thoại | `Ctrl + kéo chuột trái` | ✅ | Kéo khung: chọn thêm mọi máy nằm trong khung. |
| Mở nhiệm vụ hiện tại | Nhấn và giữ chuột phải | ✅ | |
| Quay về trang chủ | Nhấp chuột giữa | ✅ | |
| Quay lại cấp trên | `Esc` | ✅ | Nhấp chuột phải trên màn nhỏ cũng = quay lại (giống màn lớn). |

## Phím riêng của iOSView (giữ nguyên)

| Chức năng | Phím tắt |
|---|---|
| Đổi tên máy đang trỏ | `F2` |
| Bật / tắt đồng bộ | `F4` |
| Xoá nội dung ô nhập trên iPhone | `Ctrl + Backspace` / `Ctrl + Delete` |
| Hoàn tác / làm lại trên iPhone | `Ctrl + Z` / `Ctrl + Y` |
| Cuộn trên iPhone | Lăn chuột trên màn hình máy |
| Gõ phím, Enter, Tab, mũi tên… | Gõ khi trỏ chuột vào máy |
| Tải lại cửa sổ | `F5` |

- **Ctrl+C** (app đang focus, ngoài ô nhập): copy clipboard các iPhone đang chọn, hoặc tất cả máy online nếu không chọn; chỉ lấy dữ liệu theo thứ tự máy trong lưới; công tắc Cài đặt → Dòng trống giữa dữ liệu copy chọn cách ngăn giữa hai dữ liệu. Mỗi máy có 800 ms để trả dữ liệu. Máy lỗi để trống dòng nếu có máy khác trả dữ liệu. Bản này hỗ trợ TrollVNC.

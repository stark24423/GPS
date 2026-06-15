# GPS Simulator Go 開發指南

本文件給後續開發 agent 或工程師使用。專案目前同時保留 Python 版與 Go 版，但新功能開發以 Go 版為主。

## 專案目標

- 主要目標：開發 Windows 可攜式 Go/Fyne 桌面工具，用來模擬 iPhone GPS 位置。
- Go 版需保留現有功能：
  - OpenStreetMap 地圖預覽。
  - 點選地圖加入座標。
  - 單點定位與路線模擬。
  - GPX 輸出。
  - 搖桿移動。
  - Dry-run 模式。
- iPhone 模式目標：
  - 自研 iOS 17.4+ USB tunnel 核心。
  - Windows 優先。
  - USB 優先。
  - 使用外部 `wintun.dll`，不自研 Windows kernel driver。

## 主要程式架構

### `cmd/gps-simulator`

Go 版桌面程式入口。

- `main.go` 應只負責啟動 `internal/ui.Run()`。
- 不要在 entrypoint 放業務邏輯。

### `internal/ui`

Fyne 桌面 UI。

- `app.go`：主視窗、控制面板、iPhone bridge 操作、log、start/stop 流程。
- `map_widget.go`：自製地圖 widget，直接抓 OpenStreetMap tile 並用 raster 繪製。
- `joystick.go`：搖桿 widget。

UI 層只做使用者互動與狀態顯示，不直接處理 iOS protocol 細節。

### `internal/core`

與 UI/iPhone 無關的核心邏輯。

- 座標模型。
- 路線插值。
- GPS jitter。
- GPX 生成。

這個 package 應維持可單元測試、無外部裝置依賴。

### `internal/iosbridge`

UI 與 iOS 核心之間的 facade。

- 對 UI 提供簡單 API：
  - `ListDevices`
  - `CheckRequirements`
  - `SetLocation`
  - `PlayRoute`
  - `Stop`
- 不應在 UI 直接引用 `internal/ioscore` 或 `internal/iostunnel`。
- 未來所有 iPhone 操作都應先經過這層。

### `internal/ioscore`

自研 iOS 基礎 protocol。

- `plist`：標準庫 XML plist encode/decode。
- `usbmux`：usbmux framing、device list、連接 lockdown。
- `lockdown`：lockdown plist framing、`GetValue`、`StartService`。

這一層限制：

- 盡量只使用 Go 標準庫。
- 不依賴 `go-ios`、`pymobiledevice3` 或其他 iOS protocol 套件。
- 可以用 `pymobiledevice3` 或 `go-ios` 的行為當測試參考，但不要直接複製 GPLv3 原始碼。

### `internal/iostunnel`

預定新增的自研 tunnel manager。

建議 API：

```go
type Manager struct { ... }

func (m *Manager) Start(ctx context.Context, udid string) (TunnelInfo, error)
func (m *Manager) EnsureRunning(ctx context.Context, udid string) (TunnelInfo, error)
func (m *Manager) Stop(udid string) error
func (m *Manager) Status() []TunnelInfo
```

`EnsureRunning` 必須是 idempotent：同一台裝置已經有 tunnel 時，不要重複建立。

### `internal/iostunnel/wintun`

預定新增的 Windows Wintun adapter 封裝。

- 透過動態載入 `wintun.dll`。
- 啟動時檢查 DLL 是否存在。
- 不要在 unit test 操作真實 adapter。
- 真實 adapter 操作需用 interface 抽象，測試時使用 fake。

### `internal/ioslocation`

預定新增的 iPhone 定位模擬層。

- tunnel 建立後，透過 trusted RSD/DVT 呼叫定位功能。
- 第一階段先支援：
  - `SetLocation`
  - `ClearLocation`
  - `PlayRoute`
- 不要把 DVT protocol 寫進 UI 層。

## iOS Tunnel 開發邊界

第一版只支援：

- Windows。
- USB。
- iOS 17.4+。
- lockdown `com.apple.internal.devicecompute.CoreDeviceProxy` TCP tunnel。

第一版不支援：

- iOS 17.0-17.3 QUIC/remoted tunnel。
- Wi-Fi tunnel。
- 多台 iPhone 同時 tunnel。
- 自研 Windows kernel driver。

遇到不支援情境時，要回傳明確錯誤，例如：

```text
iOS 17.4+ required for built-in Go tunnel
Wintun DLL was not found next to gps-simulator-go.exe
Administrator privileges are required to create the tunnel adapter
```

不要靜默 fallback 到不明狀態。

## 授權與參考規則

`pymobiledevice3` 是 GPLv3。開發時可以：

- 閱讀文件。
- 觀察 CLI 行為。
- 用作本機對照測試。
- 比較封包或錯誤訊息。

不要直接：

- 複製 `pymobiledevice3` 原始碼。
- 逐行翻譯 Python 實作到 Go。
- 把 GPLv3 程式碼混入本 repo 後再散佈 exe。

若未來要公開或商用散佈，需做 clean-room 或授權審查。

## 中文註解規範

後續新增或修改複雜程式碼時，請加入繁體中文註解，讓維護者能理解。

需要中文註解的地方：

- iOS protocol framing。
- endian / length prefix / packet header。
- tunnel parameter exchange。
- Wintun adapter lifecycle。
- goroutine cancellation 與資源清理。
- UI 與背景 iPhone 操作之間的狀態同步。
- 任何「看起來不直覺但必要」的 workaround。

註解原則：

- 用繁體中文。
- 說明「為什麼這樣做」，不要只描述程式碼表面行為。
- 避免過長，一段 1 到 3 行即可。
- 簡單 getter/setter、明顯欄位賦值不用註解。

範例：

```go
// lockdown 封包前 4 bytes 是 big-endian 長度，後面才是 plist。
// 這裡先完整讀完 plist，避免半包造成 XML parser 讀到不完整資料。
```

## 開發流程

建議每次改動遵守以下順序：

1. 先跑可聚焦的測試。
2. 小步修改。
3. 補或更新單元測試。
4. 跑 Go 測試。
5. 最後再手動測 GUI 或真機。

常用指令：

```powershell
go test .\internal\core .\internal\ioscore\...
go test .\internal\core .\internal\ioscore\... .\internal\iosbridge
go build -o .\dist\gps-simulator-go.exe .\cmd\gps-simulator
```

Fyne build 需要 Windows C compiler / MinGW：

```powershell
$env:CGO_ENABLED = "1"
go build -o .\dist\gps-simulator-go.exe .\cmd\gps-simulator
```

## 測試策略

### 單元測試

必須能在沒有 iPhone 的環境跑：

- `internal/core`
- `internal/ioscore/plist`
- `internal/ioscore/usbmux`
- `internal/ioscore/lockdown`
- tunnel manager 狀態機與 cleanup

### 硬體整合測試

需要 Windows + iPhone：

- iPhone 已 Trust this computer。
- Developer Mode 已開。
- iOS 17.4+。
- `wintun.dll` 放在 exe 同層或可載入路徑。
- 用系統管理員權限啟動。

整合測試場景：

- Refresh devices 能看到 USB iPhone。
- Start Tunnel 後取得 RSD address/port。
- Set single point 可改變 iPhone 位置。
- Play route 可連續更新位置。
- Stop 可清除位置並釋放 tunnel。
- 拔掉 iPhone 時 UI 顯示 tunnel lost，不 crash。

## UI 行為準則

- iPhone 操作都要放背景 goroutine，不能卡住 Fyne main thread。
- 所有 UI 更新需用 `fyne.Do`。
- 錯誤要同時寫 log 與顯示可理解訊息。
- Tunnel 狀態應顯示在 iPhone 卡片，不只寫在 log。
- Start/Stop 過程中要避免使用者重複點擊造成重入。

## Build / Portable 輸出

目標輸出：

```text
dist/
  gps-simulator-go.exe
  wintun.dll
  output/
```

`wintun.dll` 不應硬編進 exe。程式啟動或 Start Tunnel 時檢查它是否可載入，缺少時顯示明確提示。

## 目前重要限制

- 自研 tunnel 尚未完成前，不要宣稱 iPhone 模式已完整可用。
- 若某個 protocol 分支尚未實作，請回傳明確 `unsupported` 或 `not implemented` 錯誤。
- 不要重新引入 `go-ios/ios/simlocation` 作為正式 iPhone bridge；若短期用來對照測試，請隔離在測試或 debug helper，不要接進正式流程。

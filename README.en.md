# Canyou

> A lightweight Windows LAN-sharing utility in a single EXE. Scan the QR code to browse, preview, download, and upload files, or exchange messages in a shared chat room. Includes an optional access password, online devices, and light/dark themes.

[简体中文](./README.md) · **English**

📦 Repository: **<https://github.com/IsErWa/canyou>**

---

## 📸 Screenshots by Scenario

These screenshots show the actual `v2.0.0` application with isolated demo folders, passwords, devices, and messages. Desktop pages use a 1440 × 900 viewport; mobile screenshots use a browser-emulated 390 × 844 viewport, not a physical-phone test. The application UI is currently in Chinese.

### Desktop App: Configuration and Service Controls

| Configured, not running | Sharing service running |
| --- | --- |
| ![Stopped desktop app with folders, adapter, port, and access password](./docs/images/server-stopped.png) | ![Running desktop app with QR code, access URL, and Stop service button](./docs/images/server.png) |

### Desktop Browser: Files Alongside Chat

![Desktop workspace with files and the public chat room](./docs/images/chat-desktop.png)

### Mobile: Switch Between Files and Chat

| File browser | Public chat room |
| --- | --- |
| ![Mobile file list with metadata on a separate line](./docs/images/files-mobile.png) | ![Mobile chat with message bubbles and composer](./docs/images/chat-mobile.png) |

<details>
<summary>Access verification: first visit, incorrect password, and mobile entry</summary>

When an access password is configured, new devices verify it first. Previously authorized devices can pass automatically on later visits.

![Desktop password verification](./docs/images/gate.png)
![Incorrect-password feedback](./docs/images/gate-error.png)
![Mobile password verification](./docs/images/gate-mobile.png)

</details>

<details>
<summary>File operations: root folder, subfolders, batch downloads, and unread messages</summary>

Open folders and navigate with breadcrumbs or the back button. Select files / folders for a ZIP download. Messages keep arriving, with an unread count while browsing files.

![Desktop file browser](./docs/images/client.png)
![Subfolder and breadcrumb navigation](./docs/images/directory.png)
![Folders selected for a batch ZIP download](./docs/images/files-selection.png)
![Unread-message indicator in the file view](./docs/images/chat-unread.png)

</details>

<details>
<summary>Media previews: images, audio, and video</summary>

Click a media filename to open the preview dialog, switch between items, or close it. The download button at the end of each row still downloads directly. The image, WAV, and WebM below are working demo files.

![Image preview dialog](./docs/images/preview-image.png)
![Audio preview with playback controls](./docs/images/preview-audio.png)
![Video preview with playback controls](./docs/images/preview-video.png)

</details>

<details>
<summary>Uploads: selection, pagination, success feedback, and mobile layout</summary>

Select or drop multiple files and remove individual entries before uploading. Selections larger than five files are paginated. Results stay in the dialog, and files are saved to the configured upload directory.

![Desktop upload dialog with selected files and pagination](./docs/images/upload-desktop.png)
![Inline upload-success feedback](./docs/images/upload-result.png)
![Mobile upload dialog](./docs/images/upload-mobile.png)

</details>

<details>
<summary>Online devices: desktop and mobile</summary>

See connected devices, their numbers, names, and IP addresses. “我” marks the current device. This is a live online list, not an authorization-management page.

![Desktop online-device list](./docs/images/devices-desktop.png)
![Mobile online-device list](./docs/images/devices-mobile.png)

</details>

<details>
<summary>Settings and appearance: light/dark themes, device profile, and chat retention</summary>

Settings, chat, and online devices are peer panels. Appearance offers dark, light, and system modes; names, avatar colors, and the chat retention limit have separate save actions.

![Desktop settings in light mode](./docs/images/settings-desktop-light.png)
![Desktop settings in dark mode](./docs/images/settings-desktop-dark.png)
![Desktop file list in dark mode](./docs/images/files-desktop-dark.png)

| Mobile light settings | Mobile dark settings | Mobile dark file browser |
| --- | --- | --- |
| ![Mobile settings in light mode](./docs/images/settings-mobile-light.png) | ![Mobile settings in dark mode](./docs/images/settings-mobile-dark.png) | ![Mobile file list in dark mode](./docs/images/files-mobile-dark.png) |

</details>

## ✨ Features

- 📂 **Dual-directory setup**: configure a *view directory* (for browsing / downloading) and an *upload directory* (to receive web uploads) separately
- 🖼️ **Instant QR access**: pick a local IP and port, and an access QR code is generated automatically — just scan to open
- ⬇️ **Flexible downloads**: download any single file, or tick multiple files / folders for a batch download that is streamed as a live zip on the fly (no temporary file)
- 📤 **Inline uploads**: open the dialog from the top bar; drag-and-drop / multi-select, remove individual files, and browse selections in pages of five; responsive on mobile with inline results
- 🖼️ **Type-aware previews**: images, audio, and video open in a dialog; PDF, HTML, text, and code files open in a new tab; other types download. Actual preview support depends on the browser and media encoding
- 🌐 **Responsive single-page workspace**: navigate folders without reloading; wide screens keep files beside a tool panel, while narrow screens switch views; files are sorted by "folders first, then modified time"
- 💬 **Public chat room**: connects automatically on page entry, with retained history, unread counts, automatic reconnect, and manual retry; file browsing and panel switching do not intentionally disconnect chat
- 👥 **Online devices and profiles**: a live device list, stable device numbers, custom names, and eight preset avatar colors
- 🎨 **Three appearance modes**: dark / light / system, with preferences saved per device
- 🔐 **Optional access gate**: with a password configured, new devices are registered after verification; without one, the page registers them automatically without a password prompt. Authorized devices can pass automatically later. Password changes take effect without restarting, but do not revoke existing authorizations
- 🔗 **One-click actions**: copy the access URL, open in default browser (only enabled while the service is running and the address is valid)
- ▶️ **Service and network controls**: one button switches between “启动服务” (Start service) and “停止服务” (Stop service); refreshing or selecting an IP only updates the URL and QR code, without restarting sharing
- 💾 **Local persistence**: folders, port, IP, and access-password settings are saved; SQLite stores chat history, authorized devices, names, avatar colors, and themes
- 🛡️ **Robust port handling**: the port field accepts digits only and is limited to 1–65535; clear messages when a port is in use or reserved by the system
- 🪟 **No console window**: built as a GUI-subsystem executable — launches cleanly with no command-line flash

## 🖥️ Requirements

- **OS**: **Windows** only (relies on the Win32 GUI)
- **Network**: clients must be on the **same local network (LAN)**
- No runtime needed (pure Go, statically compiled, CGO disabled)

## 🚀 Usage

1. Download a build from [Releases](https://github.com/IsErWa/canyou/releases), or build it yourself. Run `Canyou-<version>.exe`; local build output is in `dist/bin/`.
2. Set “查看目录” (view directory) and optionally a separate “上传目录” (upload directory). Without an upload directory, the web upload entry is not shown.
3. Select a local IP reachable by your other devices and enter a port (default `8080`). Use “刷新” (Refresh) after network-adapter changes.
4. For access verification, enter “访问密码” (access password) and click “设置密码” (Set password). Clear the field and apply again to disable the gate. “复制” copies the field's current contents.
5. Click “启动服务” (Start service), then scan the QR code or open `http://<IP>:<port>` on a LAN device. If Windows Firewall prompts you, allow access only on trusted private networks as needed.
6. If an access password is configured, new devices enter it first; otherwise, the page registers the device automatically without a password prompt. Then browse files or open uploads, chat, online devices, and settings from the top bar. Closing a tool panel returns to files.

### Common Actions

| Scenario | Action |
| --- | --- |
| Browse and download | Open folders; click filenames to preview / open; use row-end buttons to download; select multiple items and use the toolbar download button for a ZIP |
| Upload files | Open “上传” (Upload), select or drop files, check the list, then click “上传文件”; the pending list clears after complete success |
| Send messages | On desktop, `Enter` sends and `Shift+Enter` inserts a newline; on mobile, Enter inserts a newline and the Send button submits |
| Update your profile | Edit the name in Settings and click “保存名称”; select an avatar swatch and click “保存颜色” |
| Change appearance | Choose dark, light, or system in Settings; the selection saves automatically |
| Chat retention | Default: 200 messages; range: 1–10000. This is global and editable by all connected devices; reducing it deletes the oldest messages |
| Connection loss | The page retries automatically up to five times; use “重新连接” (Reconnect) if it is still disconnected |

> Changing folders or a valid port while running restarts the service and briefly interrupts connections. An empty / invalid port stops it. Refreshing or selecting an IP does not restart it. Reloading the page does not restore files that have not yet been uploaded.

### Device Authorization Flow

The page first checks for an existing registration. If none exists and no password is required, it requests authorization automatically. When a password is configured, registration requires successful password verification. Both `check` and `unlock` use database records as the source of truth; a cached authorization never skips the database lookup.

| Endpoint (POST with a JSON body) | Responsibility |
| --- | --- |
| `/gate/check` | Looks up the device by `did`; if found, updates the authorization cache and returns device details. If absent, clears the stale cache entry and returns `ok:false`, `allowed:false`, and `requiresPassword`, without inserting a record |
| `/gate/unlock` | Accepts `did`, an optional device `name`, and the access `password`; the password can be omitted when none is configured. After verification, checks the database and inserts only if absent, then updates the cache and returns details only after confirming they can be read |
| `/gate/theme` | Saves the theme only for a registered device. If the device record is missing, returns an error requiring a page refresh and reauthorization; it does not recreate the record |

Successful `check` and `unlock` responses include `ok:true`, `allowed:true`, `id`, `did`, `name`, and `theme`. Repeated authorization preserves the existing device number, name, avatar color, theme, and authorization time. An incorrect password makes `unlock` return HTTP 403; database read/write failures return HTTP 500 rather than reporting successful authorization.

## 💾 Data and Backups

| Data | Default location |
| --- | --- |
| Desktop configuration | `%APPDATA%\Canyou\config.json` |
| Chat history, global retention, device authorizations and profiles | `%LOCALAPPDATA%\Programs\Canyou\chat.db` |
| Shared and uploaded files | The view and upload directories selected in the desktop app |
| Locally built executable | `dist/bin/Canyou-<version>.exe` |
| Markdown-to-HTML script | `scripts/md2html.sh` (run `bash scripts/md2html.sh`; output: `dist/docs/`) |

Chat and the access gate use the same SQLite database. If `LOCALAPPDATA` is unset, the database falls back to `AppData\Local\Programs\Canyou\chat.db` under the user's home directory. Exit the app before backing up the configuration, database, and any shared files. These runtime data are not included in the EXE.

The browser uses site-local storage for its device identity and UI state. Changing browsers or clearing site data may require verification again.

If a device record is deleted directly from `allowed_devices`, the next `check` that finds it missing clears the device's authorization cache. On page refresh, a configured password must be verified again; without a password, the page automatically calls `unlock` to register again. This creates a new record; the device number and profile are not guaranteed to match those of the deleted record.

> Deleting a record does not immediately disconnect the device: the app does not watch database deletions or forcibly close existing WebSockets. Chat renaming and avatar-color changes can still recreate a device record, so direct deletion is not a reliable real-time revocation or forced-sign-out mechanism.

## 🔨 Building from source

Get the source:

```bash
git clone https://github.com/IsErWa/canyou.git
cd canyou
```

Prerequisites:

- [Go](https://go.dev/dl/) 1.22 or later
- `goversioninfo` (embeds the icon and version info); install once:

  ```bash
  go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
  ```

Build in Windows CMD:

```bat
set "VER=v2.0.0"
build.bat
```

Windows PowerShell 5.1:

```powershell
$env:VER = "v2.0.0"
.\build.bat
```

The root entry delegates to `scripts/build.bat`; absolute-path invocation from another working directory is supported. If `VER` is unset, the latest Git tag is used, falling back to `dev`. Project-name-prefixed versions are not supported.

The script updates `build/windows/versioninfo.json`, generates `cmd/canyou/resource.syso`, and builds `dist/bin/Canyou-<version>.exe`. Web sources and the icon are embedded; no source directory is needed at runtime. Key flags (CMD):

```bat
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.Version=%VER%" -o "dist/bin/Canyou-%VER%.exe" ./cmd/canyou
```

Run `go test ./...` and `go vet ./...` for tests and checks. Some frontend-script tests require Node.js and are skipped if it is not installed.

- `-s -w`: strip symbol table and debug info to reduce size
- `-H=windowsgui`: GUI subsystem — no console window at runtime
- `CGO_ENABLED=0`: pure Go static build

### Exporting Documentation to HTML

Documentation export requires [Node.js](https://nodejs.org/) (including npm), Bash (Git Bash works on Windows), and the `marked` command-line tool. These are not required to run the EXE.

```bash
npm install -g marked
bash scripts/md2html.sh
```

Ensure `node` and `marked` are available in Bash; add npm's global command directory to `PATH` if necessary. If `marked` is missing, the script prints the installation command and exits with status `1`.

By default, the script exports both READMEs, images, and the license file to `dist/docs/`. Open `dist/docs/README.html` or `dist/docs/README.en.html`. Explicit Markdown paths are also supported, for example `bash scripts/md2html.sh README.en.md`. Rerun the script after README changes to synchronize HTML; generated files under `dist/` are not committed.

## 🧱 Tech Stack

| Area | Choice |
| --- | --- |
| Language | Go 1.22+ |
| Desktop GUI | [tadvi/winc](https://github.com/tadvi/winc) (Win32 wrapper) |
| QR code | [skip2/go-qrcode](https://github.com/skip2/go-qrcode) |
| File server | standard library `net/http` |
| Real-time messaging | [gorilla/websocket](https://github.com/gorilla/websocket) |
| Persistence | [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (no CGO required) |
| Web UI | Native HTML / CSS / JavaScript embedded with `go:embed`; no frontend framework or CDN |
| Version info / icon | `goversioninfo` + `go:embed` |

## 📁 Project Layout

```
.
├── cmd/canyou/      # Thin entry, version variable and generated Windows resources
├── internal/
│   ├── desktop/    # Native window, controls, clipboard and QR code
│   ├── server/     # HTTP lifecycle, routing and gate composition
│   ├── share/      # Listings, files, uploads and ZIP
│   ├── chat/       # WebSocket, rooms and connection lifecycle
│   ├── gate/       # Password, device authorization and gate API
│   ├── device/     # Shared identity, name, avatar and theme rules
│   ├── storage/    # SQLite messages, devices and existing migrations
│   ├── config/     # Config, password hashing and runtime data paths
│   ├── netinfo/    # Network adapter scanning and IP selection
│   ├── assets/     # Shared embedded icon
│   └── webui/      # Embedded rendering, templates/, styles/ and scripts/
├── scripts/        # Full build, version stamping and documentation conversion
├── build/windows/  # versioninfo.json
├── tests/          # integration/ cross-package acceptance and build/ tests
├── docs/images/    # README images
├── dist/           # Build and acceptance artifacts (ignored)
│   ├── bin/        # Built EXEs
│   └── docs/       # Exported bilingual HTML and images/; other artifacts also live in dist/
├── build.bat       # Familiar root build entry
├── go.mod / go.sum
├── README.md / README.en.md
└── .github/workflows/release.yml
```

## ⚠️ Notes

- Intended for **trusted LANs**. The password and device allowlist provide only a lightweight gate, **not HTTPS transport encryption or strong authentication**. Device identities are browser-supplied and can be deliberately forged. Do not expose the app directly to the public Internet or use it on untrusted networks.
- For display and copying in the desktop app, the access password is stored in the configuration as **plaintext as well as its hash**. Protect your local account and configuration backups. Passwords are submitted over HTTP; passwords visible in screenshots are demo values only.
- Changing the password does not revoke previously authorized devices. The online-device page is not an authorization-revocation interface. Chat is public: all connected devices can read retained messages and change the global retention limit.
- Batch / folder downloads are packaged as a **live stream**: the zip is built and sent on the fly without using temporary disk space, so the save prompt may lag slightly and the browser won't show a total-size progress bar.
- Files uploaded from the web page are saved to the separate *upload directory*, kept apart from the *view directory*.
- If a port cannot be bound, it may be in use by another program or fall within a **system-reserved port range** (Windows / Hyper-V); just pick a different port.
- The upload limit is 2 GB per request.

## 📄 License

This project is licensed under the [MIT License](./LICENSE). Please retain the copyright notice and license text when using, modifying, or distributing the software.

Copyright © 2026 ErWa.

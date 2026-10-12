# Canyou

> A lightweight Windows desktop utility: share a local folder over your LAN with one click. Scan the QR code on your phone (or any device on the same network) to browse, download, and upload files.

[简体中文](./README.md) · **English**

📦 Repository: **<https://github.com/IsErWa/canyou>**

---

## 📸 Screenshots

**Server (desktop tool)** — set the directories, pick the local IP and port, and an access QR code is generated automatically:

![Server](./img/server.png)

**Client (web page)** — scan the QR code on a phone or any LAN device to browse, download, and upload:

![Client](./img/client.png)

## ✨ Features

- 📂 **Dual-directory setup**: configure a *view directory* (for browsing / downloading) and an *upload directory* (to receive web uploads) separately
- 🖼️ **Instant QR access**: pick a local IP and port, and an access QR code is generated automatically — just scan to open
- ⬇️ **Flexible downloads**: download any single file, or tick multiple files / folders for a batch download that is streamed as a live zip on the fly (no temporary file)
- 📤 **Floating upload panel**: a bottom-right upload dock with drag-and-drop / multi-select, per-file removal, pagination for many files, and a full-width layout on phones; results are shown inline with no page navigation
- 🌐 **Modern responsive web UI**: adapts to dark / light mode, keeps the header and toolbar sticky while scrolling, and sorts the list by "folders first, then by modified time"
- 🔗 **One-click actions**: copy the access URL, open in default browser (only enabled while the service is running and the address is valid)
- ▶️ **Start / stop control**: a single button toggles sharing; the running state is shown live in gray / green
- 💾 **Persistent config**: directories, port, and last-selected IP are saved to `%AppData%/Canyou/config.json`
- 🛡️ **Robust port handling**: the port field accepts digits only and is limited to 1–65535; clear messages when a port is in use or reserved by the system
- 🪟 **No console window**: built as a GUI-subsystem executable — launches cleanly with no command-line flash

## 🖥️ Requirements

- **OS**: **Windows** only (relies on the Win32 GUI)
- **Network**: clients must be on the **same local network (LAN)**
- No runtime needed (pure Go, statically compiled, CGO disabled)

## 🚀 Usage

1. Double-click `Canyou.exe`
2. Choose a *view directory* (and optionally an *upload directory*)
3. Select the local IP and enter a port (default `8080`)
4. Click **Start sharing**
5. Scan the QR code with your phone, or open `http://<IP>:<port>` in a browser on any LAN device

> Changing the directory or port while running takes effect immediately; clearing the port or setting an invalid value stops the service automatically.

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

Build (Windows):

```bat
build.bat
```

The script generates `resource.syso` and compiles `Canyou.exe`. Key flags:

```
go build -trimpath -ldflags "-s -w -H=windowsgui" -o Canyou.exe .
```

- `-s -w`: strip symbol table and debug info to reduce size
- `-H=windowsgui`: GUI subsystem — no console window at runtime
- `CGO_ENABLED=0`: pure Go static build

## 🧱 Tech Stack

| Area | Choice |
| --- | --- |
| Language | Go 1.22+ |
| Desktop GUI | [tadvi/winc](https://github.com/tadvi/winc) (Win32 wrapper) |
| QR code | [skip2/go-qrcode](https://github.com/skip2/go-qrcode) |
| File server | standard library `net/http` |
| Version info / icon | `goversioninfo` + `go:embed` |

## 📁 Project Layout

```
.
├── main.go          # UI layout, interaction, start/stop and config logic
├── server.go        # net/http-based directory sharing service (start/stop/port errors)
├── upload.go        # web directory listing, upload and download handling
├── zip.go           # on-the-fly streaming zip packaging for batch / folder downloads
├── qrcode.go        # generate the access QR code as PNG
├── netinfo.go       # scan local LAN IPv4 addresses
├── config.go        # config persistence (%AppData%/Canyou/config.json)
├── icon.go          # embedded icon resource
├── statuslabel.go   # custom-drawn status label (gray / green)
├── numericedit.go   # native digits-only edit control (port)
├── build.bat        # build script
└── versioninfo.json # EXE version info and icon
```

## ⚠️ Notes

- This tool is for **LAN** file sharing and provides **no encryption or access control** — avoid sharing sensitive folders on untrusted networks.
- Batch / folder downloads are packaged as a **live stream**: the zip is built and sent on the fly without using temporary disk space, so the save prompt may lag slightly and the browser won't show a total-size progress bar.
- Files uploaded from the web page are saved to the separate *upload directory*, kept apart from the *view directory*.
- If a port cannot be bound, it may be in use by another program or fall within a **system-reserved port range** (Windows / Hyper-V); just pick a different port.
- The upload limit is 2 GB per request.

## 📄 License

Copyright © 2026 ErWa. All rights reserved.

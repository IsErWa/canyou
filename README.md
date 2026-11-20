# Canyou

> 轻量的 Windows 局域网共享工具：一个 EXE，手机扫码即可浏览、预览、下载和上传文件，也能通过公共聊天室交流消息。支持访问密码、在线设备与深浅色主题。

**简体中文** · [English](./README.en.md)

📦 仓库地址：**<https://github.com/IsErWa/canyou>**

---

## 📸 场景截图

主程序和传输限速截图来自当前 `dev` 开发版，网页截图保留 `v2.0.0` 的真实运行界面；目录、密码、设备和消息均为隔离的演示数据。桌面网页视口为 1440 × 900，手机截图为浏览器模拟的 390 × 844 视口，不代表手机真机测试。当前应用界面为中文。

### 主程序：配置与启停

| 已配置，尚未启动 | 共享服务运行中 |
| --- | --- |
| ![主程序未运行：目录、网卡、端口与访问密码](./docs/images/server-stopped.png) | ![主程序运行中：二维码、访问地址与停止服务按钮](./docs/images/server.png) |

![主程序传输限速：单客户端与所有客户端的上传、下载上限](./docs/images/bandwidth-settings.png)

### 桌面：文件与消息并排

![桌面工作台：文件列表与公共聊天室](./docs/images/chat-desktop.png)

### 手机：文件与消息切换

| 文件浏览 | 公共聊天室 |
| --- | --- |
| ![手机文件列表：文件信息分行显示](./docs/images/files-mobile.png) | ![手机聊天室：消息气泡与发送区](./docs/images/chat-mobile.png) |

<details>
<summary>访问验证：首次访问、密码错误与手机入口</summary>

设置访问密码后，新设备先验证密码，已授权设备再次访问时可自动通过。

![桌面访问密码验证](./docs/images/gate.png)
![密码错误时的提示](./docs/images/gate-error.png)
![手机访问密码验证](./docs/images/gate-mobile.png)

</details>

<details>
<summary>文件操作：根目录、子目录、批量下载与未读提醒</summary>

点击文件夹进入子目录，使用面包屑或返回按钮导航；勾选文件 / 文件夹后可打包下载。浏览文件时仍能收到消息并显示未读数。

![桌面文件列表](./docs/images/client.png)
![子目录与面包屑导航](./docs/images/directory.png)
![多选文件夹后准备打包下载](./docs/images/files-selection.png)
![文件视图中的消息未读提醒](./docs/images/chat-unread.png)

</details>

<details>
<summary>媒体预览：图片、音频与视频</summary>

点击媒体文件名打开浮层，支持前后切换和关闭；文件行尾的下载按钮仍直接下载。下面的图片、WAV 和 WebM 均为可加载的演示文件。

![图片浮层预览](./docs/images/preview-image.png)
![音频浮层与播放控件](./docs/images/preview-audio.png)
![视频浮层与播放控件](./docs/images/preview-video.png)

</details>

<details>
<summary>上传：文件选择、分页、成功结果与手机布局</summary>

选择或拖入多个文件后可逐项移除；超过 5 个文件时分页显示。上传结果留在当前窗口，文件保存到主程序配置的上传目录。

![桌面上传窗口：已选文件与分页](./docs/images/upload-desktop.png)
![上传成功后的就地反馈](./docs/images/upload-result.png)
![手机上传窗口](./docs/images/upload-mobile.png)

</details>

<details>
<summary>在线设备：桌面与手机</summary>

查看当前连接的设备、设备编号、名称与 IP；“我”标识当前设备。这里是实时在线列表，不是授权名单管理页。

![桌面在线设备列表](./docs/images/devices-desktop.png)
![手机在线设备列表](./docs/images/devices-mobile.png)

</details>

<details>
<summary>设置与外观：浅色、深色、设备资料及聊天保留条数</summary>

设置与消息、在线设备是平级面板。外观支持深色、浅色和跟随系统；名称、头像颜色与聊天保留条数各自保存。

![桌面浅色设置](./docs/images/settings-desktop-light.png)
![桌面深色设置](./docs/images/settings-desktop-dark.png)
![桌面深色文件列表](./docs/images/files-desktop-dark.png)

| 手机浅色设置 | 手机深色设置 | 手机深色文件列表 |
| --- | --- | --- |
| ![手机浅色设置](./docs/images/settings-mobile-light.png) | ![手机深色设置](./docs/images/settings-mobile-dark.png) | ![手机深色文件列表](./docs/images/files-mobile-dark.png) |

</details>

## ✨ 功能特性

- 📂 **双目录配置**：分别设置「查看目录」（供浏览 / 下载）与「上传目录」（接收网页上传）
- 🖼️ **二维码直达**：选择本机 IP 与端口后自动生成访问二维码，手机扫码即开
- ⬇️ **灵活下载**：每个文件可单独下载；勾选多个文件或文件夹即可批量下载，服务端实时流式打包为 zip（不落临时文件）
- 📤 **就地上传**：顶栏打开上传窗口，支持拖拽 / 多选、逐项移除、每页 5 个文件；手机端自适应，上传结果无需跳转
- 🖼️ **分类预览**：图片、音频与视频使用浮层；PDF、HTML、文本和代码文件在新标签页打开；其他类型下载，实际预览能力取决于浏览器与编码格式
- 🌐 **响应式单页工作台**：目录导航不刷新整页；宽屏保留文件区与工具面板，窄屏切换文件 / 消息；列表按「文件夹优先 + 修改时间」排序
- 💬 **公共聊天室**：访问页面即自动连接，支持历史消息、未读提醒、掉线自动重连与手动重连；浏览文件、切换面板不会主动断开聊天连接
- 👥 **在线设备与个人资料**：实时设备列表、稳定设备编号、自定义名称及 8 种预设头像颜色
- 🎨 **三种外观模式**：深色 / 浅色 / 跟随系统，主题偏好按设备保存
- 🔐 **可选访问门禁**：设置密码时，新设备验证后登记；未设置密码时，网页自动登记，无需输入密码。已授权设备再次访问可自动通过；更改密码无需重启服务，但不会撤销已有授权
- 🔗 **一键操作**：复制访问地址、用默认浏览器打开（仅在服务运行且地址有效时可用）
- ▶️ **启停与网络刷新**：单个按钮切换「启动服务 / 停止服务」；刷新或切换 IP 仅更新地址、二维码，不重启共享服务
- **传输限速**：分别设置单客户端和所有客户端合计的上传、下载上限，四项默认均为 10 MB/s；单客户端仍按 IP 计额，同 IP 多连接共用额度，保存后即时生效，0 表示不限速
- 💾 **本地持久化**：保存目录、端口、IP、访问密码和限速配置；SQLite 保存聊天记录、已授权设备及其名称、头像颜色和主题
- 🛡️ **健壮的端口处理**：端口输入框仅允许数字并限制 1–65535；端口被占用 / 被系统保留时给出中文友好提示
- 🪟 **无控制台黑框**：以 GUI 子系统编译，启动即用，不闪命令行窗口

## 🖥️ 系统要求

- **操作系统**：仅支持 **Windows**（依赖 Win32 GUI）
- **网络**：访问方需与本机处于**同一局域网**
- 无需安装运行库（纯 Go 静态编译，禁用 CGO）

## 🚀 使用方法

1. 从 [Releases](https://github.com/IsErWa/canyou/releases) 获取程序，或自行构建；双击运行 `Canyou-<版本>.exe`（本地构建产物位于 `dist/bin/`）。
2. 选择「查看目录」，按需设置独立的「上传目录」；不设置上传目录时，网页不提供上传入口。
3. 选择可被其他设备访问的本机 IP、输入端口（默认 `8080`）。网卡变化后可点击「刷新」。
4. 如需访问验证，输入「访问密码」并点击「设置密码」；清空后再次点击可关闭门禁。「复制」复制密码框当前内容。
5. 点击「启动服务」，扫描二维码或在同网段设备打开 `http://<IP>:<端口>`。如 Windows 防火墙提示，请仅按需允许受信任的专用网络访问。
6. 设置了访问密码时，新设备先输入密码；未设置密码时，网页自动登记设备，无需输入密码。进入后可浏览文件，或通过顶栏打开上传、消息、在线设备、设置。关闭工具面板返回文件视图。

### 常用操作

| 场景 | 操作 |
| --- | --- |
| 浏览与下载 | 点击文件夹进入；点击文件名预览 / 打开；行尾按钮下载；多选后点击工具栏下载按钮，生成 ZIP |
| 上传文件 | 点击「上传」，选择或拖入文件，检查列表后点击「上传文件」；全部成功后清空待上传列表 |
| 发送消息 | 桌面 `Enter` 发送、`Shift+Enter` 换行；手机回车换行，点击「发送」提交 |
| 修改设备资料 | 在「设置」中修改名称后点击「保存名称」；选择头像色块后点击「保存颜色」 |
| 切换外观 | 在「设置」中选择深色、浅色或跟随系统，选择后自动保存 |
| 聊天记录上限 | 默认保留 200 条，可设为 1–10000 条；这是全局设置，所有已连接设备均可修改，减少上限会删除最旧记录 |
| 连接中断 | 页面最多自动重试 5 次；仍未连接时，可点击「重新连接」 |
| 传输限速 | 主程序点击「传输限速」，设置单客户端 / 所有客户端的上传、下载上限，再点击「保存并应用」；服务未启动时也可设置 |

> 运行中修改目录或有效端口会重启服务以应用配置，现有连接会短暂中断；清空 / 输入非法端口会停止服务。刷新或切换 IP 不重启服务。刷新网页不会恢复尚未提交的上传文件。

### 上传与下载限速

主程序的「传输限速」提供四项统一设置，「单客户端」实际仍按来源 IP 汇总，不按连接或设备单独分配额度。不在网页中开放修改，也不设置指定 IP 的单独覆盖规则。上传指 **客户端 → 本机**，下载指 **本机 → 客户端**。

| 设置 | 作用 | `bandwidth` 配置字段（字节/秒） |
| --- | --- | --- |
| 单客户端 · 上传 | 同一 IP 的全部上传连接合计上限 | `per_ip_upload_bytes_per_second` |
| 单客户端 · 下载 | 同一 IP 的文件、ZIP、媒体预览合计上限 | `per_ip_download_bytes_per_second` |
| 所有客户端 · 上传 | 全部 IP 的上传合计上限 | `total_upload_bytes_per_second` |
| 所有客户端 · 下载 | 全部 IP 的下载合计上限 | `total_download_bytes_per_second` |

- 界面单位为 **MB/s**，`1 MB = 1,000,000 字节`；范围 0–10000，最多六位小数。默认四项均为 `10 MB/s`；新配置及缺失的限速项使用此默认值，已保存的数值（包括 `0`）不变。设为 `0` 表示对应层和方向不限速。限速配置包含非法数值或类型时，四项均回退为不限速并记录日志，不影响其他配置。
- 单客户端和所有客户端的限制同时生效。默认情况下，所有客户端合计上传最高 10 MB/s、合计下载最高 10 MB/s，并非每个客户端都能同时独占 10 MB/s。例如单客户端下载为 5、总下载为 20，则同一 IP 的多个连接共用 5 MB/s，全部客户端合计不超过总限额；任一层为 0 不会取消另一层的限制。上下行配额互不占用，不保证各 IP 严格均分总额度。
- 「保存并应用」写入 `%APPDATA%\Canyou\config.json`，随后热更新已有传输；无需重新连接或重启服务。保存失败保留当前运行限速，取消或关闭窗口不保存。
- 上传在读取 multipart 请求体时限速，包含表单开销；ZIP 按压缩后的实际输出计额。普通文件和媒体预览保留 Range 支持，单次上传大小上限仍为 2 GB。
- 聊天、门禁、目录页面和目录查询不经过限速等待；较低的下载限额可能导致视频缓冲。“聊天不受限速”不代表物理网络满载时没有延迟。
- 以服务端连接实际来源 IP 汇总，不采用客户端声明的 IP 或 `did`，也不信任代理转发头。同一 NAT / 反向代理出口后的客户端如果呈现为同一来源 IP，会共享该 IP 的额度。
- 这是应用层平均传输速率控制，允许少量令牌桶及网络缓存突发；不会限制客户端访问其他网站，也不是网卡或路由器 QoS。

### 设备授权流程

网页先检查已有登记；未登记且无需密码时，自动请求授权。设置密码时，校验密码正确后才登记。`check`、`unlock` 都以数据库记录为准，不因命中旧缓存而跳过查询。

| 接口（POST，JSON 请求体） | 职责 |
| --- | --- |
| `/gate/check` | 根据 `did` 查询设备记录；存在则更新授权缓存并返回设备资料。不存在则清除旧缓存，返回 `ok:false`、`allowed:false` 及 `requiresPassword`，不新增记录 |
| `/gate/unlock` | 接收 `did`、可选设备名 `name` 及访问密码 `password`；未设置密码时可省略密码。验证通过后先查库，仅在记录缺失时写入，确认资料可读取后更新缓存并返回 |
| `/gate/theme` | 仅保存已登记设备的主题；设备记录缺失时返回错误，需刷新页面重新授权，不自动补建记录 |

`check` 与 `unlock` 成功时返回 `ok:true`、`allowed:true` 及 `id`、`did`、`name`、`theme`。重复授权保留已有编号、名称、头像颜色、主题及授权时间。密码不匹配时 `unlock` 返回 HTTP 403；数据库读写失败返回 HTTP 500，不会虚报授权成功。

## 💾 数据保存与备份

| 数据 | 默认位置 |
| --- | --- |
| 主程序配置 | `%APPDATA%\Canyou\config.json` |
| 聊天记录、全局保留条数、设备授权和资料 | `%LOCALAPPDATA%\Programs\Canyou\chat.db` |
| 共享与上传文件 | 主程序中分别选择的查看目录、上传目录 |
| 本地打包程序 | `dist/bin/Canyou-<版本>.exe` |
| 文档转 HTML 脚本 | `scripts/md2html.sh`（运行 `bash scripts/md2html.sh`，输出至 `dist/docs/`） |

聊天与门禁共用同一个 SQLite 数据库。`LOCALAPPDATA` 未设置时，数据库路径回退到用户目录下的 `AppData\Local\Programs\Canyou\chat.db`。备份前先退出主程序，再复制配置、数据库及所需共享文件；EXE 不包含这些运行数据。

浏览器使用站点本地存储保存设备标识与界面状态。更换浏览器或清理站点数据后，可能需要重新验证。

如果直接删除 `allowed_devices` 中的设备记录，下次 `check` 检查到缺失时会清除该设备的授权缓存。刷新网页后，有密码时需重新验证，无密码时会自动调用 `unlock` 重新登记。重新登记按新记录处理，编号和资料不保证与删除前一致。

> 删除记录不等于立即踢出设备：程序不监听数据库删行，也不会主动断开已有 WebSocket。聊天改名、换色目前仍可能补建设备记录，因此直接删记录不是可靠的实时撤权或强制下线方式。

## 🔨 从源码构建

获取源码：

```bash
git clone https://github.com/IsErWa/canyou.git
cd canyou
```

前置依赖：

- [Go](https://go.dev/dl/) 1.22 及以上
- `goversioninfo`（用于嵌入图标与版本信息），首次安装：

  ```bash
  go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
  ```

构建（Windows CMD）：

```bat
set "VER=v2.0.0"
build.bat
```

Windows PowerShell 5.1：

```powershell
$env:VER = "v2.0.0"
.\build.bat
```

根入口转调 `scripts/build.bat`，也可从其他工作目录通过绝对路径调用。未设置 `VER` 时沿用最近 Git 标签，无标签时为 `dev`；版本不支持项目前缀。

脚本更新 `build/windows/versioninfo.json`，生成 `cmd/canyou/resource.syso`，输出 `dist/bin/Canyou-<版本>.exe`。网页与图标均嵌入 EXE，运行时无需源码目录。关键参数（以 CMD 为例）：

```bat
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.Version=%VER%" -o "dist/bin/Canyou-%VER%.exe" ./cmd/canyou
```

测试使用 `go test ./...` 与 `go vet ./...`；部分前端脚本测试需要 Node.js，未安装时会跳过。

- `-s -w`：去除符号表与调试信息，减小体积
- `-H=windowsgui`：GUI 子系统，运行时不分配控制台窗口
- `CGO_ENABLED=0`：纯 Go 静态编译

### 文档转 HTML

导出文档需安装 [Node.js](https://nodejs.org/)（包含 npm）和 Bash（Windows 可使用 Git Bash），再安装 `marked` 命令行工具；这些不是运行 EXE 的依赖。

```bash
npm install -g marked
bash scripts/md2html.sh
```

请确保 Bash 中可找到 `node` 和 `marked`，必要时将 npm 全局命令目录加入 `PATH`。脚本找不到 `marked` 时会提示安装命令，并以退出码 `1` 终止。

默认导出中英文 README、图片和许可文件至 `dist/docs/`，可打开 `dist/docs/README.html` 或 `dist/docs/README.en.html`。也支持显式传入 Markdown 路径，例如 `bash scripts/md2html.sh README.en.md`。修改 README 后需重新运行脚本以同步 HTML；`dist/` 导出产物不提交。

## 🧱 技术栈

| 类别 | 选型 |
| --- | --- |
| 语言 | Go 1.22+ |
| 桌面 GUI | [tadvi/winc](https://github.com/tadvi/winc)（Win32 封装） |
| 二维码 | [skip2/go-qrcode](https://github.com/skip2/go-qrcode) |
| 文件服务 | 标准库 `net/http` |
| 实时消息 | [gorilla/websocket](https://github.com/gorilla/websocket) |
| 数据持久化 | [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)（无需 CGO） |
| 网页 | 原生 HTML / CSS / JavaScript，通过 `go:embed` 嵌入；无前端框架或 CDN |
| 版本信息 / 图标 | `goversioninfo` + `go:embed` |

## 📁 项目结构

```
.
├── cmd/canyou/      # 极薄入口、版本变量及生成的 Windows 资源
├── internal/
│   ├── desktop/    # 原生窗口、控件、剪贴板与二维码
│   ├── server/     # HTTP 生命周期、总路由和门禁组装
│   ├── share/      # 目录列表、文件、上传与 ZIP
│   ├── bandwidth/  # 每 IP / 全局双层限速、请求体与响应流包装
│   ├── chat/       # WebSocket、聊天室和连接生命周期
│   ├── gate/       # 密码、设备授权与门禁 API
│   ├── device/     # 共用设备标识、名称、头像与主题规则
│   ├── storage/    # SQLite 消息、设备与既有迁移
│   ├── config/     # 配置、密码哈希与运行数据路径
│   ├── netinfo/    # 网卡扫描与 IP 选择
│   ├── assets/     # 共用嵌入图标
│   └── webui/      # 嵌入渲染、templates/、styles/、scripts/
├── scripts/        # 完整构建、版本注入、文档转换脚本
├── build/windows/  # versioninfo.json
├── tests/          # integration/ 跨包验收与 build/ 构建测试
├── docs/images/    # README 图片
├── dist/           # 构建与验收产物（不提交）
│   ├── bin/        # 打包生成的 EXE
│   └── docs/       # 导出的中英 HTML 文档与 images/；其他产物也放在 dist/
├── build.bat       # 保留的根构建入口
├── go.mod / go.sum
├── README.md / README.en.md
└── .github/workflows/release.yml
```

## ⚠️ 注意事项

- 本工具面向**受信任的局域网**。访问密码和设备允许名单仅提供轻量门禁，**不提供 HTTPS 传输加密或强身份认证**；设备标识由浏览器提交，可以被刻意伪造，请勿直接暴露到公网或用于不可信网络。
- 为便于主程序回显和复制，访问密码以**明文及其哈希**保存在配置文件中，请妥善保护本机账户与配置备份。密码通过 HTTP 提交，截图中的密码仅为演示值。
- 更改密码不会撤销已授权设备；「在线设备」也不是撤销授权的管理入口。聊天是公共房间，所有已连接设备可读取保留的消息并修改全局保留条数。
- 批量 / 文件夹下载采用**实时流式打包**：点击下载后边打包边传输、不占用磁盘临时空间，故下载确认可能略有延迟，且浏览器不显示总大小进度。
- 网页上传的文件保存到独立的「上传目录」，与「查看目录」分开放置。
- 若某端口无法监听，可能是被其它程序占用，或落在 Windows / Hyper-V 的**系统保留端口区间**，换一个端口即可。
- 上传大小上限为单次请求 2 GB。

## 📄 许可

本项目采用 [MIT License](./LICENSE) 许可。使用、修改或分发时，请保留版权声明和许可文本。

Copyright © 2026 ErWa.

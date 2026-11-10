# macOS 兼容性评估与分阶段适配

## 2026-11-10 原生图片与色块回退更新 mac.6

安装包为 `dist/macos-native-cover/Molpe-1.3.0-mac.6-arm64.dmg`，构建号 13006。
封面默认主动协商 Kitty 图片传输和 Unicode 图片占位符，要求终端真彩色支持。
旧 Kitty 0.28+ 和 Ghostty 1.x 的实际版本响应用于补足旧版功能查询。
图片通过直接 PNG 分块发送，每块最多 4096 字节。终端确认接收前使用色块，
确认后以 Unicode 占位符嵌入现有左侧封面布局，下方歌词和右侧歌单沿用当前布局。
封面内存缩略图提高到最大 1024×1024，最多保留 8 张。

能力未知、传输出错、确认超时、低色彩、系统 Terminal、tmux/screen 环境使用色块。
可用 `MOLPE_COVER_MODE=blocks` 强制兼容模式。切歌、缩放、弹窗、离开播放页和退出
按本前端分配的 ID 清理图片，迟到编码和确认按 ID 校正，普通重绘复用已传图片。
当前接入 Kitty 图片协议，其他图片协议使用色块。

双击应用通过系统 Terminal 启动。在 Kitty/Ghostty 中使用以下入口。

```sh
/Applications/Molpe.app/Contents/Resources/Player.command
```

完整测试含子测试 112 项通过、2 项跳过，go vet、完整 UI race 检查及 Linux 交叉构建通过。
DMG 只读挂载后复制到带空格的路径并运行真实 TUI，以协议模拟器验证 9 种路径，
覆盖能力响应、旧版响应、能力不足、错误、超时、强制色块、tmux 和禁用颜色。
原生路径验证实际 PNG 内容、分块尺寸、占位符、同专辑复用、缩放、弹窗、迟到确认和退出清理。
原有 Apple_Terminal 兼容路径继续验证选曲、封面缓存、快速切歌和音质弹窗。
代码签名、镜像和 SHA-256 校验通过。安装包采用本机临时签名。
本机未安装 Kitty，协议模拟验证后的实际终端画面仍需人工验收。

协议实现依据 [Kitty 官方说明](https://sw.kovidgoyal.net/kitty/graphics-protocol/)，
兼容终端参考 [Ghostty 官方功能说明](https://ghostty.org/docs/features)。

## 2026-11-10 播放页布局更新 mac.5

安装包为 `dist/macos-player-list/Molpe-1.3.0-mac.5-arm64.dmg`，构建号 13005。
左侧为专辑封面、歌曲信息和下方歌词，右侧显示后端快照中的当前播放歌单。
封面根据可用高度为歌词留出位置，继续使用双颜色象限字符。窄窗口将歌词和列表纵向排列。
右侧支持方向键或 j/k 选曲、Enter 播放、n 加入下一首播放、g/G 跳到首尾。
当前播放标记独立于选择光标，普通状态推送保留选择，切歌后跟随当前曲目。

完整测试 96 项通过、2 项跳过，界面相关 race 检查、go vet、Linux 交叉构建通过。
布局测试覆盖 8 种窗口尺寸和 1、5、15 行歌词配置，包括截图中的 146×41 窗口。
DMG 复制到带空格的路径后，以隔离 IPC 和伪终端验证列表滚动、选曲命令、封面缓存、
缩放恢复、浏览返回、音质弹窗与退出。签名、镜像和 SHA-256 校验通过。
终端测试使用模拟 Apple_Terminal 环境，静态预览由程序输出生成，歌词和曲目为示意内容。
安装包为本机临时签名测试版。

## 2026-11-10 封面细节更新 mac.4

安装包为 `dist/macos-album-hd/Molpe-1.3.0-mac.4-arm64.dmg`，构建号 13004。
封面显示宽度上限由 40 列提高到 96 列，播放区宽度上限提高到 200 列。
同时将单格上下采样升级为 2×2 采样，从象限字符中选择双颜色误差最小的组合。
内存缩略图上限为 320×320，仍在图片、尺寸或背景变化时重建字符画。
180×52 的预览窗口实际使用了 80×40 字符格、160×80 图像采样，歌词保留独立的右侧空间。

完整测试 94 项通过、2 项跳过。新增验证覆盖全部二色象限排列、大窗口分辨率增长、
96 列上限及大窗口中歌词宽度和整屏高度。DMG 复制运行、256 色输出、缩放恢复、快速切歌、
浏览返回和音质弹窗继续通过端到端验证。代码签名与镜像校验已检查。
预览为程序输出生成，使用示意歌词。最终字形由用户终端字体绘制。

## 2026-11-10 独立播放页更新 mac.3

安装包为 `dist/macos-album/Molpe-1.3.0-mac.3-arm64.dmg`，应用构建号 13003。
采用用户选择的独立播放页布局，左侧专辑封面、右侧滚动歌词。选歌后自动进入，
`b`/Esc 返回先前浏览页，`v` 再次进入。小窗口使用紧凑歌词布局，放大后恢复封面。

封面用 Unicode 上半块字符的前景与背景表示上下两个颜色像素，图像通过标准 Go 库处理。
封面异步下载，设有大小与解码尺寸限制，内存最多保存 8 张 160×160 缩略图。
同专辑复用封面，切歌取消旧请求并检查结果序号，缩放时直接复用已解码图像。
封面加载失败显示占位文字，可按 `r` 重试。表示层内存缓存随前端生命周期释放。

完整测试 92 项通过、2 项跳过，新增封面与播放页测试的 race 检查通过。
只读挂载 DMG 并复制到带空格的路径后，使用真实 TUI 和隔离 IPC/图片服务验证了初始播放页、
封面下载、缩放恢复、浏览返回、同专辑复用、快速切歌取消旧图、音质弹窗和完全退出。
模拟 Apple_Terminal 环境的 256 色输出已验证。静态布局预览由实际渲染输出生成，使用示意歌词。
实际终端窗口的字体、色板和尺寸仍可在安装后进行人工验收。

## 2026-11-10 音质选择更新 mac.2

新版安装包为 `dist/macos-quality/Molpe-1.3.0-mac.2-arm64.dmg`，应用构建号 13002。
按 `a` 打开五档音质选择，使用方向键或 j/k 移动，Enter 应用，Esc 取消。
偏好由后端保存。当前歌曲会按原进度和暂停意图重新加载，弹窗分别显示偏好和实际获得的音质。
旧后端会收到明确的升级提示，更新时先按 `q` 完全退出，再用新版 DMG 替换应用。

完整测试共 86 项通过、2 项跳过。新增验证覆盖保存失败、非法选项、连续切换、歌曲间隔、
队列与历史保持稳定，以及通过真实 mpv 和模拟接口返回验证 37 秒定位、暂停和实际等级回填。
从 DMG 复制出的应用在隔离伪终端中完成了五档选择、保存、取消、后台重连和退出持久化验证。
`go vet`、Linux amd64 编译、代码签名和磁盘镜像校验均通过。
个人账号可取得的实际音质仍由服务端返回决定，当前仍采用本机临时签名。

## 2026-11-10 测试与 DMG 交付更新

已生成 `dist/macos/Molpe-1.3.0-mac.1-arm64.dmg`，约 43 MiB。
镜像内包含 `Molpe.app`、Applications 拖拽目标和安装说明。
适用范围为本机验证的 Apple Silicon 与 macOS 26。

应用封装了现有终端播放器。原生启动器通过 Launch Services 打开系统终端，
终端运行包内的 `Player.command`。该脚本设置应用专用数据目录，后端由 Darwin 版本的
`EnsureRunning` 按需启动。独立进程会话支持 `Q` 后关闭终端继续播放，`q` 会停止后端。
后端持有文件锁，重复启动会在删除 socket 前被拒绝。信号处理也已提前到创建 socket 之前。

打包内置 mpv 0.41.0、49 个运行时动态库及 CA 证书，并把动态库引用改成包内相对路径。
Go 和 Homebrew 编译缓存属于构建环境。用户安装后由 `.app` 中的运行文件提供播放能力。
随包保存 Molpe、Go 依赖及 Homebrew 组件的许可证与组件清单。
构建脚本位于 `packaging/macos/build.py`，使用现有 Go 1.27.3 和 macOS 26.5 SDK。
本机的默认 27.0 SDK 与当前链接器存在架构描述不匹配，构建时显式选择了已有的 26.5 SDK。

新增打包代码及当前 Go 改动均保留在 `codex/macos-compat` 的本地工作区，供审阅和复现。

| 验证项 | 2026-11-10 结果 |
| --- | --- |
| 完整测试 | 78 项通过，2 项跳过。跳过项为没有会话 D-Bus 的 MPRIS 集成测试，以及需要显式启用个人账号的调试测试 |
| 音频 | 播放、暂停、恢复、定位、自然结束、进程和 socket 清理均通过。另通过 IPC 确认包内 mpv 选择了 CoreAudio 输出 |
| 包内运行 | 从 PATH 排除 Homebrew 后，使用包内 mpv 重跑全部 3 项播放器测试，均通过 |
| 动态库 | 检查 50 个 Mach-O 文件的依赖，均引用包内或系统库。`DYLD_PRINT_LIBRARIES` 验证运行时未加载 Homebrew 目录中的动态库 |
| DMG | `hdiutil verify` 通过，SHA256 校验文件已生成。只读挂载镜像并把应用复制到带空格的路径，复制后代码签名验证通过 |
| 应用运行链路 | 在伪终端运行复制后应用的 Player.command，按需启动独立后端、重复后端保护、Q 后重连同一 PID、q 后 socket 清理均通过 |
| 登录入口 | 新的隔离环境可以获取匿名二维码会话。个人账号扫码、歌单和会员音质仍需用户验收 |
| 代码检查 | `go vet`、Bash 语法、`git diff --check` 通过，Linux amd64 交叉编译通过 |
| 图形界面 | 电脑操作工具拒绝访问 Terminal，原生双击启动后的画面、字体和二维码扫描留待人工验收。程序运行链路通过伪终端完成验证 |

本包采用 ad-hoc 临时签名。本机签名列表中没有有效的 Developer ID 身份，
因此正式 Developer ID 签名与 Apple 公证仍是后续交付步骤。
原生媒体键、Control Center 集成也仍待开发。

DMG 版新增的用户文件集中存放在以下位置。

- `~/Library/Application Support/Molpe/config/molpe` 保存配置与自定义主题。
- `~/Library/Application Support/Molpe/data/molpe` 保存登录 Cookie 与播放队列。
- `~/Library/Caches/Molpe` 保存运行 socket、临时文件与 `molpe/daemon.log`。

这次运行测试通过显式环境变量使用新建的 `/tmp` 目录完成，Cookie 和日志权限检查通过。
验证摘要与详细日志位于 `/path/to/molpe-local/`。
卸载时先在播放器中按 `q`，把应用移到废纸篓。若要同时清除登录态，可再删除上述应用数据和缓存目录。

以下保留 2026-11-03 的初期评估，运行行为和当前状态以本节为准。

## 2026-11-03 初期评估记录

评估日期：2026-11-03；上游基线：`aed8ee709ab7a9aa68d4fc8e60cb49617de5a3b0`（v1.3.0）。
来源：通过已连接的 GitHub 集成读取仓库、README、AGENTS、依赖和源码树，再与本地克隆核对。
本地路径：`/path/to/Molpe`；适配分支：`codex/macos-compat`。
原始 `main`/`origin/main` 保留；本次不提交、不推送、不创建远程 PR，不安装登录启动服务。

## 结论与实测环境

核心设计适合移植到 macOS；首阶段使用手动 daemon 或隔离的 `dev.sh`。
这不等于完整的 macOS 桌面集成：MPRIS、系统媒体键、Control Center 和自动后台启动尚未适配。

| 项目 | 本机读取结果 |
| --- | --- |
| 硬件 | Mac17,3，Apple M5，10 个物理核心，16 GiB 内存 |
| 架构 | `uname -m=arm64`，`sysctl.proc_translated=0`，当前进程不是 Rosetta 转译 |
| 系统 | macOS 26.6，构建 25G72 |
| shell | `/bin/zsh`，zsh 5.9；开发脚本由 Apple `/bin/bash` 3.2.57 执行 |
| 编译工具 | Command Line Tools，Apple Clang 21.0.0，SDK 27.0，GNU Make 3.81 |
| 已装工具 | Homebrew 7.0.9、Git 2.56.0、Python 3.14.7、Node 26.10.0 |
| 初始缺少 | PATH 中的 Go 和独立 mpv；未发现 CMake、Ninja、pkg-config、Rust，但本项目标准构建不要求这些 |
| 当前终端 | 自动化环境 `TERM=dumb`，没有 `TERM_PROGRAM`，不能据此确认用户日常终端型号或真实显示效果 |

硬件结论来自 `sysctl` 和 `uname`，不是应用 user agent。已装 IINA，其目录没有独立 `mpv` 可执行文件，
不能当作此项目的 `mpv` 命令替代品。未发现现有 Molpe 仓库或正式版配置/数据目录。

## 架构、依赖与平台边界

`main.go` 分发 TUI 与 `daemon`。TUI 用 Bubble Tea v2 / Bubbles v2 / Lip Gloss v2；
`internal/ipc` 用 Unix socket + JSON Lines 传输状态与命令。
`internal/server` 单事件循环管理网易云、队列、mpv 和 MPRIS；后台播放不依赖 TUI 存活。
`internal/queue` 是纯逻辑；网络工作通过 goroutine 回送事件，进度按锚点外推。

| 边界 | 现状与 macOS 判断 |
| --- | --- |
| Go | `go.mod` 精确最低要求 Go 1.27.1；README 的“≥1.27”不够精确。采用官方 Go 1.27.3 darwin/arm64 做验证，保留依赖版本和校验和 |
| 音频 | `internal/player/player.go` 用 `exec.LookPath("mpv")` + 子进程 JSON IPC；无 libmpv C 绑定。macOS 应由 mpv 选择 CoreAudio，需要真实播放验证 |
| mpv 版本 | 代码依赖带索引参数及文件级选项的 `loadfile`，上游环境是 mpv 0.41.0；建议首先使用 0.41.0 验证，不承诺旧版本兼容 |
| daemon 拉起 | `internal/server/systemd.go` 运行 systemctl、生成 systemd 用户单元；macOS 没有这套启动机制。首阶段清晰提示手动启动，避免尝试 systemctl |
| 桌面媒体 | `internal/mpris` 使用 godbus/dbus/v5；连接失败已允许降级。macOS 的原生媒体控制不由 MPRIS 提供，安装 D-Bus 不能完成原生集成 |
| 目录 | `internal/config` 明确使用 XDG，缺省 `~/.config/molpe`、`~/.local/share/molpe`、`~/.cache/molpe`；首阶段保持这些路径，避免迁移现有数据 |
| socket | `internal/ipc` 优先 `$XDG_RUNTIME_DIR/molpe/ipc.sock`，否则 cache 目录下再加 `molpe/ipc.sock`。播放器另用 `os.TempDir()`；macOS Unix socket 路径长度较短，测试应关注长临时目录 |
| 信号 | server 使用 SIGINT/SIGTERM，macOS 可用；`q`/Ctrl+C 请求停止后端，`Q` 仅关闭前端 |
| 网易云 | netease-music/util 加解密、req/v3 的 Chrome TLS 指纹、cookiejar 持久化、go-qrcode；`linuxapi` 及 Linux UA 是服务端协议选择，不是本机 Linux 系统调用，不应为 Mac 随意替换 |
| 开发脚本 | Bash、GNU `install -D`、`/run/user/<uid>`、PipeWire/Pulse socket；改用 `mkdir -p` + BSD/GNU 均支持的 install，音频 socket 链接仅在 Linux 执行。Apple Bash 3.2 把变量后紧接的中文括号解析为变量名的一部分，使用 `${LOG}`/`${arg}` 避免启动与参数报错；后台使用 nohup 防止终端会话退出时收到 SIGHUP |
| 打包 | PKGBUILD/AUR 为 Arch x86_64、glibc、systemd；不能在 Mac 上运行或使用其二进制。直接源码构建 Mach-O arm64 |

`go.mod` 还包含 x/ansi、终端能力/字符宽度、uTLS/QUIC/压缩和 x/sys 等传递依赖；
不需要为了 Mac 重写 UI 或引入另一套网易云 API。实际依赖能否在 darwin 编译，以测试和构建日志为准。

## 终端假设

上游只实测 Arch + kitty，但二维码由 Unicode 块字符绘制，没有直接依赖 Kitty 图形协议、Sixel 或外部图片命令。
界面使用 ANSI、备用屏幕、UTF-8、中日韩字符宽度、方框/阴影字符及背景色。
基本交互是普通字母、方向键和 Ctrl+C；不要求复杂 Kitty 键盘协议。
使用支持 UTF-8 的交互式终端；建议窗口至少 80×30，尤其要保证二维码完整可见。
Terminal.app 可先验收基础交互，颜色/字宽/二维码还应在实际使用的终端检查；若追求上游显示效果，可另验 kitty。
不为本次验证安装或更改终端、字体与全局 shell 配置。

## 分阶段方案

1. **保留基线并验证构建**：独立克隆、独立分支，官方 arm64 Go 工具链放在仓库之外；运行原始测试，识别原有失败与环境跳过项。
2. **最小可运行适配（本次范围）**：BSD install 兼容、Apple Bash 3.2 中文变量边界、nohup 后台存活、仅 Linux 链接 PipeWire/Pulse、尊重原 XDG 目录；增加 `MOLPE_DEV_ROOT` 和 `--no-inherit`，避免测试复制个人登录态，并用 umask 077 保护新建日志和运行目录；macOS 缺少后台时给出手动运行指引。继续保留 Linux 的 systemd 行为。
3. **本机运行验收**：安装独立 mpv 后，在隔离目录验证生命周期、暂停/恢复、定位、自然结束、daemon/TUI 重连、`q`/`Q`、SIGTERM 清理；用真实终端人工检查二维码和中文布局。扫码、个人歌单与会员音质需要用户自己确认登录。
4. **后续原生集成**：评审 macOS 按需后台进程或 LaunchAgent 的生命周期/路径/环境/重复启动保护，再实现；媒体键和“正在播放”需独立的 macOS 系统集成（如 MediaPlayer 框架），避免直接让 mpv 绕过队列响应全局按键。届时补充原生验证；发布和上游变更另行审批。

## 构建、安装与运行

通用准备（供用户选择安装，未由本次适配自动执行）：

```sh
brew install go mpv
cd /path/to/Molpe
go version                         # 至少 1.27.1
mpv --version                      # 首先按 0.41.0 验证
go test -mod=readonly ./...
go build -mod=readonly -o molpe .
./dev.sh --no-inherit               # 首次干净环境，不复制正式配置/登录态
./dev.sh -r --no-inherit            # 修改后端后强制重启开发实例
```

本次验证下载的本地 Go 位于 `/path/to/molpe-local/go/bin/go`，
来源 `https://go.dev/dl/go1.27.3.darwin-arm64.tar.gz`，SHA256：
`dd3908cf20959d798c1e06ebe5b72213f0505ce01373b40ce3110975a6cd99c2`。
命令行可在当前会话把其 `bin` 加入 PATH；未修改 shell 配置或全局 Go 安装。

需要另外隔离一套运行数据时，使用短的绝对目录，后端和前端必须继承同一组 XDG 变量：

```sh
MOLPE_DEV_ROOT="/tmp/molpe-mac-$(id -u)" ./dev.sh --no-inherit
```

也可在两个终端分别执行 `./molpe daemon` 与 `./molpe`。手动 daemon 没有系统服务自动恢复；
关闭其终端或注销可能结束播放。`Q` 后要完全停止开发实例，重跑相同 dev 命令进入界面再按 `q`。
`--no-inherit` 只控制首次复制，不删除所选 dev 目录里已经存在的登录态。
若希望本地安装命令，可人工 `mkdir -p "$HOME/.local/bin"` 后 `install -m 755 molpe "$HOME/.local/bin/molpe"`，
并在自己的 PATH 中包含该目录；本次不执行安装。

## 验证结果

本机验证使用 Go 1.27.3 darwin/arm64，模块缓存及 Go 工具链均放在仓库外的
`/path/to/molpe-local/`；未改动 `go.mod`/`go.sum`，没有全局安装 Go 或 mpv。

| 检查 | 结果 |
| --- | --- |
| 原始测试与适配后测试 | `go test -mod=readonly ./...` / `go test -mod=readonly -count=1 -json ./...` 均成功；74 项通过、4 项跳过（计入子测试） |
| 跳过项 | `TestPlayerLifecycle`、`TestLoadPauseAndStart`：没有独立 mpv；`TestService`：没有会话 D-Bus；`TestDebugSongURL`：未显式启用个人账号调试 |
| 静态检查 | `go vet -mod=readonly ./...`、`bash -n dev.sh`、`git diff --check` 均通过 |
| Mac 构建 | `go build -mod=readonly -trimpath -o molpe .` 成功，`file` 确认 Mach-O arm64；可执行文件为 `/path/to/Molpe/molpe` |
| Linux 编译检查 | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -trimpath -o dist/molpe-linux-amd64 .` 成功；只检查编译，没有运行 Linux 服务 |
| 真实 daemon 与模拟终端 | 在 100×35 的伪终端运行真实 `dev.sh`/TUI；首次进入备用屏幕；重复前端被拒绝；`Q` 后后端存活，重开连接同一 PID；`q` 关闭后端并移除 socket。这里只证明终端协议和生命周期可运行，没有验证实际终端字体/画面或二维码可扫描性 |
| 手动启动与信号 | 无后端时直接运行 molpe 返回 macOS 手动启动指引，不生成 systemd 单元；手动 daemon 完成 IPC 握手后发 SIGTERM，退出码为 0 且移除 socket |
| 脚本与权限 | 用伪造配置和空 Cookie 测试 BSD install、原 XDG 源目录、带空格路径、`-r` 替换后端、`--no-inherit` 不复制主题/配置；Cookie 与日志均无组/其他用户访问权限；帮助/错误参数不创建运行目录；相对 dev 路径被拒绝；源配置未改动 |

测试使用全新的短 `/tmp/molpe-*` 隔离目录；没有读取或继承个人 Cookie，没有扫码登录。
测试启动的后端均已结束，不留后台服务。Go 测试的 JSON 日志和脚本/TUI 验证摘要保存在
`/path/to/molpe-local/`，构建产物被仓库忽略。

仍需处理的阻塞与验收：

- **缺少独立 mpv**：尚不能声称音频解码、CoreAudio、暂停/定位/自然结束或播放器进程清理已通过本机实测；安装 mpv 后重跑两个播放器集成测试。
- **需要用户登录**：个人歌单、扫码成功、受会员权限影响的播放地址与 Hi-Res 均未验证，用户可用 `./dev.sh -r --no-inherit` 在隔离环境自行扫码验收。
- **真实终端显示**：需检查中文字符宽度、主题颜色、完整二维码、窗口缩放和按键；自动化环境的 TERM=dumb 和伪终端不代替此项。
- **原生桌面功能留待后续**：macOS 自动拉起/登录服务、媒体键和 Control Center 尚未实现；此阶段后台仍通过 TUI 重连控制。
- **上游启动时序限制**：socket 文件刚出现不等于初始化完成；实测若此刻立即发 SIGTERM，可能在注册信号处理前退出（-15），留下待下次启动清理的 socket。完成 IPC 握手后清理正常。该原有时序问题未纳入本次脚本适配，后续启动管理应以握手为就绪条件并处理此窗口。

参考：[上游 README](https://github.com/Nk-YMZ/Molpe/blob/aed8ee709ab7a9aa68d4fc8e60cb49617de5a3b0/README.md)、
[依赖清单](https://github.com/Nk-YMZ/Molpe/blob/aed8ee709ab7a9aa68d4fc8e60cb49617de5a3b0/go.mod)、
[Go 官方下载](https://go.dev/dl/)、[mpv 官方手册](https://mpv.io/manual/stable/)。

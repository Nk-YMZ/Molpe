# AGENTS.md — 山歌（Mountain Air）

## 项目定位

仅面向 Linux 现代终端的网易云音乐 TUI 播放器。功能克制、界面美观、运行稳定、资源占用低，不复刻完整的网易云客户端。

核心需求：

- 登录网易云账号
- 查看用户收藏和创建的歌单及其中歌曲
- 播放、暂停、上一首、下一首
- 顺序播放、列表循环和可靠的随机播放
- “下一首播放”队列，用户指定的歌曲拥有最高播放优先级
- 支持 Hi-Res 音质
- 通过 MPRIS 与 Linux 桌面环境通信，接受 KDE 媒体组件和媒体键控制
- TUI 运行时播放；退出 TUI 后停止播放并清理相关进程

## 技术路线

- Go 开发
- Bubble Tea v2 / Bubbles v2 / Lip Gloss v2 构建 TUI（注意：v2 导入路径为 `charm.land/...`，如 `charm.land/bubbletea/v2`）
- 网易云接口统一使用 `github.com/go-musicfox/netease-music`（`service` + `util` 包），不引入其他网易云 API
- mpv 负责音频播放和解码，通过 IPC（JSON IPC socket）控制
- D-Bus/MPRIS 与桌面环境集成（预计使用 godbus/dbus 类库）
- 统一配置文件，遵循 XDG 目录规范（`$XDG_CONFIG_HOME`、`$XDG_DATA_HOME`、`$XDG_CACHE_HOME`）

## 项目结构

```
main.go              入口（装配配置、netease 客户端、TUI）
internal/config/     XDG 目录解析；面向用户的 config.json（音质偏好等）
internal/netease/    网易云接口封装：Cookie 持久化、二维码登录、账号信息、歌单、播放地址
internal/player/     mpv 子进程控制（JSON IPC over unix socket）
internal/mpris/      MPRIS2 D-Bus 服务：向桌面环境暴露元数据/状态，接收媒体控制
internal/ui/         Bubble Tea TUI：根模型 + 登录/歌单/歌曲页、主题、通用列表组件
```

界面约定：纯键盘操作（不为鼠标做额外设计）；默认主题为纯黑背景（#000000），
配色集中在 `internal/ui/theme.go`，通过 Theme 结构预留多主题扩展。

模块名为 `mountain-air`，远程仓库 `git@github.com:Nk-YMZ/Mountain-Air.git`（主分支 `main`）。后续按需要新增 `internal/queue`（播放队列）等，不提前创建。

## 已知坑：网易云风控（-462）

二维码登录接口检测 TLS 指纹，标准 Go http.Client 会被拒绝（code=-462）。
必须使用 `github.com/imroc/req/v3` 的 `SetTLSFingerprintChrome()` 模拟浏览器指纹
（参考 go-musicfox 的 `internal/ui/qr_login_client.go`）。获取 unikey 的请求
不得携带共享 CookieJar；轮询请求需绑定共享 Jar 以接收登录 Cookie（MUSIC_U）。

## 模块化与简洁性原则

- 模块清晰的单体结构：界面（TUI）、网易云接口、播放控制、播放队列、系统集成（MPRIS）、配置管理职责分离
- 只在确实需要隔离或测试的边界进行抽象；不建设插件系统或通用框架
- 不为可能永远不会出现的需求过度设计；不提前实现尚未需要的功能

## 编码、测试、性能与安全约定

- 实现简单、清晰，符合 Go 常见习惯；优先标准库和成熟依赖，控制依赖数量
- 控制后台活动（goroutine、定时器），重视功耗、CPU、内存及退出清理
- 核心播放与队列逻辑应易于单元测试（不依赖网络、外部进程）
- 界面层、服务接口、播放后端互不混杂
- 配置统一、可校验，为后续演进留出余地
- 妥善处理错误、异步任务和外部进程（尤其 mpv 子进程的生命周期与清理）
- 不记录、不提交登录凭据、Cookie 等敏感信息；敏感存储文件不得进入 Git

## 当前开发环境（2026-09-19 检测）

- Go 1.27.1（linux/amd64），GOPATH=`/home/oracle/go`，GOPROXY=`proxy.golang.org,direct`
- Git 2.55.0
- mpv v0.41.0
- 用户级 D-Bus 可用：`DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus`，`dbus-send`、`busctl` 在位
- 桌面环境：Plasma（Wayland），满足 KDE 媒体组件集成前提
- Git 仓库已初始化，默认分支 `main`

## 已引入的依赖

- `github.com/go-musicfox/netease-music` v1.6.0（网易云接口）
- `charm.land/bubbletea/v2` v2.0.9、`charm.land/bubbles/v2` v2.2.1、`charm.land/lipgloss/v2` v2.0.6（TUI）
- `github.com/imroc/req/v3` v3.61.0（二维码登录的 Chrome TLS 指纹模拟）
- `github.com/telanflow/cookiejar`（Cookie 持久化到文件，Netscape 格式）
- `github.com/skip2/go-qrcode`（终端二维码字符画渲染）

注意：Bubble Tea v2 与 v1 差异较大，如 `View() tea.View`（用 `tea.NewView` 构造）、按键消息为 `tea.KeyPressMsg`。

## 尚未确认的依赖

- mpv 是否支持所有网易云播放地址的流媒体协议与 Cookie/Header 传参，仍需持续验证

## 当前状态

二维码登录已完成并可运行：

- 启动时检查登录态（已登录则先刷新 Cookie 再进主界面）
- 未登录展示终端二维码（每 2s 轮询状态；800 过期 / 801 等待 / 802 待确认 / 803 成功）
- `r` 手动刷新二维码；`q`/Ctrl+C 退出
- Cookie 持久化到 `$XDG_DATA_HOME/mountain-air/cookies`（0600）

歌单浏览已实现：

- 登录后展示用户创建（✎）与收藏（♥）的歌单列表
- j/k/↑/↓ 移动，g/G 顶部/底部，enter 进入歌单查看歌曲，b/esc 返回，r 刷新
- 歌曲列表展示 歌名 - 艺术家 与时长（`PlaylistTrackAllService` 一次拉全）

基础播放已实现（尚无队列）：

- 歌曲页 enter 播放，space 播放/暂停
- 底部状态栏显示当前播放曲目与接口实际返回的音质
- 播放地址走 EAPI（官方客户端通道）：WEAPI 试听接口对会员歌曲的 Hi-Res
  会错误标注甚至混发文件，EAPI 的等级与文件均与实际一致
- 音质从面向用户的 `$XDG_CONFIG_HOME/mountain-air/config.json` 读取，支持
  `standard`、`higher`、`exhigh`、`lossless`、`hires`
- 运行期间不提供音质切换弹窗；修改配置后重新启动程序生效
- Hi-Res 实际状态会结合 mpv 加载后的采样率/位深校正，避免接口返回等级与实际流参数不一致
- 退出 TUI 时关闭 mpv 子进程并清理 IPC socket

MPRIS 桌面集成已实现（总线名 `org.mpris.MediaPlayer2.mountain-air`）：

- 向系统暴露元数据（标题/艺术家/专辑/时长/封面 `mpris:artUrl`）与播放状态，
  KDE 媒体组件可正常显示封面并控制
- 接收系统媒体控制（Play/Pause/PlayPause/Stop，映射为播放/暂停；无队列故不支持切歌）
- D-Bus 不可用时降级运行，不影响主体功能

播放队列尚未实现。

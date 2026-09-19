# AGENTS.md — 木末（Molpe）

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

界面约定：纯键盘操作（不为鼠标做额外设计）；默认主题为纯黑背景（#000000）。

UI 分层约定（交互/渲染解耦）：

- 交互层（`ui.go`、`list.go`、`lyrics.go`、`queue_popup.go`、`login.go`、`theme_picker.go` 等）
  禁止使用 lipgloss，只维护状态与响应消息；`View()` 前先将 Model 收敛为纯数据快照
  `viewState`（`view.go`），列表项等切片直接引用不拷贝
- 渲染层（`theme.go`、`view.go`）是唯一允许 import lipgloss 的地方；`renderXxx` 为纯函数，
  只消费快照与 styles，不接触 netease/queue 等内部类型
- 主题文件（`$XDG_CONFIG_HOME/molpe/themes/*.json`）只能改配色（colors）、符号（glyphs）
  与弹窗边框样式（border：rounded/square/none 枚举），不能改布局、尺寸、动效或新增装饰元素；
  布局由代码固定，歌词行数等数值统一在 config.json；
  **背景色不开放给主题，所有主题统一纯黑背景（#000000）**
- 主题与默认值逐字段合并（只需写出想覆盖的字段），缺失/损坏回退默认主题并提示；
  按 `t` 打开主题选择弹窗（打开时重新扫描目录，重新选中即热重载），主题名随 config 落盘
- 新功能的扩展路径：新 msg + Update 分支（交互）→ viewState 加字段（快照）→
  renderXxx + 需要时 Theme 加 token（渲染）

模块名为 `molpe`，远程仓库 `git@github.com:Nk-YMZ/Molpe.git`（主分支 `main`）。播放队列位于 `internal/queue`，其余模块按需新增，不提前创建。

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
- Cookie 持久化到 `$XDG_DATA_HOME/molpe/cookies`（0600）

歌单浏览已实现：

- 登录后展示用户创建（✎）与收藏（♥）的歌单列表
- j/k/↑/↓ 移动，g/G 顶部/底部，enter 进入歌单查看歌曲，b/esc 返回，r 刷新
- 歌曲列表展示 歌名 - 艺术家 与时长（`PlaylistTrackAllService` 一次拉全）

基础播放与播放队列已实现：

- 歌曲页 enter 播放，space 播放/暂停；]/[ 下一首/上一首；n 加入“下一首播放”队列；
  m 在顺序/列表循环/随机间切换；+/- 调整音量；b/esc 返回；
  底部状态栏显示当前曲目（含歌单内位置 `(X/Y)`）、音质、播放模式、音量与待播数量
- l 打开播放队列弹窗（`internal/ui/queue_popup.go`）：历史记录/正在播放/下一首播放/
  歌单后续分区展示，当前曲目锚定窗口中央，限高滚动；delete/Ctrl+d 删除选中条目
  （删除当前曲自动跳转下一首），l/b/esc 关闭；随机模式不显示歌单后续
- 切歌（点歌/上一首/下一首/弹窗删除当前曲）一律解除暂停自动开播；
  `handleSongURL` 无条件将暂停状态同步给 mpv（其 pause 属性跨 loadfile 保持，不重置）

歌词显示已实现（`internal/ui/lyrics.go`、`internal/netease/lyrics.go`）：

- 歌词区固定在正文与状态栏之间，全局常驻；当前句居中高亮，上下各半，开头结尾留空；
  第一句未开始（前奏）时按第一句居中排版但不高亮，避免整区位置跳变；歌词文本水平居中于窗口

播放进度条已实现（`internal/ui/playback.go` + 渲染层 `renderProgress`）：

- 进度条固定在歌词区与状态栏之间，格式 `mm:ss ████░░░░ mm:ss`，宽度随窗口自适应，
  窄终端（<22 列）退化为纯时间文本；有正在播放的曲目即常显（切歌时位置归零、
  由新曲目元数据时长立即重绘），仅无播放时整行留空，保持栅格稳定
- 已播放/头部/未播放字符均为主题符号（progress_fill/head/empty），颜色取 accent/muted
- 刷新为 1Hz 按需定时（`scheduleProgressTick`），仅播放中运行；暂停、待播、无播放时
  完全停止；定时器带序号（progressSeq）丢弃暂停/切歌后的残留定时器

整体布局（自上而下，代码固定）：头部（左标题、右模式·音质·音量弱化信息）→ 正文 →
歌词区 → 进度条 → 状态栏（播放图标 + 曲目 + 位置 + 待播数，次要信息已移至头部右侧）→ 帮助栏。
栅格锚定：正文区固定高度（总高 - 头部/空行/歌词/进度/状态/帮助），超出截断、不足补空行，
底部区块（歌词/进度/状态/帮助）恒定锚定在屏幕底部，不随正文内容多少浮动；
登录页二维码只补不截（截断将无法扫描）。
- 歌词走 `LyricService`（linuxapi），返回 LRC 文本（每句带 `[mm:ss.xx]` 时间戳），
  翻译按相同时间戳并入原文行；`parseLRC`/`mergeTranslation`/`lyricLineAt` 为纯函数，附单测
- 滚动采用按需定时（`scheduleLyricTick`）：在下一句歌词到来时刻唤醒一次并重新校位，
  暂停、无歌词、最后一句后完全停止；定时器带序号（lyricSeq）防止暂停/切歌后残留过期定时器
- 切歌清空歌词并异步拉取（按歌曲 ID 丢弃过期响应）；列表高度扣除歌词区行数
- 快捷键集中在 `internal/ui` 的 `defaultKeyMap()`（Model.keys 字段），
  为后续配置文件自定义预留；状态栏操作提示约 3 秒后自动恢复为播放信息
- 配置项（`config.json`）：`quality` 音质、`auto_play` 启动自动开播（默认 false，恢复为待播）、
  `volume` 音量（0-100，缺省 100，退出时统一固化回配置文件，运行期间不写盘）、`volume_step` 调节步进（默认 5）、
  `lyric_translation` 歌词是否含翻译（默认 true）、`lyric_lines` 歌词显示总行数（默认 5，范围 1-15）、
  `theme` 主题名（默认 default，对应 themes/<name>.json）
- 队列核心在 `internal/queue`（纯逻辑、可单测，不依赖网络与外部进程）：
  - 顺序/列表循环/随机三种模式；随机为除当前曲外在歌单内等概率选取（不做预打乱），
    单曲歌单重复播放
  - 历史队列严格记录实际播放顺序（默认上限 100 首，跨歌单保留），
    上一首/下一首先沿历史前后移动，再消费“下一首播放”队列，最后按模式选取
  - “下一首播放”队列按添加时间 FIFO，任何模式下优先级最高；手动切歌/切歌单时保留
  - 行为参数集中在 `queue.Options`（如 HistoryLimit），为配置化预留
  - 队列快照（歌单/历史/位置/下一首队列/模式）持久化到 `$XDG_DATA_HOME/molpe/queue.json`，
    每次变动即写入；启动时恢复，上次播放的歌曲恢复为待播（暂停）状态，按 space 或桌面播放键开始
  - 支持从历史/下一首队列/歌单删除条目（RemoveHistory/RemoveNextUp/RemovePlaylist/RemoveCurrent）；
    当前曲为显式字段（持有副本），RemoveCurrent 保留歌单位置锚点以保证 Next 从原位置之后继续
- 健壮性：网易云请求统一 15s 超时；播放地址请求带递增序号、歌单响应带歌单 ID，过期响应丢弃；
  main 层对 SIGTERM 等非按键退出兜底清理 mpv/D-Bus；配置文件损坏时以默认配置运行并提示，
  且该次运行退出不写回
- mpv 通过独立 IPC 长连接监听 `end-file`（仅 reason=eof）实现自然播完自动连播
- 播放地址走 EAPI（官方客户端通道）：WEAPI 试听接口对会员歌曲的 Hi-Res
  会错误标注甚至混发文件，EAPI 的等级与文件均与实际一致
- 音质从面向用户的 `$XDG_CONFIG_HOME/molpe/config.json` 读取，支持
  `standard`、`higher`、`exhigh`、`lossless`、`hires`
- 运行期间不提供音质切换弹窗；修改配置后重新启动程序生效
- Hi-Res 实际状态会结合 mpv 加载后的采样率/位深校正，避免接口返回等级与实际流参数不一致
- 退出 TUI 时关闭 mpv 子进程并清理 IPC socket

MPRIS 桌面集成已实现（总线名 `org.mpris.MediaPlayer2.molpe`）：

- 向系统暴露元数据（标题/艺术家/专辑/时长/封面 `mpris:artUrl`）与播放状态，
  KDE 媒体组件可正常显示封面并控制
- 接收系统媒体控制（Play/Pause/PlayPause/Stop/Next/Previous，切歌走队列逻辑）
- D-Bus 不可用时降级运行，不影响主体功能

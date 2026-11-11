#!/usr/bin/env bash
# dev.sh — 在完全隔离的环境中运行开发版木末，不影响已安装的 AUR 版本。
#
# 做法：把 XDG 各目录指向 /tmp/molpe-dev/，dev 实例的 socket、配置、
# Cookie、队列持久化全部独立；手动启动后端（绕过 systemd，不会拉起
# 旧版 /usr/bin/molpe），再启动前端连接自己的 socket。
#
# 后端脱离脚本独立存活，还原正式版的完整生命周期：
# - TUI 内 q/Ctrl+C：后端收到退出命令自行结束，下次运行自动起新后端；
# - TUI 内 Q（转后台）：后端继续播放，再次运行 ./dev.sh 直接重连恢复；
# - ./dev.sh -r：强制杀掉旧后端再起新的（改了后端代码想立即生效时用）。
#
# 注意：
# - 首次运行会继承正式版的登录 Cookie、config.json 与自定义主题
#   （仅当 dev 环境还没有），删除 /tmp/molpe-dev 即恢复全新环境。
# - MPRIS 总线名全用户唯一：测试媒体键/桌面组件前请先
#   `systemctl --user stop molpe` 停掉正式版后端。
# - 后端日志在 /tmp/molpe-dev/daemon.log。
# - Linux 音频依赖符号链接进 dev 运行目录的 pipewire/pulse socket，
#   若重新登录后无声，重跑本脚本即可重建链接。
# - macOS 使用 mpv 的系统音频输出，不链接 Linux 音频 socket；
#   暂无原生系统媒体键集成。--no-inherit 可禁止复制正式版数据。

set -euo pipefail
umask 077
cd "$(dirname "$0")"

# 先解析参数，--help 不创建目录或启动进程。
FORCE=0
INHERIT=1
for arg in "$@"; do
	case "$arg" in
	-r | --rebuild) FORCE=1 ;;
	--no-inherit) INHERIT=0 ;;
	-h | --help)
		echo "用法: ./dev.sh [-r|--rebuild] [--no-inherit]"
		echo "  无参数  后端没在跑则启动，正在跑则直接重连（保留转后台体验）"
		echo "  -r      强制重启后端，让新构建的后端代码立即生效"
		echo "  --no-inherit  不复制正式版配置、主题或登录 Cookie"
		echo "  MOLPE_DEV_ROOT  指定隔离目录（默认 /tmp/molpe-dev）"
		exit 0
		;;
	*) echo "未知参数: ${arg}（-h 查看用法）" >&2; exit 2 ;;
	esac
done

REAL_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
REAL_DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
REAL_RUNTIME="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
DEV_ROOT="${MOLPE_DEV_ROOT:-/tmp/molpe-dev}"
if [[ "$DEV_ROOT" != /* ]]; then
	echo "错误：MOLPE_DEV_ROOT 必须是绝对路径" >&2
	exit 2
fi
export XDG_RUNTIME_DIR="$DEV_ROOT/run"
export XDG_CONFIG_HOME="$DEV_ROOT/config"
export XDG_DATA_HOME="$DEV_ROOT/data"
export XDG_CACHE_HOME="$DEV_ROOT/cache"
mkdir -p "$XDG_RUNTIME_DIR" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME"

# XDG_RUNTIME_DIR 被覆盖后，音频服务的 socket（pipewire/pulse）在该目录下
# 找不到，mpv 会无声。把真实运行目录里的音频 socket 链接进来。
# pulse 可能是上次运行被误建的空目录，先清掉再链接。
if [[ "$(uname -s)" == Linux ]]; then
	for s in pipewire-0 pipewire-0-manager pulse; do
		[[ -e "$REAL_RUNTIME/$s" ]] || continue
		[[ -d "$XDG_RUNTIME_DIR/$s" && ! -L "$XDG_RUNTIME_DIR/$s" ]] && rm -rf "$XDG_RUNTIME_DIR/$s"
		ln -sfn "$REAL_RUNTIME/$s" "$XDG_RUNTIME_DIR/$s"
	done
fi

BIN=./molpe-dev-bin
PIDFILE="$XDG_RUNTIME_DIR/daemon.pid"
LOG="$DEV_ROOT/daemon.log"

daemon_alive() {
	[[ -f "$PIDFILE" ]] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}

echo "==> 构建开发版"
go build -o "$BIN" .

if daemon_alive && [[ "$FORCE" -eq 0 ]]; then
	echo "==> dev 后端正在运行 (pid $(cat "$PIDFILE"))，直接重连（-r 可强制重启）"
else
	if daemon_alive; then
		echo "==> 停止旧 dev 后端 (pid $(cat "$PIDFILE"))"
		kill "$(cat "$PIDFILE")" 2>/dev/null || true
		for _ in $(seq 1 20); do
			daemon_alive || break
			sleep 0.1
		done
	fi

	# dev 还没有配置时，继承正式版的 config.json 与自定义主题，
	# 保持音质、主题、歌词行数等偏好一致；之后两者各自独立演化。
	if [[ "$INHERIT" -eq 1 && ! -f "$XDG_CONFIG_HOME/molpe/config.json" && -f "$REAL_CONFIG_HOME/molpe/config.json" ]]; then
		mkdir -p "$XDG_CONFIG_HOME/molpe"
		install -m 644 "$REAL_CONFIG_HOME/molpe/config.json" "$XDG_CONFIG_HOME/molpe/config.json"
		echo "==> 已继承正式版配置文件"
	fi
	if [[ "$INHERIT" -eq 1 && -d "$REAL_CONFIG_HOME/molpe/themes" && ! -d "$XDG_CONFIG_HOME/molpe/themes" ]]; then
		mkdir -p "$XDG_CONFIG_HOME/molpe"
		cp -R "$REAL_CONFIG_HOME/molpe/themes" "$XDG_CONFIG_HOME/molpe/themes"
		echo "==> 已继承正式版自定义主题"
	fi

	# dev 数据目录还没有 Cookie 时，从正式版复制一份，免去扫码登录。
	if [[ "$INHERIT" -eq 1 && ! -f "$XDG_DATA_HOME/molpe/cookies" && -f "$REAL_DATA_HOME/molpe/cookies" ]]; then
		mkdir -p "$XDG_DATA_HOME/molpe"
		install -m 600 "$REAL_DATA_HOME/molpe/cookies" "$XDG_DATA_HOME/molpe/cookies"
		echo "==> 已复制正式版登录 Cookie 到 dev 环境"
	fi

	echo "==> 启动 dev 后端（日志: ${LOG}）"
	# disown 只影响 shell 的作业表；nohup 同时防止终端会话结束时的 SIGHUP。
	nohup "$BIN" daemon </dev/null >>"$LOG" 2>&1 &
	echo "$!" > "$PIDFILE"
	disown

	# 等待后端 socket 就绪（含 Cookie 加载与队列恢复）。
	sock="$XDG_RUNTIME_DIR/molpe/ipc.sock"
	for _ in $(seq 1 100); do
		[[ -S "$sock" ]] && break
		sleep 0.1
	done
	if [[ ! -S "$sock" ]]; then
		echo "错误：等待 dev 后端 socket 就绪超时，日志见 $LOG" >&2
		exit 1
	fi
fi

echo "==> 启动 dev 前端"
"$BIN" || true

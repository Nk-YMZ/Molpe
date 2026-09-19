// Package player 通过 JSON IPC 控制 mpv 进行音频播放。
package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Player 持有一个 mpv 子进程及其 IPC socket。
type Player struct {
	cmd    *exec.Cmd
	socket string
	exited chan struct{} // mpv 进程退出时关闭

	mu         sync.Mutex
	closed     bool
	endWatched bool
}

// Start 启动 mpv 子进程并等待 IPC socket 就绪。
func Start() (*Player, error) {
	if _, err := exec.LookPath("mpv"); err != nil {
		return nil, fmt.Errorf("未找到 mpv，请先安装: %w", err)
	}

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("molpe-mpv-%d.sock", os.Getpid()))
	_ = os.Remove(socket)

	cmd := exec.Command("mpv",
		"--idle=yes",
		"--no-video",
		"--no-terminal",
		"--input-ipc-server="+socket,
		"--http-header-fields=Referer: https://music.163.com/",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 mpv 失败: %w", err)
	}

	p := &Player{cmd: cmd, socket: socket, exited: make(chan struct{})}
	go func() {
		_ = p.cmd.Wait()
		close(p.exited)
	}()
	if err := p.waitSocket(3 * time.Second); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// waitSocket 轮询等待 IPC socket 出现；mpv 提前退出时立即失败，避免白等满超时。
func (p *Player) waitSocket(timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(p.socket); err == nil {
			return nil
		}
		select {
		case <-p.exited:
			return errors.New("mpv 进程在 IPC socket 就绪前退出")
		case <-timer.C:
			return errors.New("等待 mpv IPC socket 就绪超时")
		case <-tick.C:
		}
	}
}

// Play 加载并播放指定的音频地址。
func (p *Player) Play(url string) error {
	_, err := p.command("loadfile", url)
	return err
}

// SetPause 设置暂停状态。
func (p *Player) SetPause(paused bool) error {
	_, err := p.command("set_property", "pause", paused)
	return err
}

// SetVolume 设置音量百分比（0-100）。
func (p *Player) SetVolume(v int) error {
	_, err := p.command("set_property", "volume", v)
	return err
}

// SetEndCallback 注册曲目自然播完（end-file 且原因为 eof）时的回调，
// 用于队列自动连播。回调在独立 goroutine 中触发；重复调用仅首次生效。
func (p *Player) SetEndCallback(cb func()) {
	p.mu.Lock()
	if p.closed || p.endWatched {
		p.mu.Unlock()
		return
	}
	p.endWatched = true
	p.mu.Unlock()
	go p.watchEnd(cb)
}

// watchEnd 持有独立的 IPC 长连接监听 end-file 事件；
// socket 关闭或 mpv 退出时 goroutine 自动结束。
func (p *Player) watchEnd(cb func()) {
	conn, err := net.Dial("unix", p.socket)
	if err != nil {
		return
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(map[string]any{
		"command": []any{"enable_event", "end-file"},
	}); err != nil {
		return
	}
	dec := json.NewDecoder(conn)
	for {
		var ev struct {
			Event  string `json:"event"`
			Reason string `json:"reason"`
		}
		if err := dec.Decode(&ev); err != nil {
			return
		}
		// 仅响应自然播完；手动切歌产生的 stop/redirect 等事件不触发连播。
		if ev.Event == "end-file" && ev.Reason == "eof" && cb != nil {
			cb()
		}
	}
}

func (p *Player) command(args ...any) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lockedCommand(args...)
}

func (p *Player) lockedCommand(args ...any) (json.RawMessage, error) {
	if p.closed {
		return nil, errors.New("播放器已关闭")
	}
	conn, err := net.DialTimeout("unix", p.socket, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接 mpv 失败: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(map[string]any{"command": args}); err != nil {
		return nil, fmt.Errorf("发送 mpv 命令失败: %w", err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("读取 mpv 响应失败: %w", err)
	}
	var resp struct {
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("解析 mpv 响应失败: %w", err)
	}
	if resp.Error != "success" {
		return nil, fmt.Errorf("mpv 命令失败: %s", resp.Error)
	}
	return resp.Data, nil
}

// Position 返回当前播放位置（秒）。
func (p *Player) Position() (float64, error) {
	data, err := p.command("get_property", "time-pos")
	if err != nil {
		return 0, err
	}
	if string(data) == "null" {
		return 0, errors.New("当前没有可用播放位置")
	}
	var pos float64
	if err := json.Unmarshal(data, &pos); err != nil {
		return 0, fmt.Errorf("解析播放位置失败: %w", err)
	}
	return pos, nil
}

// Close 退出 mpv 子进程并清理 IPC socket。
func (p *Player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	// 先请求优雅退出；失败则强制结束。
	_, _ = p.lockedCommand("quit")
	p.closed = true
	p.mu.Unlock()

	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
	_ = os.Remove(p.socket)
}

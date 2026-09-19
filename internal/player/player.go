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

	mu     sync.Mutex
	closed bool
}

// Start 启动 mpv 子进程并等待 IPC socket 就绪。
func Start() (*Player, error) {
	if _, err := exec.LookPath("mpv"); err != nil {
		return nil, fmt.Errorf("未找到 mpv，请先安装: %w", err)
	}

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("mountain-air-mpv-%d.sock", os.Getpid()))
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

	p := &Player{cmd: cmd, socket: socket}
	if err := p.waitSocket(3 * time.Second); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func (p *Player) waitSocket(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(p.socket); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("等待 mpv IPC socket 就绪超时")
		}
		time.Sleep(20 * time.Millisecond)
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

	done := make(chan struct{})
	go func() {
		_ = p.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = p.cmd.Process.Kill()
		<-done
	}
	_ = os.Remove(p.socket)
}

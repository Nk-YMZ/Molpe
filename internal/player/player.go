// Package player 通过 JSON IPC 控制 mpv 进行音频播放。
package player

import (
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

	mu            sync.Mutex
	closed        bool
	eventsWatched bool
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

// Load 加载音频地址并开播；start > 0 时从指定位置开始（开播即定位，
// 避免开播后再 seek 的二次跳转）；paused 为 true 时加载后保持暂停。
// pause 必须以 loadfile 文件级选项与加载原子生效：加载后单独
// set_property pause 会与 mpv 打开文件的过程竞争，概率性返回空错误；
// 显式传 yes/no 也保证不受上一曲遗留的全局暂停状态影响。
func (p *Player) Load(url string, start time.Duration, paused bool) error {
	opts := map[string]string{"pause": "no"}
	if paused {
		opts["pause"] = "yes"
	}
	if start > 0 {
		opts["start"] = fmt.Sprintf("%.3f", start.Seconds())
	}
	_, err := p.command("loadfile", url, "replace", 0, opts)
	return err
}

// Seek 跳转到绝对播放位置（暂停状态下同样生效，且保持暂停）。
// mpv 会对 HTTP 流发起 Range 请求重新缓冲，跳转后有短暂无声属正常。
func (p *Player) Seek(pos time.Duration) error {
	_, err := p.command("set_property", "time-pos", pos.Seconds())
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

// Stop 停止当前播放并清空播放状态，进程保持空闲；
// 用于播放失败后收敛状态（旧曲停止出声，等待重试）。
func (p *Player) Stop() error {
	_, err := p.command("stop")
	return err
}

// Exited 返回进程退出通知通道（mpv 进程退出时关闭）。
// 用于服务端感知播放器异常退出；主动 Close 后同样会关闭。
func (p *Player) Exited() <-chan struct{} {
	return p.exited
}

// SetEndCallback 注册曲目结束（end-file）事件的回调，reason 为 mpv 的
// 结束原因：eof 为自然播完（用于队列连播），error 为流播放中途失败
// （网络中断、CDN 不可达等，用于接入失败重试）；主动操作产生的
// stop/redirect 等事件不上报。回调在独立 goroutine 中触发；重复调用仅首次生效。
func (p *Player) SetEndCallback(cb func(reason string)) {
	p.SetEventCallback(func(event, reason string) {
		if event == "end-file" && cb != nil {
			cb(reason)
		}
	})
}

// SetEventCallback 监听结束、加载完成和跳转恢复事件。注册完成后才允许后续 Load，
// 避免短文件在监听连接建立前已经加载完成。重复调用仅首次生效。
func (p *Player) SetEventCallback(cb func(event, reason string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.eventsWatched {
		return
	}
	conn, err := net.DialTimeout("unix", p.socket, 2*time.Second)
	if err != nil {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	dec := json.NewDecoder(conn)
	commands := [][]string{{"disable_event", "all"}, {"enable_event", "end-file"}, {"enable_event", "file-loaded"}, {"enable_event", "playback-restart"}}
	for _, command := range commands {
		if err = json.NewEncoder(conn).Encode(map[string]any{"command": command}); err != nil {
			conn.Close()
			return
		}
		for {
			var response struct {
				Error string `json:"error"`
			}
			if err = dec.Decode(&response); err != nil {
				conn.Close()
				return
			}
			if response.Error == "" {
				continue
			}
			if response.Error != "success" {
				conn.Close()
				return
			}
			break
		}
	}
	_ = conn.SetDeadline(time.Time{})
	p.eventsWatched = true
	go p.watchEvents(conn, dec, cb)
}

func (p *Player) watchEvents(conn net.Conn, dec *json.Decoder, cb func(event, reason string)) {
	defer conn.Close()
	for {
		var ev struct {
			Event  string `json:"event"`
			Reason string `json:"reason"`
		}
		if err := dec.Decode(&ev); err != nil {
			return
		}
		switch ev.Event {
		case "end-file":
			if cb != nil && (ev.Reason == "eof" || ev.Reason == "error") {
				cb(ev.Event, ev.Reason)
			}
		case "file-loaded", "playback-restart":
			if cb != nil {
				cb(ev.Event, "")
			}
		}
	}
}

// AudioInfo 包含源路径和解码器输出，Format 保留 mpv 的样本格式（如 s32）。
// s32 是解码存储格式，24-bit FLAC 也可能使用该格式。
type AudioInfo struct {
	Source     string
	Codec      string
	Format     string `json:"format"`
	SampleRate int    `json:"samplerate"`
}

// AudioInfo 在同一命令锁内读取来源和解码参数，切歌期间的结果由调用者核对来源。
func (p *Player) AudioInfo() (AudioInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var info AudioInfo
	path, err := p.lockedCommand("get_property", "path")
	if err != nil {
		return info, err
	}
	if err = json.Unmarshal(path, &info.Source); err != nil {
		return info, err
	}
	data, err := p.lockedCommand("get_property", "audio-params")
	if err != nil {
		return info, err
	}
	if err = json.Unmarshal(data, &info); err != nil {
		return info, err
	}
	if info.SampleRate <= 0 || info.Format == "" {
		return info, errors.New("解码参数尚未就绪")
	}
	codec, err := p.lockedCommand("get_property", "audio-codec-name")
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(codec, &info.Codec)
	return info, err
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

	dec := json.NewDecoder(conn)
	for {
		var resp struct {
			Error string          `json:"error"`
			Data  json.RawMessage `json:"data"`
		}
		if err := dec.Decode(&resp); err != nil {
			return nil, fmt.Errorf("读取 mpv 响应失败: %w", err)
		}
		// mpv 可以先推送加载或跳转事件，命令响应由 error 字段标识。
		if resp.Error == "" {
			continue
		}
		if resp.Error != "success" {
			return nil, fmt.Errorf("mpv 命令失败: %s", resp.Error)
		}
		return resp.Data, nil
	}
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

package ipc

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// dialTimeout 是连接后端的超时时间。
const dialTimeout = 2 * time.Second

// ErrBusy 表示后端已有前端连接。
var ErrBusy = errors.New("已有运行中的 molpe 界面")

// Client 是前端到后端的 IPC 连接。事件推送经 Events 通道流出；
// Request 用于带响应的数据请求；Send 用于无需响应的命令。
type Client struct {
	conn net.Conn
	r    *bufio.Reader // 与握手共用同一读缓冲，避免缓冲数据丢失

	mu      sync.Mutex // 保护写端与 pending
	nextID  int
	pending map[int]chan Message

	events chan Message
	gone   chan struct{} // 连接断开时关闭
	once   sync.Once
}

// Dial 连接后端并完成握手：后端以 State 快照应答，已有前端连接时返回 ErrBusy。
func Dial(socket string) (*Client, State, error) {
	var st State
	conn, err := net.DialTimeout("unix", socket, dialTimeout)
	if err != nil {
		return nil, st, err
	}
	c := &Client{
		conn:    conn,
		r:       bufio.NewReader(conn),
		pending: make(map[int]chan Message),
		events:  make(chan Message, 64),
		gone:    make(chan struct{}),
	}
	// 握手：后端在连接建立后立即推送首份状态快照；此处同步读取，
	// 握手完成后再启动读循环，避免首份快照误入事件通道。
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	msg, err := Decode(c.r)
	if err != nil {
		conn.Close()
		return nil, st, fmt.Errorf("等待后端应答失败: %w", err)
	}
	if msg.Type == TBusy {
		conn.Close()
		return nil, st, ErrBusy
	}
	if msg.Type != TState {
		conn.Close()
		return nil, st, fmt.Errorf("后端应答异常: %s", msg.Type)
	}
	if err := msg.DecodeData(&st); err != nil {
		conn.Close()
		return nil, st, err
	}
	_ = conn.SetReadDeadline(time.Time{})
	go c.readLoop()
	return c, st, nil
}

// Events 返回服务端事件推送通道（状态快照、提示、歌词）。
func (c *Client) Events() <-chan Message { return c.events }

// Gone 返回连接断开通知通道（断开时关闭）。
func (c *Client) Gone() <-chan struct{} { return c.gone }

// readLoop 持续读取服务端消息：带 ID 的投递给对应请求方，其余进事件通道；
// 读取出错（对端关闭等）时关闭 gone 通道并结束。
func (c *Client) readLoop() {
	for {
		msg, err := Decode(c.r)
		if err != nil {
			c.once.Do(func() { close(c.gone) })
			return
		}
		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		if ok {
			delete(c.pending, msg.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- msg
			continue
		}
		c.events <- msg
	}
}

// Send 发送无需响应的命令。
func (c *Client) Send(t Type, payload any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Encode(c.conn, NewMessage(0, t, payload))
}

// Request 发送带 ID 的请求并阻塞等待响应；连接断开时返回错误。
func (c *Client) Request(t Type, payload any) (Message, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan Message, 1)
	c.pending[id] = ch
	err := Encode(c.conn, NewMessage(id, t, payload))
	c.mu.Unlock()
	if err != nil {
		return Message{}, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-c.gone:
		return Message{}, errors.New("与后端的连接已断开")
	}
}

// Close 关闭连接。
func (c *Client) Close() {
	_ = c.conn.Close()
	c.once.Do(func() { close(c.gone) })
}

package ui

import (
	"bytes"
	"fmt"
	"image"
	"math/rand/v2"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"
)

const (
	graphicsQueryID   = 0x7fffffff
	graphicsFeatureID = 0x7ffffffe
)

// graphicsModel 只管理前端自己的 Kitty 图片，图片确认可用前继续显示色块。
// 图片 ID 的高字节划分本次进程的命名空间，低 24 位单调递增以识别迟到响应。
type graphicsModel struct {
	allowed, queryOK, unicodeOK, failed bool
	profile                             colorprofile.Profile
	prefix, serial                      int
	id                                  int
	url                                 string
	width, height                       int
	ready                               bool
}

type graphicsEncodedMsg struct {
	id   int
	data string
	err  error
}
type graphicsTimeoutMsg struct{ id int }
type graphicsProbeTimeoutMsg struct{}

func newGraphicsModel() graphicsModel {
	term := os.Getenv("TERM")
	multiplexed := os.Getenv("TMUX") != "" || os.Getenv("STY") != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
	allowed := !multiplexed && os.Getenv("MOLPE_COVER_MODE") != "blocks" && os.Getenv("TERM_PROGRAM") != "Apple_Terminal"
	return graphicsModel{allowed: allowed, prefix: (1 + rand.IntN(120)) << 24}
}

func (g graphicsModel) init() tea.Cmd {
	if !g.allowed {
		return nil
	}
	// 无文件传输的实际解码查询 + 占位符能力查询 + 实际终端版本查询。
	// 旧 Kitty 尚无 e= 查询时，版本响应补足 0.28 引入的占位符能力判断。
	query := fmt.Sprintf("\x1b_Ga=q,i=%d,s=1,v=1,t=d,f=24;AAAA\x1b\\\x1b_Ga=q,i=%d,e=T,U=1\x1b\\\x1b[>q", graphicsQueryID, graphicsFeatureID)
	return tea.Batch(tea.Raw(query), tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return graphicsProbeTimeoutMsg{} }))
}

var graphicsVersion = regexp.MustCompile(`^(kitty\(|ghostty[ (])([0-9]+)\.([0-9]+)(?:[.) -]|$)`)

func supportsUnicodeImages(name string) bool {
	parts := graphicsVersion.FindStringSubmatch(strings.ToLower(name))
	if len(parts) == 0 {
		return false
	}
	major, _ := strconv.Atoi(parts[2])
	minor, _ := strconv.Atoi(parts[3])
	if strings.HasPrefix(parts[1], "kitty") {
		return major > 0 || minor >= 28
	}
	return major >= 1
}

func (g graphicsModel) supported() bool {
	return g.allowed && g.queryOK && g.unicodeOK && !g.failed && g.profile == colorprofile.TrueColor
}

func (g graphicsModel) owns(id int) bool {
	return id > g.prefix && id <= g.prefix+g.serial
}

func deleteGraphics(id int) tea.Cmd {
	return tea.Raw(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id))
}

func (g *graphicsModel) clear() tea.Cmd {
	if g.id == 0 {
		return nil
	}
	id := g.id
	g.id, g.ready = 0, false
	g.url = ""
	return deleteGraphics(id)
}

func (m *Model) quitGraphics() tea.Cmd {
	m.graphics.allowed = false
	return tea.Sequence(m.graphics.clear(), tea.Quit)
}

// syncGraphics 在每次状态转移后运行。传输与解码都在命令中执行，普通重绘不重复传图。
func (m *Model) syncGraphics() tea.Cmd {
	g := &m.graphics
	layout := playingLayout(m.width, m.playerHeight(), m.lyricLines)
	visible := m.page == pagePlaying && !m.popup.open && !m.picker.open && !m.quality.open && m.cover.image != nil && layout.coverWidth > 0
	if !g.supported() || !visible {
		return g.clear()
	}
	if g.id != 0 && g.url == m.cover.url && g.width == layout.coverWidth && g.height == layout.coverHeight {
		return nil
	}
	cleanup := g.clear()
	g.serial++
	// 长时间运行耗尽本次命名空间时保守回退，避免覆盖其他应用的图片。
	if g.serial >= 1<<24 {
		g.failed = true
		return cleanup
	}
	g.id = g.prefix + g.serial
	g.url, g.width, g.height = m.cover.url, layout.coverWidth, layout.coverHeight
	id, img, width, height := g.id, m.cover.image, g.width, g.height
	encode := func() tea.Msg {
		data, err := encodeGraphics(img, id, width, height)
		return graphicsEncodedMsg{id: id, data: data, err: err}
	}
	return tea.Batch(cleanup, encode)
}

func encodeGraphics(img image.Image, id, width, height int) (string, error) {
	var out bytes.Buffer
	err := kitty.EncodeGraphics(&out, img, &kitty.Options{
		Action: kitty.TransmitAndPut, Transmission: kitty.Direct, Format: kitty.PNG, Chunk: true,
		ID: id, PlacementID: 1, Columns: width, Rows: height, VirtualPlacement: true, DoNotMoveCursor: true,
	})
	return out.String(), err
}

func (m Model) handleGraphics(msg tea.Msg) (tea.Model, tea.Cmd) {
	g := &m.graphics
	switch msg := msg.(type) {
	case tea.ColorProfileMsg:
		g.profile = msg.Profile
	case tea.TerminalVersionMsg:
		if g.allowed {
			g.unicodeOK = g.unicodeOK || supportsUnicodeImages(msg.Name)
		}
	case graphicsProbeTimeoutMsg:
		if !g.queryOK || !g.unicodeOK {
			g.allowed = false
		}
	case uv.KittyGraphicsEvent:
		id, ok := msg.Options.ID, string(msg.Payload) == "OK"
		switch id {
		case graphicsQueryID:
			if g.allowed {
				g.queryOK = ok
			}
		case graphicsFeatureID:
			if g.allowed && ok && msg.Options.VirtualPlacement {
				g.unicodeOK = true
			}
		default:
			if !g.owns(id) {
				return m, nil
			}
			if id != g.id {
				return m, deleteGraphics(id)
			}
			if !ok {
				g.failed = true
				cmd := g.clear()
				return m, cmd
			}
			g.ready = true
		}
	case graphicsEncodedMsg:
		if msg.id != g.id {
			return m, nil
		}
		if msg.err != nil {
			g.failed = true
			cmd := g.clear()
			return m, cmd
		}
		return m, tea.Batch(tea.Raw(msg.data), tea.Tick(2*time.Second, func(time.Time) tea.Msg { return graphicsTimeoutMsg{id: msg.id} }))
	case graphicsTimeoutMsg:
		if msg.id == g.id && !g.ready {
			g.failed = true
			cmd := g.clear()
			return m, cmd
		}
	}
	return m, nil
}

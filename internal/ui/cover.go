package ui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

const maxCoverBytes = 8 << 20

type coverCache struct {
	mu     sync.Mutex
	images map[string]image.Image
	order  []string
}

func (c *coverCache) load(ctx context.Context, address string) (image.Image, error) {
	c.mu.Lock()
	img := c.images[address]
	c.mu.Unlock()
	if img != nil {
		return img, nil
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("无效封面地址")
	}
	if u.Scheme == "http" && strings.HasSuffix(u.Hostname(), ".music.126.net") {
		u.Scheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://music.163.com/")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxCoverBytes {
		return nil, errors.New("封面响应不可用")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCoverBytes {
		return nil, errors.New("封面文件过大")
	}
	img, err = decodeCover(data)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.images == nil {
		c.images = make(map[string]image.Image)
	}
	if c.images[address] == nil {
		if len(c.order) >= 8 {
			delete(c.images, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, address)
	}
	c.images[address] = img
	return img, nil
}

func decodeCover(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return nil, errors.New("封面尺寸不可用")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	x, y := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	return resizeCover(img, image.Rect(x, y, x+side, y+side), min(1024, side), min(1024, side)), nil
}

// resizeCover 按像素区域平均颜色；只在加载封面或改变显示尺寸时调用。
func resizeCover(src image.Image, bounds image.Rectangle, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		y0 := bounds.Min.Y + y*bounds.Dy()/height
		y1 := min(bounds.Max.Y, max(y0+1, bounds.Min.Y+(y+1)*bounds.Dy()/height))
		for x := range width {
			x0 := bounds.Min.X + x*bounds.Dx()/width
			x1 := min(bounds.Max.X, max(x0+1, bounds.Min.X+(x+1)*bounds.Dx()/width))
			var red, green, blue, alpha uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := src.At(sx, sy).RGBA()
					red += uint64(r)
					green += uint64(g)
					blue += uint64(b)
					alpha += uint64(a)
				}
			}
			n := uint64((x1 - x0) * (y1 - y0))
			dst.SetRGBA(x, y, color.RGBA{uint8((red / n) >> 8), uint8((green / n) >> 8), uint8((blue / n) >> 8), uint8((alpha / n) >> 8)})
		}
	}
	return dst
}

type coverModel struct {
	url             string
	image           image.Image
	art             string
	width, height   int
	background      string
	loading, failed bool
	seq             int
	cancel          context.CancelFunc
	cache           *coverCache
}

type coverLoadedMsg struct {
	url   string
	seq   int
	image image.Image
	err   error
}

func (m *Model) syncCover() tea.Cmd {
	address := ""
	if m.st.Playing != nil {
		address = m.st.Playing.Song.CoverURL
	}
	if address != m.cover.url {
		if m.cover.cancel != nil {
			m.cover.cancel()
		}
		m.cover.seq++
		m.cover.url, m.cover.image, m.cover.art = address, nil, ""
		m.cover.loading, m.cover.failed = false, false
	}
	if m.page != pagePlaying {
		return nil
	}
	m.refreshCoverArt()
	if address == "" || m.cover.image != nil || m.cover.loading || m.cover.failed {
		return nil
	}
	if m.cover.cache == nil {
		m.cover.cache = &coverCache{}
	}
	cache, seq := m.cover.cache, m.cover.seq
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	m.cover.cancel, m.cover.loading = cancel, true
	return func() tea.Msg {
		defer cancel()
		img, err := cache.load(ctx, address)
		return coverLoadedMsg{url: address, seq: seq, image: img, err: err}
	}
}

func (m *Model) refreshCoverArt() {
	layout := playingLayout(m.width, m.playerHeight(), m.lyricLines)
	if layout.coverWidth == 0 || m.cover.image == nil {
		m.cover.art = ""
		return
	}
	if m.cover.art != "" && m.cover.width == layout.coverWidth && m.cover.height == layout.coverHeight && m.cover.background == m.theme.Colors.Background {
		return
	}
	m.cover.width, m.cover.height = layout.coverWidth, layout.coverHeight
	m.cover.background = m.theme.Colors.Background
	m.cover.art = renderCoverImage(m.cover.image, layout.coverWidth, layout.coverHeight, m.theme.BackgroundColor())
}

func (m Model) playerHeight() int {
	return max(1, m.height-chromeFixed-m.lyricGapBelow-m.playbackGapBelow)
}

func (m *Model) openPlayer() tea.Cmd {
	if m.page != pagePlaying {
		m.playerReturn = m.page
		m.page = pagePlaying
		m.syncPlayerList(m.st.Queue.Songs, true)
	}
	return m.syncCover()
}

func (m *Model) closePlayer() {
	m.page = m.playerReturn
	if m.page != pageSongs && m.page != pagePlaylists {
		m.page = pagePlaylists
	}
}

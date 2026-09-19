package netease

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-musicfox/netease-music/service"
)

// LyricLine 是一句带时间戳的歌词。
type LyricLine struct {
	Time float64 // 出现时间（秒）
	Text string
}

// Lyrics 获取歌曲歌词。includeTranslation 为 true 时，翻译按相同时间戳
// 合并到对应歌词行之后（“原文 译文”）；时间戳对不上的翻译行直接丢弃。
func (c *Client) Lyrics(id int64, includeTranslation bool) ([]LyricLine, error) {
	svc := &service.LyricService{ID: strconv.FormatInt(id, 10)}
	code, body := svc.Lyric()
	if code != 200 {
		return nil, fmt.Errorf("获取歌曲 %d 歌词失败: code=%v", id, code)
	}
	var resp struct {
		LRC struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		TLyric struct {
			Lyric string `json:"lyric"`
		} `json:"tlyric"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析歌曲 %d 歌词失败: %w", id, err)
	}
	lines := parseLRC(resp.LRC.Lyric)
	if includeTranslation && resp.TLyric.Lyric != "" {
		mergeTranslation(lines, resp.TLyric.Lyric)
	}
	return lines, nil
}

// lrcTimeRE 匹配 LRC 时间戳，如 [01:23.45]。
var lrcTimeRE = regexp.MustCompile(`\[(\d+):(\d+(?:\.\d+)?)\]`)

// parseLRC 解析 LRC 文本为按时间排序的歌词行；跳过元数据标签与空文本行。
func parseLRC(lrc string) []LyricLine {
	var lines []LyricLine
	for _, raw := range strings.Split(lrc, "\n") {
		matches := lrcTimeRE.FindAllStringSubmatchIndex(raw, -1)
		if len(matches) == 0 {
			continue
		}
		// 文本位于最后一个时间戳之后；一行多个时间戳时各生成一行。
		text := strings.TrimSpace(raw[matches[len(matches)-1][1]:])
		if text == "" {
			continue
		}
		for _, mt := range matches {
			min, _ := strconv.Atoi(raw[mt[2]:mt[3]])
			sec, _ := strconv.ParseFloat(raw[mt[4]:mt[5]], 64)
			lines = append(lines, LyricLine{Time: float64(min)*60 + sec, Text: text})
		}
	}
	slices.SortFunc(lines, func(a, b LyricLine) int { return cmp.Compare(a.Time, b.Time) })
	return lines
}

// mergeTranslation 将翻译歌词按相同时间戳合并进原文行（原文在前）。
func mergeTranslation(lines []LyricLine, tlyric string) {
	byTime := make(map[float64]int, len(lines))
	for i, l := range lines {
		if _, ok := byTime[l.Time]; !ok {
			byTime[l.Time] = i
		}
	}
	for _, t := range parseLRC(tlyric) {
		if i, ok := byTime[t.Time]; ok {
			lines[i].Text += " " + t.Text
		}
	}
}

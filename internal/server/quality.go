package server

import (
	"slices"
	"time"

	"molpe/internal/config"
	"molpe/internal/netease"
)

// setQuality 先保存偏好，再按原进度和暂停意图重新取流；队列与历史不变。
func (s *Server) setQuality(quality string) {
	if !slices.Contains(netease.QualityLevels(), quality) {
		s.pushNote("不支持的音质选项", true)
		return
	}
	if quality == s.quality {
		return
	}
	if s.cfgErr != nil {
		s.pushNote("配置文件存在错误，请先修复后再保存音质", true)
		return
	}
	cfg := s.cfg
	cfg.Quality = quality
	if err := config.SaveConfig(s.dirs.Config, cfg); err != nil {
		s.pushNote("保存音质失败："+err.Error(), true)
		return
	}
	s.cfg, s.quality = cfg, quality
	if s.playing == nil || s.gapSong != nil {
		s.pushState()
		s.pushNote("音质偏好已保存", false)
		return
	}
	pos, _ := s.anchor.get()
	offset := max(0, time.Duration(pos*float64(time.Second)))
	// 快速连续切换时，沿用仍在取流的定位，避免回到曲首。
	if s.loading && s.startOffsetSeq == s.playSeq {
		offset = s.startOffset
	}
	if s.playing.Song.Duration > 0 {
		offset = clampDur(offset, 0, s.playing.Song.Duration)
	}
	song := s.playing.Song
	s.cancelRetry()
	s.playSong(song)
	s.startOffset, s.startOffsetSeq = offset, s.playSeq
	s.pushNote("音质已切换，正在重新加载当前歌曲", false)
}

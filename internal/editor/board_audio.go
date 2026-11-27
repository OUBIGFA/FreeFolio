package editor

import (
	"slices"
	"strconv"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Local media exposes only controls that its workflow actually consumes.
func (b *Board) aiLocalMediaParams(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, inline, disabled bool) {
	set := func(fn func(*whiteboard.AIParams)) {
		if disabled {
			return
		}
		b.updateAINode(o, id, func(n *whiteboard.AINode) { fn(&n.Params) })
	}
	choice := func(key, label, current string, values, labels []string) string {
		if inline {
			v, _ := aiInlineSelect(c, id+key, i18n.T(label), current, values, labels, disabled)
			return v
		}
		return aiParamChoice(c, o, key, label, current, values, labels, disabled)
	}
	p := n.Params
	if !inline {
		defer b.aiComfySamplingParams(c, o, id, n, disabled)
	}
	if n.Model == "qwen3-tts" {
		speaker := p.Speaker
		if speaker == "" {
			speaker = "Vivian"
		}
		voices := []string{"Vivian", "Serena", "Uncle_Fu", "Dylan", "Eric", "Ryan", "Aiden", "Ono_Anna", "Sohee"}
		if v := choice("speaker", "音色", speaker, voices, voices); v != speaker {
			set(func(p *whiteboard.AIParams) { p.Speaker = v })
		}
		if !inline {
			language := p.Language
			if language == "" {
				language = "Chinese"
			}
			langs := []string{"Chinese", "English", "Auto", "Japanese", "Korean", "French", "German", "Spanish", "Portuguese", "Russian", "Italian"}
			labels := translated("中文", "英语", "自动", "日语", "韩语", "法语", "德语", "西班牙语", "葡萄牙语", "俄语", "意大利语")
			if v := choice("language", "语言", language, langs, labels); v != language {
				set(func(p *whiteboard.AIParams) { p.Language = v })
			}
			ui.Text(c, i18n.T("语气与情绪")).FontSize(12).TextColor(o.Theme.TextMuted)
			if ui.TextArea(c.Key("voice-style"), &b.ai.buf.voiceStyle).WidthPercent(100).Height(84).Disabled(disabled).Label(i18n.T("语气与情绪")).Placeholder(i18n.T("例如：轻快、热情，语速稍快")).Changed() {
				set(func(p *whiteboard.AIParams) { p.VoiceStyle = b.ai.buf.voiceStyle })
			}
			ui.Text(c, i18n.T("在指令框填写朗读内容，每次最多 1000 字")).FontSize(12).TextColor(o.Theme.TextMuted)
		}
		return
	}
	values, labels := []string{"0", "5", "10", "30", "60"}, []string{i18n.T("默认 10 秒"), "5s", "10s", "30s", "60s"}
	if n.Model == "ace15-turbo" {
		values, labels = []string{"0", "10", "15", "20", "30", "60", "120"}, []string{i18n.T("默认 30 秒"), "10s", "15s", "20s", "30s", "60s", "120s"}
	} else if n.Role == whiteboard.AIVideo {
		values, labels = []string{"0", "1", "2", "3", "4", "5"}, []string{i18n.T("默认 4 秒"), "1s", "2s", "3s", "4s", "5s"}
	}
	if inline {
		if p.Seconds > 0 && !slices.Contains(values, strconv.Itoa(p.Seconds)) {
			values = append(values, strconv.Itoa(p.Seconds))
			labels = append(labels, strconv.Itoa(p.Seconds)+"s")
		}
		if v := choice("seconds", "时长", strconv.Itoa(p.Seconds), values, labels); v != strconv.Itoa(p.Seconds) {
			k, _ := strconv.Atoi(v)
			set(func(p *whiteboard.AIParams) { p.Seconds = k })
		}
		return
	}
	seconds, lo, hi := p.Seconds, 1, 60
	if n.Model == "ace15-turbo" {
		lo, hi = 5, 120
		if seconds == 0 {
			seconds = 30
		}
	} else if n.Role == whiteboard.AIVideo {
		hi = 5
		if seconds == 0 {
			seconds = 4
		}
	} else if seconds == 0 {
		seconds = 10
	}
	aiParamNumber(c, "seconds", "时长（秒）", float64(seconds), float64(lo), float64(hi), 1, disabled, func(v float64) { set(func(p *whiteboard.AIParams) { p.Seconds = int(v) }) })
	if n.Model == "ace15-turbo" {
		bpm := p.BPM
		if bpm == 0 {
			bpm = 120
		}
		aiParamNumber(c, "bpm", "速度（BPM）", float64(bpm), 10, 300, 1, disabled, func(v float64) { set(func(p *whiteboard.AIParams) { p.BPM = int(v) }) })
		key := p.KeyScale
		if key == "" {
			key = "C major"
		}
		var keys, keyLabels []string
		for _, mode := range []string{"major", "minor"} {
			for _, note := range []string{"C", "C#", "Db", "D", "D#", "Eb", "E", "F", "F#", "Gb", "G", "G#", "Ab", "A", "A#", "Bb", "B"} {
				keys = append(keys, note+" "+mode)
				label := i18n.T("大调")
				if mode == "minor" {
					label = i18n.T("小调")
				}
				keyLabels = append(keyLabels, note+" "+label)
			}
		}
		if v := choice("musical-key", "调性", key, keys, keyLabels); v != key {
			set(func(p *whiteboard.AIParams) { p.KeyScale = v })
		}
		meter := p.TimeSignature
		if meter == "" {
			meter = "4"
		}
		if v := choice("meter", "拍号", meter, []string{"2", "3", "4", "6"}, []string{"2/4", "3/4", "4/4", "6/8"}); v != meter {
			set(func(p *whiteboard.AIParams) { p.TimeSignature = v })
		}
		language := p.Language
		if language == "" {
			language = "zh"
		}
		if v := choice("language", "歌词语言", language, []string{"zh", "en", "ja", "ko", "yue", "unknown"}, translated("中文", "英语", "日语", "韩语", "粤语", "未指定")); v != language {
			set(func(p *whiteboard.AIParams) { p.Language = v })
		}
		ui.Text(c, i18n.T("歌词，留空生成纯音乐")).FontSize(12).TextColor(o.Theme.TextMuted)
		linked := n.LinkAt("lyrics") != ""
		if ui.TextArea(c.Key("audio-lyrics"), &b.ai.buf.lyrics).WidthPercent(100).Height(140).Disabled(disabled || linked).Label(i18n.T("歌词")).Changed() {
			set(func(p *whiteboard.AIParams) { p.Lyrics = b.ai.buf.lyrics })
		}
	} else if n.Role == whiteboard.AIVideo {
		size := p.Size
		if size == "" {
			size = "832x480"
		}
		if v := choice("video-size", "视频尺寸", size, []string{"832x480", "480x832", "640x640", "1280x704", "704x1280"}, translated("832×480 横向", "480×832 竖向", "640×640 方形", "1280×704 横向", "704×1280 竖向")); v != size {
			set(func(p *whiteboard.AIParams) { p.Size = v })
		}
		fps := p.FPS
		if fps == 0 {
			fps = 24
		}
		if v := choice("fps", "帧率", strconv.Itoa(fps), []string{"12", "16", "24", "30"}, []string{"12", "16", "24", "30"}); v != strconv.Itoa(fps) {
			k, _ := strconv.Atoi(v)
			set(func(p *whiteboard.AIParams) { p.FPS = k })
		}
		ui.Text(c, i18n.T("尺寸、帧率越高，占用显存越多，可连接一张首帧图片")).FontSize(12).TextColor(o.Theme.TextMuted)
	}
}

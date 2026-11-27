package editor

import (
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
		if disabled {
			shown := current
			for i, v := range values {
				if v == current {
					shown = labels[i]
				}
			}
			ui.Text(c, i18n.T(label)).FontSize(12).TextColor(o.Theme.TextMuted)
			ui.Select(c.Key("board-ai-"+key), &shown, labels).WidthPercent(100).FontSize(13).Disabled(true).Label(i18n.T(label))
			return current
		}
		return aiChoice(c, o, key, label, current, values, labels)
	}
	p := n.Params
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
			ui.Text(c, i18n.T("在指令框填写朗读内容，每次最多 1000 字")).FontSize(12).TextColor(o.Theme.TextMuted)
		}
		return
	}
	values, labels := []string{"0", "5", "10", "30", "60"}, []string{i18n.T("默认 10 秒"), "5s", "10s", "30s", "60s"}
	if n.Model == "ace15-turbo" {
		values, labels = []string{"0", "10", "30", "60", "120"}, []string{i18n.T("默认 30 秒"), "10s", "30s", "60s", "120s"}
	} else if n.Role == whiteboard.AIVideo {
		values, labels = []string{"0", "1", "2", "3", "4", "5"}, []string{i18n.T("默认 4 秒"), "1s", "2s", "3s", "4s", "5s"}
	}
	if v := choice("seconds", "时长", strconv.Itoa(p.Seconds), values, labels); v != strconv.Itoa(p.Seconds) {
		k, _ := strconv.Atoi(v)
		set(func(p *whiteboard.AIParams) { p.Seconds = k })
	}
	if inline {
		return
	}
	if n.Model == "ace15-turbo" {
		language := p.Language
		if language == "" {
			language = "zh"
		}
		if v := choice("language", "歌词语言", language, []string{"zh", "en", "ja", "ko", "yue", "unknown"}, translated("中文", "英语", "日语", "韩语", "粤语", "纯音乐")); v != language {
			set(func(p *whiteboard.AIParams) { p.Language = v })
		}
		ui.Text(c, i18n.T("歌词，留空生成纯音乐")).FontSize(12).TextColor(o.Theme.TextMuted)
		linked := n.LinkAt("lyrics") != ""
		if ui.TextArea(c.Key("audio-lyrics"), &b.ai.buf.lyrics).WidthPercent(100).Height(140).Disabled(disabled || linked).Label(i18n.T("歌词")).Changed() {
			set(func(p *whiteboard.AIParams) { p.Lyrics = b.ai.buf.lyrics })
		}
	} else if n.Role == whiteboard.AIVideo {
		ui.Text(c, i18n.T("832×480 · 24 帧/秒，可连接一张首帧图片")).FontSize(12).TextColor(o.Theme.TextMuted)
	}
}

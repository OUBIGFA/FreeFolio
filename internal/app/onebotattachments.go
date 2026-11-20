package app

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo"
)

func oneBotAttachment(path string) (agent.ContentPart, error) {
	info, err := os.Stat(path)
	if err != nil {
		return agent.ContentPart{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 20<<20 {
		return agent.ContentPart{}, fmt.Errorf("%s: attachment must be a regular file smaller than 20 MB", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return agent.ContentPart{}, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	part := agent.ContentPart{Name: filepath.Base(path), MIME: mime.TypeByExtension(ext)}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		part.Type = "image"
	case ".pdf":
		part.Type = "file"
		part.MIME = "application/pdf"
	case ".wav", ".mp3":
		part.Type = "audio"
	case ".mp4", ".webm":
		part.Type = "video"
	default:
		if len(data) > 2<<20 || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return part, fmt.Errorf("%s: unsupported attachment format", part.Name)
		}
		part.Type = "text"
		part.Text = "Attached file: " + part.Name + "\n" + string(data)
		return part, nil
	}
	if part.MIME == "" {
		return part, fmt.Errorf("%s: unknown media type", part.Name)
	}
	part.Data = "data:" + part.MIME + ";base64," + base64.StdEncoding.EncodeToString(data)
	return part, nil
}

func (a *App) pickOneBotAttachments() {
	done := func(paths []string) {
		for _, path := range paths {
			if len(a.qa.attachments) >= 5 {
				a.notify("%s", i18n.T("一次最多添加五个附件"))
				return
			}
			part, err := oneBotAttachment(path)
			if err != nil {
				a.notify("%s", err.Error())
				continue
			}
			a.qa.attachments = append(a.qa.attachments, part)
		}
	}
	if a.pickFiles != nil {
		a.pickFiles(done)
		return
	}
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: i18n.T("添加参考附件"), Multiple: true})
		if err != nil {
			return
		}
		var parts []agent.ContentPart
		var failures []string
		for _, path := range paths {
			if len(parts) >= 5 {
				break
			}
			part, err := oneBotAttachment(path)
			if err != nil {
				failures = append(failures, err.Error())
			} else {
				parts = append(parts, part)
			}
		}
		win.Update(func() {
			for _, part := range parts {
				if len(a.qa.attachments) >= 5 {
					break
				}
				a.qa.attachments = append(a.qa.attachments, part)
			}
			if len(failures) > 0 {
				a.notify("%s", strings.Join(failures, "\n"))
			}
		})
	}()
}

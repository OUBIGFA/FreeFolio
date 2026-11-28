// Package gitsync manages explicitly selected Git repository spaces.
package gitsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

type Binding struct{ Remote, URL, Branch, Root string }
type SpaceConfig struct {
	Enabled, Automatic                                         bool
	Account                                                    string
	Binding                                                    Binding
	Mode, Plan, DailyTime                                      string
	IntervalMinutes, DailyLimit, RetryLimit                    int
	OnStart, OnSave                                            bool
	SaveDelaySeconds                                           int
	MessageMode, Template, Connection, Model, Language, Prompt string
	TemplateOnFailure                                          bool
}
type Config struct {
	Version int
	Spaces  map[string]*SpaceConfig
}

func DefaultSpace() *SpaceConfig {
	return &SpaceConfig{Mode: "both", Plan: "interval", DailyTime: "21:00", IntervalMinutes: 30, RetryLimit: 3, SaveDelaySeconds: 30, MessageMode: "template", Template: "Update notes {date}", Language: "中文"}
}
func (c SpaceConfig) Validate() error {
	if c.Mode != "both" && c.Mode != "pull" && c.Mode != "push" {
		return errors.New(i18n.T("请选择同步动作"))
	}
	if c.Plan != "interval" && c.Plan != "daily" {
		return errors.New(i18n.T("请选择同步计划"))
	}
	if _, e := time.Parse("15:04", c.DailyTime); e != nil {
		return errors.New(i18n.T("每天同步时间应为 HH:MM"))
	}
	if c.IntervalMinutes < 1 || c.IntervalMinutes > 1440 || c.DailyLimit < 0 || c.DailyLimit > 1440 || c.RetryLimit < 0 || c.RetryLimit > 10 || c.SaveDelaySeconds < 0 || c.SaveDelaySeconds > 3600 {
		return errors.New(i18n.T("同步间隔、保存延迟或次数无效"))
	}
	if c.MessageMode != "manual" && c.MessageMode != "template" && c.MessageMode != "ai" {
		return errors.New(i18n.T("请选择提交说明方式"))
	}
	if c.Automatic && c.MessageMode == "manual" && c.Mode != "pull" {
		return errors.New(i18n.T("自动提交需要固定模板或 OneBot 说明"))
	}
	if strings.TrimSpace(c.Template) == "" {
		return errors.New(i18n.T("请填写提交说明模板"))
	}
	if c.MessageMode == "ai" && (c.Connection == "" || c.Model == "") {
		return errors.New(i18n.T("请选择生成提交说明的 OneBot 模型"))
	}
	if c.Enabled && (c.Binding.Remote == "" || c.Binding.Branch == "" || c.Binding.URL == "") {
		return errors.New(i18n.T("请先检查并绑定仓库"))
	}
	return nil
}
func LoadConfig(dir string) (Config, error) {
	c := Config{Version: 1, Spaces: map[string]*SpaceConfig{}}
	b, e := os.ReadFile(filepath.Join(dir, "git-sync.json"))
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	if c.Version != 1 {
		return c, errors.New(i18n.T("不支持的 Git 同步设置版本"))
	}
	if c.Spaces == nil {
		c.Spaces = map[string]*SpaceConfig{}
	}
	for _, s := range c.Spaces {
		if s == nil {
			return c, errors.New(i18n.T("Git 同步空间设置无效"))
		}
		if e = s.Validate(); e != nil {
			return c, e
		}
	}
	return c, nil
}
func SaveConfig(dir string, c Config) error {
	c.Version = 1
	for _, s := range c.Spaces {
		if s == nil {
			return errors.New(i18n.T("Git 同步空间设置无效"))
		}
		if e := s.Validate(); e != nil {
			return e
		}
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, "git-sync.json"), append(b, '\n'), 0600)
}
func (c SpaceConfig) next(now time.Time) time.Time {
	if c.Plan == "daily" {
		clock, _ := time.Parse("15:04", c.DailyTime)
		n := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if !n.After(now) {
			n = n.AddDate(0, 0, 1)
		}
		return n
	}
	return now.Add(time.Duration(c.IntervalMinutes) * time.Minute)
}

// Schedule consumes startup only once and discards pending jobs on configuration
// changes. The UI supplies its clock; no background timers or missed-job replay.
type Schedule struct {
	planNext       time.Time
	Next           time.Time
	config         SpaceConfig
	day            string
	count, retries int
	paused         bool
}

func (s *Schedule) Configure(c *SpaceConfig, now time.Time, startup bool) {
	s.config = *c
	s.Next = time.Time{}
	s.retries = 0
	s.paused = false
	if c.Enabled && c.Automatic {
		s.Next = c.next(now)
		s.planNext = s.Next
		if startup && c.OnStart {
			s.Next = now.Add(2 * time.Second)
		}
	}
}
func (s *Schedule) Saved(now time.Time) {
	if s.config.Enabled && s.config.Automatic && s.config.OnSave && !s.paused {
		n := now.Add(time.Duration(s.config.SaveDelaySeconds) * time.Second)
		s.Next = s.planNext
		if s.Next.IsZero() || n.Before(s.Next) {
			s.Next = n
		}
	}
}
func (s *Schedule) Ready(now time.Time) bool {
	if s.day != now.Format("2006-01-02") {
		s.day = now.Format("2006-01-02")
		s.count = 0
	}
	return !s.paused && s.config.Enabled && s.config.Automatic && !s.Next.IsZero() && !now.Before(s.Next) && (s.config.DailyLimit == 0 || s.count < s.config.DailyLimit)
}
func (s *Schedule) Finished(now time.Time, success, retryable bool) {
	if s.day != now.Format("2006-01-02") {
		s.day = now.Format("2006-01-02")
		s.count = 0
	}
	s.count++
	s.Next = s.config.next(now)
	s.planNext = s.Next
	if success {
		s.retries = 0
		return
	}
	if !retryable {
		s.paused = true
		s.Next = time.Time{}
		return
	}
	if s.retries < s.config.RetryLimit {
		s.retries++
		s.Next = now.Add(time.Duration(s.retries) * time.Minute)
	}
}
func Template(c SpaceConfig, now time.Time, count int) string {
	return strings.NewReplacer("{date}", now.Format("2006-01-02 15:04"), "{count}", fmt.Sprint(count)).Replace(c.Template)
}

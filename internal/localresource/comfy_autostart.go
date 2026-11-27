package localresource

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// A service URL identifies a network endpoint, not an executable. Only a local
// endpoint may fall back to a configured or registered local ComfyUI install.
func comfyAutoStartDirectory(base, dir string, env comfyDetectEnv) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("remote ComfyUI must be started on its own computer")
	}
	if dir = strings.TrimSpace(dir); dir != "" {
		return dir, nil
	}
	meta := loadComfyDesktopV2Meta(env.userConfigDir)
	if layout, ok := selectBestDesktopV2Install(meta, func(comfyDesktopV2Install) bool { return true }, env.goos); ok {
		return layout.Root, nil
	}
	legacy := loadComfyLegacyDesktopMeta(env.userConfigDir)
	if legacy.basePath != "" {
		if layout, err := detectComfyLayout(legacy.basePath, env); err == nil {
			return layout.Root, nil
		}
	}
	return "", errors.New("select a local ComfyUI directory in Optional resources to enable automatic startup")
}

package gitsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

const GCMVersion = "3.0.2"

type managerArtifact struct {
	Name, Digest string
	Bytes        int64
}

var managerArtifacts = map[string]managerArtifact{
	"windows/amd64": {"gcm-win-x64-3.0.2.zip", "9d2bef6e42a37654ea7ef33172bbcf1b6a1df6191fcf6a57dfbf39e1cbc27a0f", 20793237},
	"windows/arm64": {"gcm-win-arm64-3.0.2.zip", "c3b815f2b964cea59e6a4c26962e7a5faf60d6053e0e72cf58fd79868ba41c63", 19696904},
	"linux/amd64":   {"gcm-linux-x64-3.0.2.tar.gz", "b8e59e22a830006ca4d93e34513effbb2d9fc0c45d9ccf40395eaab7c4c1b97e", 31823657},
	"linux/arm64":   {"gcm-linux-arm64-3.0.2.tar.gz", "650360154f92bfdd4e836043453410385fb53c06630280aabcb8ece6143a1706", 18358840},
	"darwin/amd64":  {"gcm-osx-x64-3.0.2.tar.gz", "a59672ed02fd5e785a4ff89a70890955a958015867995c1079023d3fbc6643b1", 23195300},
	"darwin/arm64":  {"gcm-osx-arm64-3.0.2.tar.gz", "2031e8f81683ff0bd835a762e6cfd0831e9d28cdd5a55b10135c73e9498750cc", 21776743},
}

func managerName() string {
	if runtime.GOOS == "windows" {
		return "git-credential-manager.exe"
	}
	return "git-credential-manager"
}
func supportedManager(ctx context.Context, path string) bool {
	if path == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	hideCommand(cmd)
	out, e := cmd.Output()
	if e != nil {
		return false
	}
	var major, minor int
	if _, e = fmt.Sscanf(strings.TrimSpace(string(out)), "%d.%d", &major, &minor); e != nil {
		return false
	}
	return major > 2 || major == 2 && minor >= 7
}
func findManager(ctx context.Context, gitPath, resources string) string {
	candidates := []string{}
	if p, e := exec.LookPath(managerName()); e == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, filepath.Join(resources, "git-credential-manager", GCMVersion, managerName()))
	cmd := exec.CommandContext(ctx, gitPath, "--exec-path")
	hideCommand(cmd)
	if out, e := cmd.Output(); e == nil {
		base := strings.TrimSpace(string(out))
		candidates = append(candidates, filepath.Join(base, managerName()), filepath.Join(base, "..", "..", "bin", managerName()))
	}
	candidates = append(candidates, filepath.Join(filepath.Dir(gitPath), managerName()))
	for _, p := range candidates {
		if supportedManager(ctx, p) {
			return p
		}
	}
	return ""
}
func ensureCredentialManager(ctx context.Context, credentials string, progress func(RuntimeProgress)) (*credentialManager, error) {
	resources := filepath.Join(filepath.Dir(credentials), "resources")
	gitPath, e := EnsureGit(ctx, resources, progress)
	if e != nil {
		return nil, e
	}
	managerPath := findManager(ctx, gitPath, resources)
	if managerPath == "" {
		spec, ok := managerArtifacts[runtime.GOOS+"/"+runtime.GOARCH]
		if !ok {
			return nil, errors.New(i18n.T("此平台没有可用的 GitHub 登录组件"))
		}
		parent := filepath.Join(resources, "git-credential-manager")
		if e = os.MkdirAll(parent, 0700); e != nil {
			return nil, e
		}
		lock, e := os.OpenFile(filepath.Join(parent, "download.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		defer lock.Close()
		locked, e := tryLock(lock)
		if e != nil || !locked {
			return nil, errors.New(i18n.T("GitHub 登录组件正在准备，请稍后重试"))
		}
		defer unlock(lock)
		managerPath = findManager(ctx, gitPath, resources)
		if managerPath == "" {
			stage, e := runtimeArchive(ctx, parent, spec.Name, "https://github.com/git-ecosystem/git-credential-manager/releases/download/v"+GCMVersion+"/"+spec.Name, spec.Digest, spec.Bytes, progress)
			if e != nil {
				return nil, e
			}
			candidate := filepath.Join(stage, managerName())
			if !supportedManager(ctx, candidate) {
				return nil, errors.New(i18n.T("GitHub 登录组件无法启动"))
			}
			dest := filepath.Join(parent, GCMVersion)
			if _, e = os.Stat(dest); e == nil {
				if e = os.Rename(dest, dest+".invalid-"+time.Now().UTC().Format("20060102T150405.000000000")); e != nil {
					return nil, e
				}
			}
			if e = os.Rename(stage, dest); e != nil {
				return nil, e
			}
			managerPath = filepath.Join(dest, managerName())
		}
	}
	return &credentialManager{Path: managerPath, GitPath: gitPath, Dir: credentials}, nil
}

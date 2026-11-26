//go:build !windows

package localresource

import "os"

func publishDownload(source, target string) error { return os.Link(source, target) }

package localresource

import (
	"context"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GPUInfo describes a locally detected NVIDIA CUDA GPU and its dedicated VRAM.
type GPUInfo struct {
	Name    string
	VRAMMiB int
}

// VRAMGiB returns the GPU memory size rounded to the nearest GiB.
func (g GPUInfo) VRAMGiB() int {
	if g.VRAMMiB <= 0 {
		return 0
	}
	return int(math.Round(float64(g.VRAMMiB) / 1024.0))
}

// DetectLocalGPU queries nvidia-smi for local NVIDIA GPUs and returns the GPU
// with the largest VRAM. It never returns an error; absence is reported as false.
func DetectLocalGPU(ctx context.Context) (GPUInfo, bool) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return GPUInfo{}, false
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return GPUInfo{}, false
	}
	return parseNvidiaSMIOutput(string(out))
}

func parseNvidiaSMIOutput(raw string) (GPUInfo, bool) {
	var best GPUInfo
	found := false
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		vramStr := strings.TrimSpace(parts[1])
		vram, err := strconv.Atoi(vramStr)
		if err != nil || name == "" || vram <= 0 {
			continue
		}
		if !found || vram > best.VRAMMiB {
			best = GPUInfo{Name: name, VRAMMiB: vram}
			found = true
		}
	}
	return best, found
}

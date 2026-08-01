package gpu

import (
	"os/exec"
	"strconv"
	"strings"
)

// GPUInfo describes a detected graphics adapter.
type GPUInfo struct {
	Vendor string // "nvidia", "amd", "intel", "unknown"
	Name   string
	VRAM   uint64 // MB
}

// DetectGPU probes the system for a discrete GPU. On Windows it first checks
// for the NVIDIA `nvidia-smi` CLI (fast and reliable), then falls back to a
// WMI query via PowerShell that covers AMD/Intel adapters too.
func DetectGPU() GPUInfo {
	if info, ok := detectNVIDIA(); ok {
		return info
	}
	if info, ok := detectWMI(); ok {
		return info
	}
	return GPUInfo{Vendor: "unknown", Name: "Unknown", VRAM: 0}
}

// detectNVIDIA runs `nvidia-smi` and parses the GPU name + VRAM.
func detectNVIDIA() (GPUInfo, bool) {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return GPUInfo{}, false
	}
	out, err := exec.Command(path,
		"--query-gpu=name,total_memory",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return GPUInfo{}, false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return GPUInfo{}, false
	}
	// Take the first GPU only.
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	parts := strings.SplitN(line, ",", 2)
	name := strings.TrimSpace(parts[0])
	vram := uint64(0)
	if len(parts) > 1 {
		if n, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
			vram = n
		}
	}
	if name == "" {
		return GPUInfo{}, false
	}
	return GPUInfo{Vendor: "nvidia", Name: name, VRAM: vram}, true
}

// detectWMI falls back to a PowerShell WMI query for Win32_VideoController,
// which surfaces AMD and Intel adapters that nvidia-smi cannot see.
func detectWMI() (GPUInfo, bool) {
	ps, err := exec.LookPath("powershell")
	if err != nil {
		return GPUInfo{}, false
	}
	query := "Get-CimInstance -ClassName Win32_VideoController | " +
		"Select-Object -First 1 -ExpandProperty Name"
	out, err := exec.Command(ps, "-NoProfile", "-Command", query).Output()
	if err != nil {
		return GPUInfo{}, false
	}
	name := strings.TrimSpace(string(out))
	if name == "" || strings.EqualFold(name, "Standard VGA Graphics Adapter") {
		return GPUInfo{}, false
	}
	vendor := vendorFromName(name)
	return GPUInfo{Vendor: vendor, Name: name, VRAM: 0}, true
}

// vendorFromName heuristically maps a GPU name to a vendor token.
func vendorFromName(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.Contains(l, "nvidia") || strings.Contains(l, "geforce") || strings.Contains(l, "quadro") || strings.Contains(l, "tesla") || strings.Contains(l, "rtx") || strings.Contains(l, "radeon inst") && strings.Contains(l, "nvidia"):
		return "nvidia"
	case strings.Contains(l, "amd") || strings.Contains(l, "radeon") || strings.Contains(l, "firepro") || strings.Contains(l, "firegl"):
		return "amd"
	case strings.Contains(l, "intel"):
		return "intel"
	default:
		return "unknown"
	}
}

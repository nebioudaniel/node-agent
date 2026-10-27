package specs

import (
	"fmt"
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

type Specs struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	CPU      string `json:"cpu"`
	Cores    int    `json:"cores"`
	TotalRAM uint64 `json:"total_ram"`
}

func Collect() (Specs, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return Specs{}, fmt.Errorf("get virtual memory: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return Specs{}, fmt.Errorf("get hostname: %w", err)
	}

	info, err := cpu.Info()
	if err != nil {
		return Specs{}, fmt.Errorf("get CPU info: %w", err)
	}

	if len(info) == 0 {
		return Specs{}, fmt.Errorf("get CPU info: no CPU information available")
	}

	s := Specs{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPU:      info[0].ModelName,
		Cores:    runtime.NumCPU(),
		TotalRAM: v.Total,
	}

	return s, nil
}

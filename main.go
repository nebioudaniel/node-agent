package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func main() {
	v, err := mem.VirtualMemory()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	info, err := cpu.Info()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	if len(info) == 0 {
		fmt.Println("error: no CPU info available")
		return
	}

	specs := Specs{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPU:      info[0].ModelName,
		Cores:    runtime.NumCPU(),
		TotalRAM: v.Total,
	}

	data, err := json.MarshalIndent(specs, "", "  ")
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println(string(data))

	http.HandleFunc("/health", healthHandler)

	fmt.Println("Server running on :8080")
	http.ListenAndServe(":8080", nil)
}

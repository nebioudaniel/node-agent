package main

import (
	"encoding/json"
	"fmt"
	"github.com/shirou/gopsutil/v4/mem"
	"os"
	"runtime"
	"net/http"
)

type Specs struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	CPU      string `json:"cpu"`
	Cores    int    `json:"cores"`
	TotalRAM uint64 `json:"total_ram"`
}
 
 
  func healthHandler(w http.ResponseWriter, r*http.Request){
  w.Header().Set("Content-Type","application/json")
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
	specs := Specs{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
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

package regiist

import (
	"node-agent/internal/api"
	"sync"
	"time"
)
type Worker struct {
	ID string `json:"id"`
	Hostname string `json:"host_name"`
	OS string `json:"os"`
	Arch string `json:"arch"`
	CPUModel int `json:"cpu_model"`
    RAMTotalByte uint64 `json:"ram_total_byte"`
    Lables map[string] string `json:"labeles"`
    SecretHash [32]byte `json:"secret_hash"`
    LastSeen time.Time `json:"last_seen"`
    Last api.Heartbeat `json:"last"`
}
type Worker struct {
	ID string 
	Hostname string 
	OS string 
	Arch string 
	CPUModel int 
    RAMTotalByte uint64 
    Lables map[string] string 
    SecretHash [32]byte 
    LastSeen time.Time 
    Last api.Heartbeat 
    Status string 
}
 type  Registry struct {
  mu sync.RWMutex ;
  workers map[string] map[string]*Worker; 
 }
/*
 * type Worker { ID, Hostname, OS, Arch, CPUModel string; CPUCores int; RAMTotalBytes uint64;
 Labels map[string]string; SecretHash [32]byte; LastSeen time.Time;
 Last api.Heartbeat }
 type View { same as Worker but NO SecretHash, plus Status string }
 type Registry { mu sync.RWMutex; workers map[string]*Worker; now func() time.Time }
 */
func 
package api

import (
	"errors"
	"math"
	"node-agent/internal/specs"
	"time"
)

type RegisterRequest struct {
	JoinToken string `json:"join_token"`
	Specs specs.Specs `joson:"specs"`
}
type RegisterResponse struct{
	WorkerID string `josn:"worker_id"`
	Secret string `json:"secret"`
	HeartbeatSeconds int `json:"heartbeat_seconds"`
}
type Heartbeat struct{
	WorkerID string `json:"worker_id"`
	Seq uint64 `json:"seq"`
	SentAt time.Time `json:"sent_at"`
	AgentVersion string `json:"agent_version"`
	CPUPercent float64 `json:"cpu_percent"`
    RAMTotalBytes uint64 `json:"ram_total_bytes"`
    RAMAvailableBytes uint64 `json:"ram_available_bytes"`
    ActiveTasks int `json:"active_tasks"`
    LoadedModels []string `json:"loaded_models"`
}
 type HeartbeatResponse struct {
   NextHeartbeatSeconds int `json:"next_heartbeat_second"`
   MinAgentVersion string `json:"mini_agent_version"`
 }

 func (h Heartbeat) Validate() error {
  switch {
  case h.WorkerID =="":
  return errors.New("Worker_id is empty")
  case h.RAMTotalBytes == 0:
  return errors.New("ram_totoal_byte must be > 0")
  case h.RAMAvailableBytes > h.RAMTotalBytes:
  return errors.New("ram_availablity_byte exceeds  ram_total_bytes")
  case math.IsNaN(h.CPUPercent)||h.CPUPercent < 0 ||h.CPUPercent > 100:
  return errors.New("cpu_percent must be b/n 0 and  100")
  case h.ActiveTasks < 0:
  return errors.New("active_tasks must be >= 0")
  }
  return  nil
 }
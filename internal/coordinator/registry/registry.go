package regiist


type Worker struct {
	ID string `json:"id"`
	Hostname string `json:"host_name"`
	OS string `json:"os"`
	Arch string `json:"arch"`
	CPUModel int `json:"cpu_model"`
    RAMTotalByte uint64 `json:"ram_total_byte"`
    Lables map[string] string `json:"labeles"`
    
}
package coordinator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"node-agent/internal/api"
)

func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", handlerHeartbeat)
	return mux
}
func handlerHeartbeat(w http.ResponseWriter, r *http.Request) {
	// decode the json body into api.heartbeat , if the json is bad return 400
	var hb api.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	// it call the hb.Validate()if that fails it aslo return 400
	if err := hb.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid heartbeat: %v", err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

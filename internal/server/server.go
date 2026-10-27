package server

import (
	"encoding/json"
		"fmt"
		"net/http"
	
		"node-agent/internal/specs"
)


func NewHandler(s specs.Specs) http.Handler {
 mux := http.NewServeMux()
 mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
 mux.HandleFunc("/specs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err:= json.NewEncoder(w).Encode(s);err!=nil{
			fmt.Printf("encode specs response: %v\n", err)
		}
	})
 return mux
}
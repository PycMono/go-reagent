package main

import (
	"encoding/json"
	"net/http"
)

// pingHandler 返回一个简单的 JSON 响应，用于健康检查
func pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"pong":   "pong",
	})
}

func main() {
	http.HandleFunc("/ping", pingHandler)
	http.ListenAndServe(":8080", nil)
}

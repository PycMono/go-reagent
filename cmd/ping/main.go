// Package main 提供一个简单的 HTTP ping 服务。
package main

import (
	"fmt"
	"net/http"
)

// pingHandler 返回一个简单的 ping 响应。
func pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"message": "pong"}`)
}

// main 启动一个 HTTP 服务，监听 8080 端口并暴露 /ping 接口。
func main() {
	http.HandleFunc("/ping", pingHandler)
	fmt.Println("ping server listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("server error:", err)
	}
}

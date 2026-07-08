package main

import (
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"gateway/internal/middleware"
	"gateway/internal/proxy"
)

func main() {
	// 加载配置
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	elizaAgentURL := os.Getenv("ELIZA_AGENT_URL")
	if elizaAgentURL == "" {
		elizaAgentURL = "http://127.0.0.1:3002"
	}

	aaBridgeURL := os.Getenv("AA_BRIDGE_URL")
	if aaBridgeURL == "" {
		aaBridgeURL = "http://127.0.0.1:3001/aa/settle"
	}

	log.Printf("Starting gateway on port %s", port)
	log.Printf("Eliza Agent URL: %s", elizaAgentURL)
	log.Printf("AA Bridge Settle URL: %s", aaBridgeURL)

	r := chi.NewRouter()

	// 基础中间件
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	// 创建反向代理
	proxyHandler, err := proxy.NewReverseProxy(elizaAgentURL, aaBridgeURL)
	if err != nil {
		log.Fatalf("Failed to initialize reverse proxy: %v", err)
	}

	// 路由注册
	r.Route("/agent", func(r chi.Router) {
		r.Use(middleware.X402Middleware)
		r.Handle("/execute", proxyHandler)
	})

	// 启动服务
	addr := "0.0.0.0:" + port
	log.Printf("Server listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server exited with error: %v", err)
	}
}

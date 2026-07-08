package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"

	"gateway/internal/middleware"
	"gateway/internal/proxy"
	"gateway/internal/queue"
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

	// 基础环境 Context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 挂载限流中间件在最顶端，每秒充能 5 个，最大容纳 10 个
	limiter := middleware.NewIPRateLimiter(rate.Limit(5), 10)
	// 启动后台 Cleanup 协程，每隔 1 分钟执行一次，清除过期时间达 5 分钟的 IP 记录
	go limiter.StartCleanup(ctx, 1*time.Minute, 5*time.Minute)
	r.Use(middleware.RateLimitMiddleware(limiter))

	// 基础中间件
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	internalSecret := os.Getenv("INTERNAL_SECRET")

	// 初始化 SQLite 本地队列管理器
	queueMgr, err := queue.NewQueueManager("gateway.db", aaBridgeURL, internalSecret)
	if err != nil {
		log.Fatalf("Failed to initialize queue manager: %v", err)
	}
	defer queueMgr.Close()

	// 启动后台重试 Worker
	queueMgr.StartWorker(ctx)

	// 创建反向代理
	proxyHandler, err := proxy.NewReverseProxy(elizaAgentURL, aaBridgeURL, internalSecret, queueMgr)
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

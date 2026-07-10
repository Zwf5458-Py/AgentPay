package main

import (
	"context"
	"encoding/json"
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
	"gateway/internal/stripe"
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

	// 挂载 CORS 中间件放行跨域及暴露头部
	r.Use(CORSMiddleware)

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

	queueMgr, err := queue.NewQueueManager("gateway.db", aaBridgeURL, internalSecret)
	if err != nil {
		log.Fatalf("Failed to initialize queue manager: %v", err)
	}
	defer queueMgr.Close()

	// 注入到 X402 中间件供其进行已消费 Session 的持久化校验
	middleware.DBQueueManager = queueMgr

	// 启动后台重试 Worker
	queueMgr.StartWorker(ctx)

	// 创建反向代理
	proxyHandler, err := proxy.NewReverseProxy(elizaAgentURL, aaBridgeURL, internalSecret, queueMgr)
	if err != nil {
		log.Fatalf("Failed to initialize reverse proxy: %v", err)
	}

	stripeKey := os.Getenv("STRIPE_SECRET_KEY")
	stripeClient := stripe.NewStripeClient(stripeKey)

	// 路由注册
	r.Route("/agent", func(r chi.Router) {
		r.Use(middleware.X402Middleware)
		r.Handle("/execute", proxyHandler)
	})

	r.Post("/stripe/create-session", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Amount     uint64 `json:"amount"`
			SuccessURL string `json:"successUrl"`
			CancelURL  string `json:"cancelUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Amount == 0 {
			req.Amount = 50000 // 默认 50000 微单位 (0.05 USD)
		}
		if req.SuccessURL == "" {
			req.SuccessURL = "http://localhost:3000/success"
		}
		if req.CancelURL == "" {
			req.CancelURL = "http://localhost:3000/cancel"
		}

		sessionID, url, err := stripeClient.CreateCheckoutSession(req.Amount, req.SuccessURL, req.CancelURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"sessionId": sessionID,
			"url":       url,
		})
	})

	r.Post("/stripe/webhook", func(w http.ResponseWriter, r *http.Request) {
		// Stripe Webhook 占位端点说明：
		// 目前网关在 execute 审计时会主动通过 VerifyCheckoutSession 向 Stripe API 发送请求实时校验 Session 支付状态。
		// 本端点目前作为接收支付成功异步回调的 Logging/审计日志占位端点。
		// 生产环境部署时：需在该接口内配置官方的 HMAC-SHA256 Stripe Webhook 签名验证机制 (Stripe-Signature)，
		// 并将已支付订单异步固化更新到 DB 的 consumed_stripe_sessions 状态中。
		sigHeader := r.Header.Get("Stripe-Signature")
		if sigHeader == "" && os.Getenv("APP_ENV") == "production" {
			http.Error(w, "Missing Stripe-Signature header", http.StatusBadRequest)
			return
		}
		log.Println("[Stripe Webhook] Received webhook event notification.")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	})

	// 调试接口：获取最近的 10 个结算任务
	r.Get("/debug/tasks", func(w http.ResponseWriter, r *http.Request) {
		tasks, err := queueMgr.GetLatestTasks(10)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tasks)
	})

	// 调试接口：清空所有结算任务
	r.Post("/debug/tasks/clear", func(w http.ResponseWriter, r *http.Request) {
		if err := queueMgr.ClearTasks(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Tasks cleared"))
	})

	// 启动服务
	addr := "0.0.0.0:" + port
	log.Printf("Server listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server exited with error: %v", err)
	}
}

// CORSMiddleware 放行跨域及暴露头部
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Internal-Secret")
		// 必须允许前端读取自定义头部
		w.Header().Set("Access-Control-Expose-Headers", "X-402-Payment-Address, X-402-Price, X-402-Payment-Type, X-Agent-Proof, X-402-Platform-Bps, X-402-Model-Provider, X-402-Payment-Methods, X-402-Hold-Amount, X-402-Settle-Receipt, X-402-Currency, X-402-Chain, X-402-Version, X-402-Payment-Method, X-402-Stripe-Session")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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
	"gateway/internal/plugin"
	"gateway/internal/proxy"
	"gateway/internal/queue"
	"gateway/internal/stripe"
	"sync"

	"ledger/rail"
	"ledger/service"
	"ledger/store"

	pricingservice "pricing/service"
	pricingstore "pricing/store"
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
	// P4: 优先使用 Redis 分布式限流，降级为内存限流
	redisURL := os.Getenv("REDIS_URL")
	limiter := middleware.NewRedisRateLimiter(redisURL, rate.Limit(5), 10)
	var fallbackLimiter *middleware.IPRateLimiter

	if limiter == nil {
		// Redis 不可用，降级为内存限流
		fallbackLimiter = middleware.NewIPRateLimiter(rate.Limit(5), 10)
		go fallbackLimiter.StartCleanup(ctx, 1*time.Minute, 5*time.Minute)
		r.Use(middleware.RateLimitMiddleware(fallbackLimiter))
	} else {
		// Redis 限流：使用中间件包装（兼容现有 RateLimitMiddleware 签名）
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ip := middleware.GetClientIP(req)
				if !limiter.Allow(ip) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					json.NewEncoder(w).Encode(map[string]string{
						"error":   "rate_limit_exceeded",
						"message": "too many requests",
					})
					return
				}
				next.ServeHTTP(w, req)
			})
		})
	}

	// 基础中间件
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	internalSecret := os.Getenv("INTERNAL_SECRET")

	// 初始化可编程记账引擎 (Ledger Service)
	ledgerStore, err := store.NewSQLiteStore("ledger.db")
	if err != nil {
		log.Fatalf("Failed to initialize ledger store: %v", err)
	}

	stripeKey := os.Getenv("STRIPE_SECRET_KEY")
	stripeRail := rail.NewStripeRail(stripeKey)
	cryptoRail := rail.NewCryptoRail(aaBridgeURL, internalSecret)

	rails := map[string]rail.PaymentRail{
		"stripe": stripeRail,
		"crypto": cryptoRail,
	}
	ledgerService := service.NewLedgerService(ledgerStore, rails)

	// 初始化通用计费引擎 (Pricing Service)
	pricingStore, perr := pricingstore.NewSQLiteStore("pricing.db")
	if perr != nil {
		log.Fatalf("Failed to initialize pricing store: %v", perr)
	}
	pricingService := pricingservice.NewPricingService(pricingStore)

	// 挂载 Redis 客户端到计费计量引擎以启用高频缓冲，并启动后台 5 秒定时刷盘 Worker
	if limiter != nil && limiter.GetRedisClient() != nil {
		pricingService.SetRedisClient(limiter.GetRedisClient())
	}
	pricingService.StartFlushWorker(ctx, 5*time.Second)

	queueMgr, err := queue.NewQueueManager("gateway.db", aaBridgeURL, internalSecret)
	if err != nil {
		log.Fatalf("Failed to initialize queue manager: %v", err)
	}
	defer queueMgr.Close()

	// 挂载记账服务与计费服务到异步队列中
	queueMgr.SetLedgerService(ledgerService)
	queueMgr.SetPricingService(pricingService)

	// 注入到 X402 中间件供其进行已消费 Session 的持久化校验
	middleware.DBQueueManager = queueMgr

	// 启动后台重试 Worker
	queueMgr.StartWorker(ctx)

	// P4: 启动锁回收 Worker（每分钟扫描超时锁）
	queueMgr.StartReclaim(ctx, 1*time.Minute)

	// 创建反向代理
	proxyHandler, err := proxy.NewReverseProxy(elizaAgentURL, aaBridgeURL, internalSecret, queueMgr)
	if err != nil {
		log.Fatalf("Failed to initialize reverse proxy: %v", err)
	}

	// 挂载记账服务到反向代理拦截器中
	proxyHandler.SetLedgerService(ledgerService)
	proxyHandler.SetPricingService(pricingService)

	stripeClient := stripe.NewStripeClient(stripeKey)

	// 路由注册
	mcpHandler := plugin.NewMcpHandler(ledgerService, pricingService, queueMgr)
	r.Handle("/v1/plugin/mcp", mcpHandler)

	r.Route("/agent", func(r chi.Router) {
		r.Use(middleware.X402Middleware)
		r.Handle("/execute", proxyHandler)
	})

	stripeCreateSessionHandler := func(w http.ResponseWriter, r *http.Request) {
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
			"sessionId":    sessionID,
			"url":          url,
			"checkout_url": url,
		})
	}

	r.Post("/stripe/create-session", stripeCreateSessionHandler)
	r.Post("/api/stripe/create-session", stripeCreateSessionHandler)

	stripeWebhookHandler := func(w http.ResponseWriter, r *http.Request) {
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
	}

	r.Post("/stripe/webhook", stripeWebhookHandler)
	r.Post("/api/stripe/webhook", stripeWebhookHandler)

	// 调试及清理接口组（挂载 AdminAuthMiddleware 鉴权以防数据泄露/恶意清空）
	r.Route("/debug", func(r chi.Router) {
		r.Use(AdminAuthMiddleware)
		r.Get("/tasks", func(w http.ResponseWriter, r *http.Request) {
			tasks, err := queueMgr.GetLatestTasks(10)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(tasks)
		})
		r.Post("/tasks/clear", func(w http.ResponseWriter, r *http.Request) {
			if err := queueMgr.ClearTasks(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Tasks cleared"))
		})
	})

	// 注册 /admin 路由组（受 CORSMiddleware 和 AdminAuthMiddleware 保护）
	r.Route("/admin", func(r chi.Router) {
		r.Use(AdminAuthMiddleware)
		r.Get("/stats", func(w http.ResponseWriter, r *http.Request) {
			stats, err := queueMgr.GetAdminStats()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(stats)
		})
		r.Get("/tasks", func(w http.ResponseWriter, r *http.Request) {
			tasks, err := queueMgr.GetAllTasks()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(tasks)
		})
		r.Post("/tasks/retry", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				LockID string `json:"lock_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			if req.LockID == "" {
				http.Error(w, "lock_id is required", http.StatusBadRequest)
				return
			}
			if err := queueMgr.ManualRetryTask(req.LockID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Task scheduled for retry"))
		})
		r.Post("/stripe-sessions/clear", func(w http.ResponseWriter, r *http.Request) {
			if err := queueMgr.ClearStripeSessions(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Stripe sessions cleared"))
		})
		r.Get("/stripe-sessions", func(w http.ResponseWriter, r *http.Request) {
			sessions, err := queueMgr.GetConsumedStripeSessions()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(sessions)
		})
	})

	// 启动服务
	addr := "0.0.0.0:" + port
	log.Printf("Server listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server exited with error: %v", err)
	}
}

// AdminAuthMiddleware 管理员鉴权中间件
func AdminAuthMiddleware(next http.Handler) http.Handler {
	var loggedWarning sync.Once // 只在开发模式下输出一次安全警告
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalSecret := os.Getenv("INTERNAL_SECRET")
		isProd := os.Getenv("APP_ENV") == "production"

		if internalSecret == "" {
			if isProd {
				log.Println("[CRITICAL SECURITY WARNING] Admin APIs blocked because INTERNAL_SECRET is not configured in production env.")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			loggedWarning.Do(func() {
				log.Println("[WARNING] INTERNAL_SECRET is empty. Admin APIs are unprotected in development environment!")
			})
		} else {
			reqSecret := r.Header.Get("X-Internal-Secret")
			if reqSecret != internalSecret {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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

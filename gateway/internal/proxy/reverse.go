package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"gateway/internal/middleware"
	"gateway/internal/queue"
)

// ReverseProxyWrapper 封装了反向代理的逻辑
type ReverseProxyWrapper struct {
	proxy          *httputil.ReverseProxy
	aaBridgeURL    string
	agentOwner     string
	escrowAddress  string
	internalSecret string
	QueueManager   *queue.QueueManager
}

// NewReverseProxy 构造反向代理实例
func NewReverseProxy(targetURL string, aaBridgeURL string, internalSecret string, queueMgr *queue.QueueManager) (*ReverseProxyWrapper, error) {
	url, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	// 默认值
	agentOwner := os.Getenv("AGENT_OWNER")
	if agentOwner == "" {
		agentOwner = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" // Anvil 默认第一个账户
	}

	escrowAddress := os.Getenv("ESCROW_ADDRESS")
	if escrowAddress == "" {
		escrowAddress = "0x5FbDB2315678afecb367f032d93F642f64180aa3" // 默认合约 Mock 地址
	}

	wrapper := &ReverseProxyWrapper{
		aaBridgeURL:    aaBridgeURL,
		agentOwner:     agentOwner,
		escrowAddress:  escrowAddress,
		internalSecret: internalSecret,
		QueueManager:   queueMgr,
	}

	proxy := httputil.NewSingleHostReverseProxy(url)

	// 劫持响应
	proxy.ModifyResponse = func(res *http.Response) error {
		proof := res.Header.Get("X-Agent-Proof")
		if proof != "" {
			// 提取 lockId
			ctx := res.Request.Context()
			lockID := middleware.GetLockID(ctx)

			if lockID != "" {
				log.Printf("[Proxy] Intercepted X-Agent-Proof. Enqueueing settle task for lockId: %s", lockID)
				if err := wrapper.QueueManager.Enqueue(lockID, proof, wrapper.agentOwner, wrapper.escrowAddress); err != nil {
					log.Printf("[Proxy] Enqueue failed for lockId %s: %v", lockID, err)
				}
			} else {
				log.Printf("[Proxy] Intercepted X-Agent-Proof but lockId is missing in request context.")
			}
		}
		return nil
	}

	wrapper.proxy = proxy
	return wrapper, nil
}

// ServeHTTP 转发请求
func (w *ReverseProxyWrapper) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	w.proxy.ServeHTTP(rw, req)
}

package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"gateway/internal/middleware"
)

// ReverseProxyWrapper 封装了反向代理的逻辑
type ReverseProxyWrapper struct {
	proxy         *httputil.ReverseProxy
	aaBridgeURL   string
	agentOwner    string
	escrowAddress string
	Client        *http.Client // 复用 HTTP Client，避免并发端口耗尽
}

// NewReverseProxy 构造反向代理实例
func NewReverseProxy(targetURL string, aaBridgeURL string) (*ReverseProxyWrapper, error) {
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
		aaBridgeURL:   aaBridgeURL,
		agentOwner:    agentOwner,
		escrowAddress: escrowAddress,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
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
				log.Printf("[Proxy] Intercepted X-Agent-Proof. Triggering async settle for lockId: %s", lockID)
				go wrapper.settle(lockID, proof)
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

func (w *ReverseProxyWrapper) settle(lockID, proof string) {
	// 1. Panic 安全屏障，决不引发主网关进程崩溃
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Proxy Settle] Recovered from panic: %v", r)
		}
	}()

	bodyMap := map[string]string{
		"lockId":        lockID,
		"proof":         proof,
		"agentOwner":    w.agentOwner,
		"escrowAddress": w.escrowAddress,
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		log.Printf("[Proxy Settle] Error marshaling settle body: %v", err)
		return
	}

	maxRetries := 3
	var lastErr error
	var resp *http.Response

	// 2. 退避重试机制
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("POST", w.aaBridgeURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			log.Printf("[Proxy Settle] Error creating settle request (attempt %d/%d): %v", attempt, maxRetries, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		// 3. 复用结构体自带的 Client 进行网络调用
		resp, err = w.Client.Do(req)
		if err != nil {
			lastErr = err
			log.Printf("[Proxy Settle] Connection failed (attempt %d/%d) for lockId %s: %v", attempt, maxRetries, lockID, err)
			if attempt < maxRetries {
				time.Sleep(1 * time.Second)
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP status %s: %s", resp.Status, string(respBody))
			log.Printf("[Proxy Settle] Bridge returned non-200 (attempt %d/%d) for lockId %s: %v", attempt, maxRetries, lockID, lastErr)
			if attempt < maxRetries {
				time.Sleep(1 * time.Second)
			}
			continue
		}

		// 成功响应
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[Proxy Settle] AA Bridge response for lockId %s: Status: %s, Body: %s", lockID, resp.Status, string(respBody))
		return
	}

	// 3次重试后依然失败，记录致命报警日志
	log.Printf("[Proxy Settle] [CRITICAL ERROR] Failed to settle payment for lockId %s after %d attempts. Last error: %v", lockID, maxRetries, lastErr)
}

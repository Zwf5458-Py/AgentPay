package proxy

import (
	"bytes"
	"encoding/json"
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

	req, err := http.NewRequest("POST", w.aaBridgeURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Printf("[Proxy Settle] Error creating settle request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Proxy Settle] Error executing settle request for lockId %s: %v", lockID, err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[Proxy Settle] AA Bridge response for lockId %s: Status: %s, Body: %s", lockID, resp.Status, string(respBody))
}

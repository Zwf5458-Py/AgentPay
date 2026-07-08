# Task 4 Report: Go Gateway 令牌桶限流 rate-limiting 实现

## 1. 任务概述
在 Go Gateway 内部实现了基于 IP 粒度的令牌桶限流拦截器，保护网关及 downstream Eliza Agent 算力资源。频次超限后拦截返回 `HTTP 429` 错误。

## 2. 代码实现清单

### 2.1 新增限流中间件 `gateway/internal/middleware/rate_limit.go`
- 实现了 `IPRateLimiter` 结构体，其持有 `sync.RWMutex` 以支持高并发下安全的 Map 读写。
- 提供 `NewIPRateLimiter(r rate.Limit, b int)` 构造器。
- 实现 `GetLimiter(ip string) *rate.Limiter` 动态创建与缓存单个客户端 IP 对应的令牌桶。
- 实现 `RateLimitMiddleware` 中间件，使用以下规则精准提取客户端真实 IP（剔除端口号）：
  - 优先解析 `X-Forwarded-For` 头部的首个 IP 实体。
  - 若不存在，则回退解析 `r.RemoteAddr`。
  - 自动剥离 `[ip]:port` 格式的端口，支持 IPv4 与 IPv6。
- 限流触发时，返回 `HTTP 429 Too Many Requests` 以及 JSON 格式错误：
  ```json
  {"error":"rate_limit_exceeded","message":"too many requests"}
  ```

### 2.2 挂载中间件至 `gateway/cmd/gateway/main.go`
在 Chi 路由最外侧挂载限流中间件：
```go
limiter := middleware.NewIPRateLimiter(rate.Limit(5), 10)
r.Use(middleware.RateLimitMiddleware(limiter))
```
以保证其在 `X402Middleware` 之前执行，起到前置限流保护作用。

### 2.3 单元测试 `gateway/internal/middleware/x402_test.go`
新增了单元测试 `TestRateLimitMiddleware_LimitExceeded`：
- 配置 `IPRateLimiter` 限制速率为 2/s，桶大小为 3。
- 循环连续发送 5 次请求。
- 断言前 3 次正常通过限流（由于未带 Auth token，被下层 `X402Middleware` 拦截返回 402）。
- 第 4 次和第 5 次触发限流，断言返回 `HTTP 429` 且 Body 正确匹配 `rate_limit_exceeded`。

## 3. 测试与验证结果
在 `gateway` 目录下执行 `go test -v ./...`。
运行结果：已通过。

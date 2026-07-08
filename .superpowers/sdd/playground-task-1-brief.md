# Task 1 Brief: Go Gateway 跨域 CORS 中间件与调试接口开发

## 目标
在 Go Gateway 内部支持 CORS 跨域请求（允许浏览器 JavaScript 读取 X-402 特殊头部），并提供本地 SQLite 后台结算队列的任务监视接口。

## 涉及文件
- 修改: `gateway/internal/queue/sqlite_queue.go`
- 修改: `gateway/cmd/gateway/main.go`
- 修改: `gateway/internal/middleware/x402_test.go`

## 详细要求

### 1. 编写 GetLatestTasks 查询
- 在 `gateway/internal/queue/sqlite_queue.go` 中为 `QueueManager` 实现：
  `func (q *QueueManager) GetLatestTasks(limit int) ([]map[string]interface{}, error)`
- 锁定 Mutex 并执行 SQL：
  `SELECT lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at FROM settle_tasks ORDER BY id DESC LIMIT ?`
- 读取时，将 `created_at`（在 SQLite 中类型为 int64）直接 Scan 进 `int64` 变量，避免时间解析反射 panic。

### 2. 网关挂载 CORS 跨域规则
- 在 `gateway/cmd/gateway/main.go` 中实现跨域中间件：
  ```go
  func CORSMiddleware(next http.Handler) http.Handler {
      return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
          w.Header().Set("Access-Control-Allow-Origin", "*")
          w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
          w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Internal-Secret")
          // 必须允许前端读取自定义头部
          w.Header().Set("Access-Control-Expose-Headers", "X-402-Payment-Address, X-402-Price, X-402-Payment-Type, X-Agent-Proof")
          
          if r.Method == "OPTIONS" {
              w.WriteHeader(http.StatusOK)
              return
          }
          next.ServeHTTP(w, r)
      })
  }
  ```
- 将其配置在路由器 `r` 最外侧的最顶端，作为首个挂载的全局中间件。

### 3. 注册 /debug/tasks 调试接口
- 在 `gateway/cmd/gateway/main.go` 中增加接口：
  - 路由 `GET /debug/tasks`。
  - 调用 `queueMgr.GetLatestTasks(10)`，并以 JSON 格式输出，返回 200。

### 4. 编写测试验证
- 在 `gateway/internal/middleware/x402_test.go` 中，新增单元测试：
  - 验证对 `/agent/execute` 发送 OPTIONS 请求，断言返回 200 且包含所有必需的 CORS Headers。
  - 验证对 `/debug/tasks` 发送 GET 请求，断言能够返回 JSON 数组，状态码为 200。

## 验证与测试命令
在 `gateway` 目录下执行：
```bash
go test -v ./...
```
要求：100% 成功。

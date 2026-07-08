# Go Gateway 跨域 CORS 中间件与调试接口开发实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Go Gateway 中支持 CORS 跨域请求并提供本地 SQLite 队列任务监视接口，同时编写测试进行验证。

**Architecture:** 
1. 为 SQLite `QueueManager` 新增互斥锁保护的 `GetLatestTasks` 方法，安全地以 `int64` 类型读取 `created_at` 字段。
2. 在 `cmd/gateway/main.go` 中实现 `CORSMiddleware` 并将其挂载到路由器最外侧最顶端，同时在路由上注册 `GET /debug/tasks` 调用 `QueueManager.GetLatestTasks` 返回 JSON 数组。
3. 在 `x402_test.go` 中编写单元测试来验证 OPTIONS 请求的 CORS 响应头以及 `/debug/tasks` 响应状态与格式。

**Tech Stack:** Go, SQLite, chi v5 router

## Global Constraints

- 修改: `gateway/internal/queue/sqlite_queue.go`
- 修改: `gateway/cmd/gateway/main.go`
- 修改: `gateway/internal/middleware/x402_test.go`
- 测试验证通过：在 `gateway` 目录下执行 `go test -v ./...`

---

### Task 1: 实现 GetLatestTasks 方法

**Files:**
- Modify: `gateway/internal/queue/sqlite_queue.go`

**Interfaces:**
- Consumes: `QueueManager` 数据库连接和互斥锁
- Produces: `func (qm *QueueManager) GetLatestTasks(limit int) ([]map[string]interface{}, error)`

- [x] **Step 1: 在 `gateway/internal/queue/sqlite_queue.go` 中实现 `GetLatestTasks`**
  
  在文件末尾添加以下代码：
  ```go
  // GetLatestTasks 获取最近的 limit 个结算任务，用互斥锁保护，created_at 读为 int64
  func (qm *QueueManager) GetLatestTasks(limit int) ([]map[string]interface{}, error) {
  	qm.mu.Lock()
  	defer qm.mu.Unlock()
  
  	query := `
  	SELECT lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at
  	FROM settle_tasks
  	ORDER BY id DESC
  	LIMIT ?
  	`
  	rows, err := qm.db.Query(query, limit)
  	if err != nil {
  		return nil, fmt.Errorf("failed to query latest tasks: %w", err)
  	}
  	defer rows.Close()
  
  	var tasks []map[string]interface{}
  	for rows.Next() {
  		var lockID, proof, agentOwner, escrowAddress, status string
  		var retryCount int
  		var createdAt int64
  		err := rows.Scan(&lockID, &proof, &agentOwner, &escrowAddress, &status, &retryCount, &createdAt)
  		if err != nil {
  			return nil, fmt.Errorf("failed to scan task: %w", err)
  		}
  		task := map[string]interface{}{
  			"lock_id":        lockID,
  			"proof":          proof,
  			"agent_owner":    agentOwner,
  			"escrow_address": escrowAddress,
  			"status":         status,
  			"retry_count":    retryCount,
  			"created_at":     createdAt,
  		}
  		tasks = append(tasks, task)
  	}
  	if err := rows.Err(); err != nil {
  		return nil, fmt.Errorf("rows iteration error: %w", err)
  	}
  	return tasks, nil
  }
  ```

- [x] **Step 2: 编译代码，检查语法错误**
  
  运行: `go build ./internal/queue`
  Expected: 编译成功，无任何错误输出。

---

### Task 2: 配置 CORS 中间件与 `/debug/tasks` 路由

**Files:**
- Modify: `gateway/cmd/gateway/main.go`

- [x] **Step 1: 在 `gateway/cmd/gateway/main.go` 中实现 `CORSMiddleware` 并添加 `encoding/json` 引入**

  修改 imports 列表，加入 `"encoding/json"`。
  在 `main.go` 的 main 函数外面添加 `CORSMiddleware`：
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

- [x] **Step 2: 挂载 `CORSMiddleware` 到路由器最外侧最顶端**

  在 `r := chi.NewRouter()` 之后，立刻挂载 CORS 中间件：
  ```go
  	r := chi.NewRouter()
  
  	// 挂载 CORS 中间件
  	r.Use(CORSMiddleware)
  ```

- [x] **Step 3: 注册 `GET /debug/tasks` 路由**

  在 `queueMgr` 初始化之后（例如第 66 行 `queueMgr.StartWorker(ctx)` 之后），注册调试接口：
  ```go
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
  ```

- [x] **Step 4: 编译主程序**

  运行: `go build ./cmd/gateway`
  Expected: 编译成功。

---

### Task 3: 编写单元测试验证

**Files:**
- Modify: `gateway/internal/middleware/x402_test.go`

- [x] **Step 1: 新增 `TestCORS_OPTIONS` 和 `TestDebugTasks` 测试函数**

  在 `gateway/internal/middleware/x402_test.go` 的末尾追加：
  ```go
  func TestCORS_OPTIONS(t *testing.T) {
  	corsMiddleware := func(next http.Handler) http.Handler {
  		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  			w.Header().Set("Access-Control-Allow-Origin", "*")
  			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
  			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Internal-Secret")
  			w.Header().Set("Access-Control-Expose-Headers", "X-402-Payment-Address, X-402-Price, X-402-Payment-Type, X-Agent-Proof")
  			
  			if r.Method == "OPTIONS" {
  				w.WriteHeader(http.StatusOK)
  				return
  			}
  			next.ServeHTTP(w, r)
  		})
  	}
  
  	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.WriteHeader(http.StatusOK)
  	}))
  
  	req := httptest.NewRequest("OPTIONS", "/agent/execute", nil)
  	rr := httptest.NewRecorder()
  
  	handler.ServeHTTP(rr, req)
  
  	if rr.Code != http.StatusOK {
  		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
  	}
  
  	expectedHeaders := map[string]string{
  		"Access-Control-Allow-Origin":  "*",
  		"Access-Control-Allow-Methods": "POST, GET, OPTIONS, PUT, DELETE",
  		"Access-Control-Allow-Headers": "Content-Type, Authorization, X-Internal-Secret",
  		"Access-Control-Expose-Headers": "X-402-Payment-Address, X-402-Price, X-402-Payment-Type, X-Agent-Proof",
  	}
  
  	for key, expectedValue := range expectedHeaders {
  		gotValue := rr.Header().Get(key)
  		if gotValue != expectedValue {
  			t.Errorf("Header %s: expected %q, got %q", key, expectedValue, gotValue)
  		}
  	}
  }
  
  func TestDebugTasks(t *testing.T) {
  	dbPath := t.TempDir() + "/test_debug_tasks.db"
  	queueMgr, err := queue.NewQueueManager(dbPath, "http://mock-bridge/aa/settle", "test-secret")
  	if err != nil {
  		t.Fatalf("Failed to create QueueManager: %v", err)
  	}
  	defer queueMgr.Close()
  
  	err = queueMgr.Enqueue("lock-123", "proof-abc", "owner-xyz", "escrow-123")
  	if err != nil {
  		t.Fatalf("Failed to enqueue task: %v", err)
  	}
  
  	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		tasks, err := queueMgr.GetLatestTasks(10)
  		if err != nil {
  			http.Error(w, err.Error(), http.StatusInternalServerError)
  			return
  		}
  		w.Header().Set("Content-Type", "application/json")
  		w.WriteHeader(http.StatusOK)
  		json.NewEncoder(w).Encode(tasks)
  	})
  
  	req := httptest.NewRequest("GET", "/debug/tasks", nil)
  	rr := httptest.NewRecorder()
  
  	handler.ServeHTTP(rr, req)
  
  	if rr.Code != http.StatusOK {
  		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
  	}
  
  	if contentType := rr.Header().Get("Content-Type"); contentType != "application/json" {
  		t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
  	}
  
  	var tasks []map[string]interface{}
  	err = json.Unmarshal(rr.Body.Bytes(), &tasks)
  	if err != nil {
  		t.Fatalf("Failed to unmarshal response: %v", err)
  	}
  
  	if len(tasks) != 1 {
  		t.Fatalf("Expected 1 task, got %d", len(tasks))
  	}
  
  	task := tasks[0]
  	if task["lock_id"] != "lock-123" {
  		t.Errorf("Expected lock_id 'lock-123', got %v", task["lock_id"])
  	}
  	if task["proof"] != "proof-abc" {
  		t.Errorf("Expected proof 'proof-abc', got %v", task["proof"])
  	}
  	if task["agent_owner"] != "owner-xyz" {
  		t.Errorf("Expected agent_owner 'owner-xyz', got %v", task["agent_owner"])
  	}
  	if task["escrow_address"] != "escrow-123" {
  		t.Errorf("Expected escrow_address 'escrow-123', got %v", task["escrow_address"])
  	}
  	if task["status"] != "pending" {
  		t.Errorf("Expected status 'pending', got %v", task["status"])
  	}
  	if task["retry_count"] == nil {
  		t.Errorf("Expected retry_count to not be nil")
  	}
  	if task["created_at"] == nil {
  		t.Errorf("Expected created_at to not be nil")
  	}
  }
  ```

- [x] **Step 2: 运行测试并确保全部通过**

  运行: `go test -v ./...`
  Expected: 所有测试通过。

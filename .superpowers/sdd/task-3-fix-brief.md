# Task 3 修复 Brief: Go Gateway SQLite 并发控制与优雅退出加固

## 问题描述与修复要求

### 1. 加固 SQLite 并发连接与吞吐配置 (`gateway/internal/queue/sqlite_queue.go`)
- 在 `NewQueueManager` 实例化 `db` 后，立即挂载连接数与吞吐性能优化指令：
  - `db.SetMaxOpenConns(1)`：强制单连接写操作，彻底杜绝高并发文件锁死错误。
  - 在建表前，执行 `PRAGMA journal_mode=WAL;` 开启预写日志，提高吞吐量。
  - 在建表前，执行 `PRAGMA busy_timeout=5000;` 设定 5000 毫秒忙碌重试等待。

### 2. 引入 sync.WaitGroup 建立优雅退出机制 (`gateway/internal/queue/sqlite_queue.go`)
- 在 `QueueManager` 结构体中添加 `wg sync.WaitGroup` 追踪协程。
- 修改 `StartWorker(ctx context.Context)`：
  - 在拉起 `go func()` 前，执行 `q.wg.Add(1)`。
  - 在 `go func()` 协程入口的 defer 链中追加 `defer q.wg.Done()`。
- 修改 `Close()`：
  - 目前 `Close` 仅直接关闭 DB。需要修改为：等重试协程完全检测到 Done 信号并退出（通过 `q.wg.Wait()` 阻塞等待）后，再最终安全调用 `q.db.Close()`，保障优雅退出。

## 验证与测试命令
在 `gateway` 目录下执行：
```bash
go test -v ./...
```
要求：所有测试正常跑通。

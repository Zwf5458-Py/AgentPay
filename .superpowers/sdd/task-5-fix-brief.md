# Task 5 修复 Brief: Go 支付网关安全加固与连接复用

## 问题描述与修复要求

### 1. 异步结算 Panic 安全屏障 (`gateway/internal/proxy/reverse.go`)
- 在 `settle` 函数体入口中，添加 `defer recover()` Panic 拦截。如果发生崩溃，捕获错误并写入日志日志，决不能引发网关进程崩溃宕机：
  ```go
  defer func() {
      if r := recover(); r != nil {
          log.Printf("[Proxy Settle] Recovered from panic: %v", r)
      }
  }()
  ```

### 2. HTTP 客户端复用机制
- 重构 `ReverseProxyWrapper` 结构体，增加 `Client *http.Client` 成员变量。
- 在初始化方法 `NewReverseProxyWrapper` 中一次性实例化客户端（设置合理 Timeout 如 10 秒）。
- 在 `settle` 方法中直接复用该 `w.Client` 进行网络调用，禁止在请求级别重新实例化。

### 3. 指数退避重试机制
- 为 `settle` 方法添加结算重试逻辑。如果向 AA Bridge 请求发生网络超时、非 200 HTTP 响应，自动记录日志并重试（最多重试 3 次，每次重试等待 1-2 秒）。如果 3 次后依然失败，记录致命报警日志。

## 验证与测试命令
在 `gateway` 目录下执行：
```bash
go test -v ./...
```
要求：所有测试正常通过。

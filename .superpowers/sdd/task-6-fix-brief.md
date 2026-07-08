# Task 6 修复 Brief: SDK 逻辑健壮性加固与限额漏洞修复

## 问题描述与修复要求

### 1. 修复 Nullish 合并漏洞 (`sdk/src/client.ts`)
- 将配置初始化行：
  `this.maxPriceLimit = config.maxPriceLimit || 5000n;`
  修改为：
  `this.maxPriceLimit = config.maxPriceLimit ?? 5000n;`
  以确保传入 `0n` 作为支付上限时不会被 falsy 条件覆盖。

### 2. 价格解析异常防御 (`sdk/src/client.ts`)
- 对价格字段进行 try-catch 异常包裹：
  ```typescript
  let price: bigint;
  try {
      price = BigInt(priceStr);
  } catch (err) {
      throw new Error(`Invalid HTTP 402 price format from Gateway: ${priceStr}`);
  }
  ```

### 3. 添加签名 TODO 注解 (`sdk/src/client.ts`)
- 在生产环境分支中增加清晰的代码 TODO 标注以规划后续：
  `// TODO: Implement EIP-3009 EIP-712 typing signature verification using viem.signTypedData`

### 4. 单元测试追加 (`sdk/test/e2e.test.ts`)
- 新增测试用例 `should throw error if maxPriceLimit is set to 0n`：
  - 配置 `maxPriceLimit: 0n`，调用 `execute` 发起推理。
  - 验证捕获到的异常信息，断言确实抛出价格超限错误，验证限额保护。

## 验证与测试命令
在 `sdk` 目录下执行：
```bash
npm run test
```
要求：所有测试正常通过。

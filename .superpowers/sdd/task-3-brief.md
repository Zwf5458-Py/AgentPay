# Task 3 Brief: Update Gateway X-402 Middleware Headers & Configs

**Goal**: Update the gateway middleware to include split-settlement information in the 402 challenge response headers and expose them in CORS.

**Files**:
- Modify: `gateway/internal/middleware/x402.go`
- Modify: `gateway/cmd/gateway/main.go` (CORS headers list)
- Test: `gateway/internal/middleware/x402_test.go`

**Instructions**:
1. Locate `trigger402` function in `gateway/internal/middleware/x402.go`.
2. Update it to set the following headers in 402 responses:
   - `X-402-Platform-Bps`: value of environment variable `PLATFORM_BPS` (default is `"10"`, i.e., 0.1%).
   - `X-402-Model-Provider`: value of environment variable `MODEL_PROVIDER_ADDRESS` (default is `"0x90F79bf6EB2c4f870365E785982E1f101E93b906"`).
   - `X-402-Payment-Methods`: `"crypto-channel,fiat-stripe"`.
3. Locate CORS middleware in `gateway/cmd/gateway/main.go`. Update `Access-Control-Expose-Headers` list to include:
   `X-402-Platform-Bps, X-402-Model-Provider, X-402-Payment-Methods, X-402-Hold-Amount, X-402-Settle-Receipt, X-402-Currency, X-402-Chain, X-402-Version`.
4. Update `gateway/internal/middleware/x402_test.go` where it mocks CORS headers to also include the new headers in the expose list.
5. Verify all tests in `gateway/internal/middleware` pass.
6. Commit changes.

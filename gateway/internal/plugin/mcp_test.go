package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ledger/rail"
	"ledger/service"
	"ledger/store"
)

func TestMcpHandler(t *testing.T) {
	// 1. 初始化测试数据库和 Ledger
	dbFile := "./test_mcp.db"
	defer os.Remove(dbFile)

	sqliteStore, err := store.NewSQLiteStore(dbFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer sqliteStore.Close()

	stripeRail := rail.NewStripeRail(true)
	rails := map[string]rail.PaymentRail{
		"stripe": stripeRail,
		"crypto": stripeRail, // 这里为了方便单元测试，让 crypto 和 stripe 都使用 stripe mock 轨道
	}

	ledgerSvc := service.NewLedgerService(sqliteStore, rails)
	mcpHandler := NewMcpHandler(ledgerSvc, nil)

	// 2. 测试 tools/list
	listReq := JsonRpcRequest{
		JsonRpc: "2.0",
		Method:  "tools/list",
		ID:      1,
	}
	body, _ := json.Marshal(listReq)
	req := httptest.NewRequest("POST", "/v1/plugin/mcp", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	mcpHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var listResp JsonRpcResponse
	json.NewDecoder(rec.Body).Decode(&listResp)

	resultMap, ok := listResp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", listResp.Result)
	}

	tools, ok := resultMap["tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	// 3. 测试 tools/call (pay)
	payArgs := map[string]interface{}{
		"agentId":    888,
		"amountUsdc": 0.50,
	}
	callPayParams := map[string]interface{}{
		"name":      "pay",
		"arguments": payArgs,
	}
	callPayParamsBytes, _ := json.Marshal(callPayParams)

	payReq := JsonRpcRequest{
		JsonRpc: "2.0",
		Method:  "tools/call",
		Params:  callPayParamsBytes,
		ID:      2,
	}
	bodyPay, _ := json.Marshal(payReq)
	reqPay := httptest.NewRequest("POST", "/v1/plugin/mcp", bytes.NewBuffer(bodyPay))
	recPay := httptest.NewRecorder()

	mcpHandler.ServeHTTP(recPay, reqPay)

	var payResp JsonRpcResponse
	json.NewDecoder(recPay.Body).Decode(&payResp)

	payResult, ok := payResp.Result.(map[string]interface{})
	if !ok {
		t.Logf("payResp Error: %v", payResp.Error)
		t.Logf("Response body: %s", recPay.Body.String())
		t.Fatalf("expected call result map, got %v", payResp.Result)
	}

	contentList, _ := payResult["content"].([]interface{})
	if len(contentList) == 0 {
		t.Fatalf("expected response content")
	}

	contentTextMap := contentList[0].(map[string]interface{})
	textVal := contentTextMap["text"].(string)

	if !strings.Contains(textVal, "Successfully locked funds. InvoiceID: ") {
		t.Errorf("unexpected output text: %s", textVal)
	}

	// 提取 InvoiceID 进行 checkout 测试
	invoiceID := textVal[strings.Index(textVal, "InvoiceID: ")+11 : strings.Index(textVal, ", Locked:")]

	// 给 payer 模拟存钱
	_ = sqliteStore.UpdateBalance(context.Background(), "mcp_client_default", 1000000)

	// 4. 测试 tools/call (checkout)
	checkoutArgs := map[string]interface{}{
		"invoiceId":      invoiceID,
		"actualCostUsdc": 0.35,
	}
	callCheckoutParams := map[string]interface{}{
		"name":      "checkout",
		"arguments": checkoutArgs,
	}
	callCheckoutParamsBytes, _ := json.Marshal(callCheckoutParams)

	checkoutReq := JsonRpcRequest{
		JsonRpc: "2.0",
		Method:  "tools/call",
		Params:  callCheckoutParamsBytes,
		ID:      3,
	}
	bodyCheckout, _ := json.Marshal(checkoutReq)
	reqCheckout := httptest.NewRequest("POST", "/v1/plugin/mcp", bytes.NewBuffer(bodyCheckout))
	recCheckout := httptest.NewRecorder()

	mcpHandler.ServeHTTP(recCheckout, reqCheckout)

	var checkoutResp JsonRpcResponse
	json.NewDecoder(recCheckout.Body).Decode(&checkoutResp)

	checkoutResult := checkoutResp.Result.(map[string]interface{})
	checkoutContent := checkoutResult["content"].([]interface{})
	checkoutText := checkoutContent[0].(map[string]interface{})["text"].(string)

	if !strings.Contains(checkoutText, "Successfully settled invoice") {
		t.Errorf("unexpected checkout response: %s", checkoutText)
	}

	// 验证账户余额是否正确清算记账
	payerAcc, _ := sqliteStore.GetAccount(context.Background(), "mcp_client_default")
	expectedPayerBalance := 1000000 - 350000
	if payerAcc.Balance != int64(expectedPayerBalance) {
		t.Errorf("expected payer balance %d, got %d", expectedPayerBalance, payerAcc.Balance)
	}
}

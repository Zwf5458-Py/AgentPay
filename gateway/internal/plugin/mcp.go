package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"gateway/internal/queue"
	"ledger/rail"
	"ledger/service"

	pricingmodel "pricing/model"
	pricingservice "pricing/service"
)

type McpHandler struct {
	ledgerSvc *service.LedgerService
	pricingSvc *pricingservice.PricingService
	queueMgr  *queue.QueueManager
}

func NewMcpHandler(ledgerSvc *service.LedgerService, pricingSvc *pricingservice.PricingService, queueMgr *queue.QueueManager) *McpHandler {
	return &McpHandler{
		ledgerSvc: ledgerSvc,
		pricingSvc: pricingSvc,
		queueMgr:  queueMgr,
	}
}

type JsonRpcRequest struct {
	JsonRpc string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id"`
}

type JsonRpcResponse struct {
	JsonRpc string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

func (h *McpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req JsonRpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON-RPC request", http.StatusBadRequest)
		return
	}

	var result interface{}
	var err error

	switch req.Method {
	case "tools/list":
		result = h.handleListTools()
	case "tools/call":
		result, err = h.handleCallTool(r.Context(), req.Params)
	default:
		err = fmt.Errorf("method not found: %s", req.Method)
	}

	w.Header().Set("Content-Type", "application/json")
	var resp JsonRpcResponse
	resp.JsonRpc = "2.0"
	resp.ID = req.ID

	if err != nil {
		resp.Error = map[string]interface{}{
			"code":    -32603,
			"message": err.Error(),
		}
	} else {
		resp.Result = result
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *McpHandler) handleListTools() interface{} {
	return map[string]interface{}{
		"tools": []map[string]interface{}{
			{
				"name":        "pay",
				"description": "Lock funds in AgentPay channel for an AI invocation, based on expected token usage",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"agentId": map[string]interface{}{
							"type":        "integer",
							"description": "The unique ID of the agent",
						},
						"tokens": map[string]interface{}{
							"type":        "integer",
							"description": "The expected tokens to lock for (e.g., 2000)",
						},
					},
					"required": []string{"agentId", "tokens"},
				},
			},
			{
				"name":        "checkout",
				"description": "Settle and clear the locked invoice with actual consumption cost",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"invoiceId": map[string]interface{}{
							"type":        "string",
							"description": "The Invoice ID returned by pay tool",
						},
						"actualCostUsdc": map[string]interface{}{
							"type":        "number",
							"description": "The actual cost to clear in USDC (e.g. 0.35)",
						},
						"tokens": map[string]interface{}{
							"type":        "integer",
							"description": "The actual tokens consumed (optional, e.g. 1500)",
						},
						"nonce": map[string]interface{}{
							"type":        "string",
							"description": "Idempotent nonce key",
						},
					},
					"required": []string{"invoiceId", "actualCostUsdc"},
				},
			},
		},
	}
}

func (h *McpHandler) handleCallTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var callReq struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callReq); err != nil {
		return nil, err
	}

	if h.ledgerSvc == nil {
		return nil, fmt.Errorf("ledger service is not configured on this node")
	}

	switch callReq.Name {
	case "pay":
		var args struct {
			AgentID int64  `json:"agentId"`
			Tokens  uint64 `json:"tokens"`
		}
		if err := json.Unmarshal(callReq.Arguments, &args); err != nil {
			return nil, err
		}

		var microAmount uint64
		if h.pricingSvc != nil {
			quote, qerr := h.pricingSvc.Quote(ctx, strconv.FormatInt(args.AgentID, 10), args.Tokens, "")
			if qerr != nil {
				return nil, fmt.Errorf("pricing quote failed: %w", qerr)
			}
			microAmount = quote.MicroAmount
		} else {
			// 退回默认的线性计费费率: $0.0015 / 1k + $0.002
			modelCost := (args.Tokens / 1000) * 1500
			if args.Tokens%1000 != 0 {
				modelCost += 1500
			}
			microAmount = (modelCost + 2000) * 10000 / 9000 // 10% Platform Bps
		}

		payer := "mcp_client_default"
		inv, err := h.ledgerSvc.CreateInvoice(ctx, payer, strconv.FormatInt(args.AgentID, 10), microAmount, "crypto")
		if err != nil {
			return nil, err
		}

		checkoutLink := fmt.Sprintf("http://localhost:3003/client.html?agentId=%d&invoiceId=%s", args.AgentID, inv.ID)
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Successfully locked funds. InvoiceID: %s, Locked: %.6f USDC (expected tokens: %d). Please visit this checkout link to complete payment (select USDC Wallet or Stripe Credit Card): %s", inv.ID, float64(microAmount)/1e6, args.Tokens, checkoutLink),
				},
			},
		}, nil

	case "checkout":
		var args struct {
			InvoiceID      string  `json:"invoiceId"`
			ActualCostUsdc float64 `json:"actualCostUsdc"`
			Tokens         uint64  `json:"tokens,omitempty"`
			Nonce          string  `json:"nonce"`
		}
		if err := json.Unmarshal(callReq.Arguments, &args); err != nil {
			return nil, err
		}

		microCost := uint64(args.ActualCostUsdc * 1e6)
		nonceStr := args.Nonce
		if nonceStr == "" {
			nonceStr = fmt.Sprintf("mcp_nonce_%s", args.InvoiceID)
		}

		// 默认分账：80% 分配给智能体所有者，20% 作为平台抽成
		agentOwner := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" // Anvil default
		platformTreasury := "0x15d34AAf54a67C68101F309492526a9000025B7b"

		agentPayout := uint64(float64(microCost) * 0.8)
		platformPayout := microCost - agentPayout

		payouts := []rail.Payout{
			{Target: agentOwner, Amount: agentPayout},
			{Target: platformTreasury, Amount: platformPayout},
		}

		err := h.ledgerSvc.SettleInvoice(ctx, args.InvoiceID, microCost, payouts, nonceStr, "")
		if err != nil {
			return nil, err
		}

		// 结算成功后，记录实际用量到计费引擎中
		if h.pricingSvc != nil {
			// 如果没有传入 tokens，根据 1.5 USDC/1k tokens 的默认配置反推实际用量
			tokensVal := args.Tokens
			if tokensVal == 0 {
				tokensVal = (microCost / 1500) * 1000
			}

			// 获取 Invoice 获取 AgentID 并在记录时传入
			inv, getErr := h.ledgerSvc.GetInvoice(ctx, args.InvoiceID)
			agentIDStr := "888"
			if getErr == nil && inv != nil {
				agentIDStr = inv.Agent
			}

			q := &pricingmodel.Quote{
				AgentID:     agentIDStr,
				MicroAmount: microCost,
			}
			_ = h.pricingSvc.RecordUsage(ctx, agentIDStr, args.InvoiceID, tokensVal, q)
		}

		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Successfully settled invoice %s. Bookkeeping recorded: %.6f USDC distributed.", args.InvoiceID, args.ActualCostUsdc),
				},
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool name: %s", callReq.Name)
	}
}

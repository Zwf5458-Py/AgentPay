package stripe

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type StripeClient struct {
	secretKey  string
	isMock     bool
	httpClient *http.Client
}

func NewStripeClient(secretKey string) *StripeClient {
	isMock := secretKey == "" || strings.HasPrefix(secretKey, "mock_")
	return &StripeClient{
		secretKey: secretKey,
		isMock:    isMock,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CreateCheckoutSession creates a Stripe Checkout Session.
// amountMicro is the amount in micro-units (e.g. 1,000,000 micro-units = 1 USD).
// Stripe Checkout Session requires the amount in cents (1 USD = 100 cents).
func (c *StripeClient) CreateCheckoutSession(amountMicro uint64, successURL, cancelURL string) (string, string, error) {
	if c.isMock {
		// Mock Mode
		sessionID := fmt.Sprintf("cs_mock_%s", randomString(16))
		
		// Append session_id to successURL
		u, err := url.Parse(successURL)
		if err == nil {
			q := u.Query()
			q.Set("session_id", sessionID)
			u.RawQuery = q.Encode()
			successURL = u.String()
		}
		
		fmt.Printf("[Stripe Mock] Created mock checkout session %s\n", sessionID)
		return sessionID, successURL, nil
	}

	// Real Mode: convert micro-units to cents
	cents := amountMicro / 10000
	if cents == 0 && amountMicro > 0 {
		cents = 1
	}

	// Use pure net/http to make a POST request to Stripe API
	data := url.Values{}
	data.Set("payment_method_types[0]", "card")
	data.Set("line_items[0][price_data][currency]", "usd")
	data.Set("line_items[0][price_data][product_data][name]", "Agent Payment")
	data.Set("line_items[0][price_data][unit_amount]", fmt.Sprintf("%d", cents))
	data.Set("line_items[0][quantity]", "1")
	data.Set("mode", "payment")
	data.Set("success_url", successURL)
	data.Set("cancel_url", cancelURL)

	req, err := http.NewRequest("POST", "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(data.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.secretKey))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to execute stripe http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var stripeErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&stripeErr); err == nil && stripeErr.Error.Message != "" {
			return "", "", fmt.Errorf("stripe api error (status %d): %s", resp.StatusCode, stripeErr.Error.Message)
		}
		return "", "", fmt.Errorf("stripe api error (status %d)", resp.StatusCode)
	}

	var sessionResp struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return "", "", fmt.Errorf("failed to decode stripe response: %w", err)
	}

	return sessionResp.ID, sessionResp.URL, nil
}

// VerifyCheckoutSession checks if the checkout session is successfully paid.
func (c *StripeClient) VerifyCheckoutSession(sessionID string) (bool, error) {
	if c.isMock {
		if strings.HasPrefix(sessionID, "cs_mock_") {
			return true, nil
		}
		return false, nil
	}

	// Real Mode: query Stripe API for session details
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.stripe.com/v1/checkout/sessions/%s", sessionID), nil)
	if err != nil {
		return false, fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.secretKey))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to execute stripe http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var stripeErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&stripeErr); err == nil && stripeErr.Error.Message != "" {
			return false, fmt.Errorf("stripe api error (status %d): %s", resp.StatusCode, stripeErr.Error.Message)
		}
		return false, fmt.Errorf("stripe api error (status %d)", resp.StatusCode)
	}

	var sessionResp struct {
		PaymentStatus string `json:"payment_status"` // "paid", "unpaid", "no_payment_required"
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return false, fmt.Errorf("failed to decode stripe response: %w", err)
	}

	return sessionResp.PaymentStatus == "paid", nil
}

func randomString(n int) string {
	bytes := make([]byte, n/2)
	if _, err := rand.Read(bytes); err != nil {
		return "12345678" // fallback
	}
	return hex.EncodeToString(bytes)
}

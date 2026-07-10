package stripe

import (
	"strings"
	"testing"
)

func TestMockStripeClient(t *testing.T) {
	// Initialize with mock key
	client := NewStripeClient("mock_secret_key")

	if !client.isMock {
		t.Fatal("Expected client to be in mock mode when key starts with mock_")
	}

	// Test checkout session creation
	amountMicro := uint64(50000) // 0.05 USD
	successURL := "http://localhost:3000/success"
	cancelURL := "http://localhost:3000/cancel"

	sessionID, checkoutURL, err := client.CreateCheckoutSession(amountMicro, successURL, cancelURL)
	if err != nil {
		t.Fatalf("Unexpected error creating mock session: %v", err)
	}

	if !strings.HasPrefix(sessionID, "cs_mock_") {
		t.Errorf("Expected session ID to start with 'cs_mock_', got %s", sessionID)
	}

	if !strings.Contains(checkoutURL, "session_id="+sessionID) {
		t.Errorf("Expected checkout URL to contain session_id query param, got %s", checkoutURL)
	}

	// Test session verification
	valid, err := client.VerifyCheckoutSession(sessionID)
	if err != nil {
		t.Fatalf("Unexpected error verifying mock session: %v", err)
	}
	if !valid {
		t.Errorf("Expected mock session to be verified as true, got false")
	}

	// Test verification with invalid session ID
	invalid, err := client.VerifyCheckoutSession("cs_invalid_session")
	if err != nil {
		t.Fatalf("Unexpected error verifying invalid mock session: %v", err)
	}
	if invalid {
		t.Errorf("Expected invalid session verification to return false, got true")
	}
}

func TestMockStripeClientEmptyKey(t *testing.T) {
	// Initialize with empty key
	client := NewStripeClient("")

	if !client.isMock {
		t.Fatal("Expected client to be in mock mode when key is empty")
	}
}

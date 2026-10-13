package auth

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSubscriptionPriceIsServerDefined(t *testing.T) {
	if got := subscriptionPrice("monthly"); got != 23750 {
		t.Fatalf("monthly price = %d, want 23750", got)
	}
	if got := subscriptionPrice("yearly"); got != 249799 {
		t.Fatalf("yearly price = %d, want 249799", got)
	}
}

func TestSubscriptionPlanLimits(t *testing.T) {
	if freeMaxUsers != 5 {
		t.Fatalf("free account limit = %d, want 5", freeMaxUsers)
	}
	if premiumMaxUsers != 10 {
		t.Fatalf("Starter Pack account limit = %d, want 10", premiumMaxUsers)
	}
	if premiumMaxStores != 3 {
		t.Fatalf("Starter Pack store limit = %d, want 3", premiumMaxStores)
	}
}

func TestSubscriptionOrderIDFitsMidtransLimit(t *testing.T) {
	const maxMidtransOrderIDLength = 50
	orderID := newSubscriptionOrderID()
	if len(orderID) > maxMidtransOrderIDLength {
		t.Fatalf("subscription order ID length = %d, maximum is %d", len(orderID), maxMidtransOrderIDLength)
	}
	if len(orderID) <= len("LUMI-") {
		t.Fatalf("subscription order ID is missing its unique suffix: %q", orderID)
	}
}

func TestSameGrossAmountAllowsEquivalentDecimalFormatting(t *testing.T) {
	if !sameGrossAmount("23750.00", "23750") {
		t.Fatal("equivalent rupiah amounts should match")
	}
	if sameGrossAmount("23750.00", "23750.01") {
		t.Fatal("different gross amounts must not match")
	}
	if sameGrossAmount("NaN", "NaN") {
		t.Fatal("non-finite gross amounts must not match")
	}
}

func TestSubscriptionPeriodEndClampsMonthAndLeapYear(t *testing.T) {
	location := time.UTC
	monthly := subscriptionPeriodEnd(time.Date(2026, time.January, 31, 12, 30, 0, 0, location), "monthly")
	if want := time.Date(2026, time.February, 28, 12, 30, 0, 0, location); !monthly.Equal(want) {
		t.Fatalf("monthly end = %s, want %s", monthly, want)
	}
	yearly := subscriptionPeriodEnd(time.Date(2024, time.February, 29, 12, 30, 0, 0, location), "yearly")
	if want := time.Date(2025, time.February, 28, 12, 30, 0, 0, location); !yearly.Equal(want) {
		t.Fatalf("yearly end = %s, want %s", yearly, want)
	}
}

func TestValidMidtransSignature(t *testing.T) {
	const serverKey = "sandbox-test-key"
	notification := midtransNotification{
		OrderID:     "LUMI-test-order",
		StatusCode:  "200",
		GrossAmount: "23750.00",
	}
	digest := sha512.Sum512([]byte(notification.OrderID + notification.StatusCode + notification.GrossAmount + serverKey))
	notification.SignatureKey = hex.EncodeToString(digest[:])
	if !validMidtransSignature(notification, serverKey) {
		t.Fatal("valid signature was rejected")
	}
	notification.GrossAmount = "23751.00"
	if validMidtransSignature(notification, serverKey) {
		t.Fatal("signature for a modified amount was accepted")
	}
}

func TestRequestSnapTransactionUsesServerAmount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "sandbox-key" || password != "" {
			t.Errorf("unexpected Midtrans basic auth")
		}
		var body snapRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.TransactionDetails.GrossAmount != yearlyPremiumPrice {
			t.Errorf("gross amount = %d, want %d", body.TransactionDetails.GrossAmount, yearlyPremiumPrice)
		}
		if len(body.ItemDetails) != 1 || body.ItemDetails[0].Price != yearlyPremiumPrice {
			t.Errorf("unexpected item details: %+v", body.ItemDetails)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"snap-token","redirect_url":"https://example.test/snap"}`))
	}))
	defer server.Close()

	result, err := requestSnapTransaction(t.Context(), server.Client(), "sandbox-key", server.URL, "order-1", yearlyPremiumPrice, "yearly", "Account", "account@example.test")
	if err != nil {
		t.Fatalf("requestSnapTransaction: %v", err)
	}
	if result.Token != "snap-token" || result.RedirectURL != "https://example.test/snap" {
		t.Fatalf("unexpected Snap response: %+v", result)
	}
}

func TestFetchMidtransTransactionStatusUsesAuthenticatedStatusEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/order-123/status" {
			t.Errorf("unexpected status request: %s %s", r.Method, r.URL.Path)
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "sandbox-key" || password != "" {
			t.Errorf("unexpected Midtrans basic auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"order_id":"order-123","status_code":"200","gross_amount":"23750.00","transaction_status":"settlement","fraud_status":"accept","transaction_id":"tx-1","payment_type":"bank_transfer"}`))
	}))
	defer server.Close()

	status, err := fetchMidtransTransactionStatus(t.Context(), server.Client(), "sandbox-key", server.URL+"/v2/", "order-123")
	if err != nil {
		t.Fatalf("fetchMidtransTransactionStatus: %v", err)
	}
	if status.OrderID != "order-123" || status.TransactionStatus != "settlement" || status.GrossAmount != "23750.00" {
		t.Fatalf("unexpected transaction status: %+v", status)
	}
}

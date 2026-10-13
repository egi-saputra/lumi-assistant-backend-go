package handlers

import (
	"strings"
	"testing"
)

func TestValidateCreateOrder(t *testing.T) {
	validItem := createOrderItem{ProductID: strings.Repeat("a", 32), Quantity: 1}
	validStart := "2026-10-08"
	tests := []struct {
		name    string
		request createOrderRequest
		wantErr bool
	}{
		{
			name: "valid cash order",
			request: createOrderRequest{
				Code: "INV-123", PaymentMethod: "cash", DiscountType: "percent",
				Items: []createOrderItem{validItem},
			},
		},
		{
			name: "valid ULID product ID",
			request: createOrderRequest{
				Code: "INV-ULID", PaymentMethod: "cash",
				Items: []createOrderItem{{ProductID: strings.Repeat("a", 26), Quantity: 1}},
			},
		},
		{
			name: "valid installment order",
			request: createOrderRequest{
				Code: "INV-123", PaymentMethod: "installment", InstallmentTenor: 3,
				InstallmentStart: &validStart, Items: []createOrderItem{validItem},
			},
		},
		{name: "missing items", request: createOrderRequest{Code: "INV", PaymentMethod: "cash"}, wantErr: true},
		{
			name:    "invalid product id",
			request: createOrderRequest{Code: "INV", PaymentMethod: "cash", Items: []createOrderItem{{ProductID: "1", Quantity: 1}}},
			wantErr: true,
		},
		{
			name:    "zero quantity",
			request: createOrderRequest{Code: "INV", PaymentMethod: "cash", Items: []createOrderItem{{ProductID: strings.Repeat("a", 32)}}},
			wantErr: true,
		},
		{
			name:    "installment without start date",
			request: createOrderRequest{Code: "INV", PaymentMethod: "installment", InstallmentTenor: 3, Items: []createOrderItem{validItem}},
			wantErr: true,
		},
		{
			name:    "installment too long",
			request: createOrderRequest{Code: "INV", PaymentMethod: "installment", InstallmentTenor: 25, InstallmentStart: &validStart, Items: []createOrderItem{validItem}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateCreateOrder(&test.request)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateCreateOrder() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestCalculateOrderDiscount(t *testing.T) {
	tests := []struct {
		name       string
		subtotal   int64
		kind       string
		discount   int64
		wantAmount int64
		wantErr    bool
	}{
		{name: "percent", subtotal: 12345, kind: "percent", discount: 10, wantAmount: 1234},
		{name: "amount", subtotal: 10000, kind: "amount", discount: 1250, wantAmount: 1250},
		{name: "percent over limit", subtotal: 1000, kind: "percent", discount: 101, wantErr: true},
		{name: "amount over subtotal", subtotal: 1000, kind: "amount", discount: 1001, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := calculateOrderDiscount(test.subtotal, test.kind, test.discount)
			if (err != nil) != test.wantErr {
				t.Fatalf("calculateOrderDiscount() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.wantAmount {
				t.Fatalf("calculateOrderDiscount() = %d, want %d", got, test.wantAmount)
			}
		})
	}
}

func TestOrderArithmetic(t *testing.T) {
	if got := percentOf(12345, 10); got != 1234 {
		t.Fatalf("percentOf() = %d, want 1234", got)
	}
	if got := proportionalAmount(100, 1, 3); got != 33 {
		t.Fatalf("proportionalAmount() = %d, want 33", got)
	}
	if _, ok := multiplyNonNegative(1<<62, 4); ok {
		t.Fatal("multiplyNonNegative accepted an overflow")
	}
	if _, ok := multiplySigned(1<<62, 4); ok {
		t.Fatal("multiplySigned accepted an overflow")
	}
}

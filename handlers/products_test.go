package handlers

import (
	"strings"
	"testing"
)

func TestValidateProduct(t *testing.T) {
	tests := []struct {
		name     string
		request  productRequest
		creating bool
		wantErr  bool
	}{
		{name: "valid defaults", request: productRequest{Name: "Produk", Price: 10000}},
		{name: "missing name", request: productRequest{Price: 10000}, wantErr: true},
		{name: "name too long", request: productRequest{Name: strings.Repeat("a", 256)}, wantErr: true},
		{name: "negative price", request: productRequest{Name: "Produk", Price: -1}, wantErr: true},
		{name: "negative initial stock", request: productRequest{Name: "Produk", Stock: -1}, creating: true, wantErr: true},
		{name: "unsupported valuation", request: productRequest{Name: "Produk", ValuationMethod: "LIFO"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateProduct(&test.request, test.creating)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateProduct() error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && (test.request.Unit != "item" || test.request.ValuationMethod != "AVG") {
				t.Fatalf("defaults were not applied: %+v", test.request)
			}
		})
	}
}

func TestValidateStockMovement(t *testing.T) {
	tests := []struct {
		name    string
		request stockMovementRequest
		wantErr bool
	}{
		{name: "valid inbound", request: stockMovementRequest{Type: "IN", Quantity: 1, Note: "Restock"}},
		{name: "valid outbound", request: stockMovementRequest{Type: "OUT", Quantity: 2, Note: "Rusak"}},
		{name: "invalid type", request: stockMovementRequest{Type: "ADJUSTMENT", Quantity: 1, Note: "Setel"}, wantErr: true},
		{name: "zero quantity", request: stockMovementRequest{Type: "IN", Note: "Restock"}, wantErr: true},
		{name: "missing note", request: stockMovementRequest{Type: "IN", Quantity: 1}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.request.Type = strings.ToUpper(strings.TrimSpace(test.request.Type))
			test.request.Note = strings.TrimSpace(test.request.Note)
			err := validateStockMovement(test.request)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateStockMovement() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestParseBoundedInt(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "valid", value: "30", want: 30},
		{name: "below minimum", value: "-1", wantErr: true},
		{name: "above maximum", value: "101", wantErr: true},
		{name: "trailing characters", value: "30x", wantErr: true},
		{name: "not an integer", value: "abc", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseBoundedInt(test.value, 0, 100)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseBoundedInt() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("parseBoundedInt() = %d, want %d", got, test.want)
			}
		})
	}
}

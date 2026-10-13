package handlers

import (
	"strings"
	"testing"
)

func TestValidateExpense(t *testing.T) {
	tests := []struct {
		name    string
		request expenseRequest
		wantErr bool
	}{
		{name: "valid", request: expenseRequest{Name: "Operasional", Amount: 15000}},
		{name: "missing name", request: expenseRequest{Amount: 15000}, wantErr: true},
		{name: "name too long", request: expenseRequest{Name: strings.Repeat("a", 256), Amount: 15000}, wantErr: true},
		{name: "zero amount", request: expenseRequest{Name: "Operasional"}, wantErr: true},
		{name: "negative amount", request: expenseRequest{Name: "Operasional", Amount: -1}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateExpense(test.request)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateExpense() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

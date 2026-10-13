package handlers

import (
	"strings"
	"testing"
)

func TestValidateContactTrimsFields(t *testing.T) {
	request := contactRequest{
		Name:    "  Pelanggan  ",
		Phone:   " 08123456789 ",
		Address: "  Jakarta ",
	}

	if err := validateContact(&request); err != nil {
		t.Fatalf("validateContact returned error: %v", err)
	}
	if request.Name != "Pelanggan" || request.Phone != "08123456789" || request.Address != "Jakarta" {
		t.Fatalf("validateContact did not trim fields: %+v", request)
	}
}

func TestValidateContactRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		request contactRequest
	}{
		{name: "missing name", request: contactRequest{Phone: "08123456789"}},
		{name: "missing phone", request: contactRequest{Name: "Pelanggan"}},
		{name: "name too long", request: contactRequest{Name: strings.Repeat("a", 256), Phone: "08123456789"}},
		{name: "phone too long", request: contactRequest{Name: "Pelanggan", Phone: strings.Repeat("1", 21)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateContact(&test.request); err == nil {
				t.Fatal("validateContact accepted invalid contact")
			}
		})
	}
}

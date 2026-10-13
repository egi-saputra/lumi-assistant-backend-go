package handlers

import "testing"

func TestValidateSocialLink(t *testing.T) {
	tests := []struct {
		name    string
		request socialLinkRequest
		wantErr bool
	}{
		{name: "valid", request: socialLinkRequest{Platform: "Instagram", Username: "toko", URL: "https://instagram.com/toko"}},
		{name: "missing platform", request: socialLinkRequest{Username: "toko", URL: "https://example.com"}, wantErr: true},
		{name: "URL too long", request: socialLinkRequest{Platform: "Instagram", Username: "toko", URL: string(make([]byte, 2001))}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateSocialLink(test.request); (err != nil) != test.wantErr {
				t.Fatalf("validateSocialLink() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidatePaymentMethod(t *testing.T) {
	valid := paymentMethodRequest{
		MethodType: "bank", ProviderName: "BCA",
		AccountNumber: "123456", AccountHolder: "Pemilik",
	}
	if err := validatePaymentMethod(valid); err != nil {
		t.Fatalf("valid payment method rejected: %v", err)
	}
	valid.MethodType = "crypto"
	if err := validatePaymentMethod(valid); err == nil {
		t.Fatal("invalid payment method type accepted")
	}
}

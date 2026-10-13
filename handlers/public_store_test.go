package handlers

import "testing"

func TestStoreSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "normalizes spaces and punctuation", in: "Toko Maju Jaya!", want: "toko-maju-jaya"},
		{name: "keeps unicode letters", in: "Café Cemerlang", want: "café-cemerlang"},
		{name: "trims separators", in: "  Toko---Utama  ", want: "toko-utama"},
		{name: "empty name", in: " ! ", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := storeSlug(test.in); got != test.want {
				t.Fatalf("storeSlug(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestValidStoreLogoPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "valid jpg", path: "tenant-123/tenant-123-logo-456.jpg", want: true},
		{name: "valid webp", path: "tenant-123/tenant-123-logo-456.webp", want: true},
		{name: "wrong tenant directory", path: "tenant-456/tenant-123-logo-456.jpg", want: false},
		{name: "path traversal", path: "tenant-123/../tenant-123-logo-456.jpg", want: false},
		{name: "wrong filename prefix", path: "tenant-123/logo-456.jpg", want: false},
		{name: "unsupported extension", path: "tenant-123/tenant-123-logo-456.svg", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validStoreLogoPath("tenant-123", test.path); got != test.want {
				t.Fatalf("validStoreLogoPath(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestPublicProductImageProxyURL(t *testing.T) {
	if got := publicProductImageProxyURL("shop name", "product-456"); got != "/public/stores/shop%20name/product-images/product-456" {
		t.Fatalf("publicProductImageProxyURL() = %q", got)
	}
}

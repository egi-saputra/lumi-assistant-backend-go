package storage

import "testing"

func TestValidKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{key: "product-images/tenant-1/product-1-image.jpg", want: true},
		{key: "store-logos/tenant-1/logo-1.png", want: true},
		{key: "", want: false},
		{key: "../outside.jpg", want: false},
		{key: "C:/outside.jpg", want: false},
		{key: "product-images//outside.jpg", want: false},
		{key: "product-images/./outside.jpg", want: false},
		{key: "product-images/../outside.jpg", want: false},
		{key: "product-images\\outside.jpg", want: false},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			if got := validKey(test.key); got != test.want {
				t.Fatalf("validKey(%q) = %t, want %t", test.key, got, test.want)
			}
		})
	}
}

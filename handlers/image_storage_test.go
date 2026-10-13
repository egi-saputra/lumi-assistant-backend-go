package handlers

import "testing"

func TestSupportedImageContentType(t *testing.T) {
	tests := []struct {
		name        string
		image       []byte
		contentType string
		want        bool
	}{
		{name: "png", image: []byte("\x89PNG\r\n\x1a\n"), contentType: "image/png", want: true},
		{name: "jpeg", image: []byte("\xff\xd8\xff"), contentType: "image/jpeg", want: true},
		{name: "webp", image: []byte("RIFF\x04\x00\x00\x00WEBP"), contentType: "image/webp", want: true},
		{name: "unsupported", image: []byte("<script>alert(1)</script>"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contentType, supported := supportedImageContentType(test.image)
			if supported != test.want || contentType != test.contentType {
				t.Fatalf("supportedImageContentType() = (%q, %t), want (%q, %t)",
					contentType, supported, test.contentType, test.want)
			}
		})
	}
}

package handlers

import (
	"encoding/base64"
	"testing"
)

func TestParseAdMobSignedQuery(t *testing.T) {
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))

	tests := []struct {
		name      string
		rawQuery  string
		wantData  string
		wantKeyID int64
		wantErr   bool
	}{
		{
			name:      "valid signed query",
			rawQuery:  "ad_unit=unit&custom_data=attempt&signature=" + signature + "&key_id=17",
			wantData:  "ad_unit=unit&custom_data=attempt",
			wantKeyID: 17,
		},
		{
			name:     "signature is missing",
			rawQuery: "ad_unit=unit&key_id=17",
			wantErr:  true,
		},
		{
			name:     "key id is malformed",
			rawQuery: "ad_unit=unit&signature=" + signature + "&key_id=0",
			wantErr:  true,
		},
		{
			name:     "signature encoding is malformed",
			rawQuery: "ad_unit=unit&signature=*&key_id=17",
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signedData, keyID, parsedSignature, err := parseAdMobSignedQuery(test.rawQuery)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseAdMobSignedQuery() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if signedData != test.wantData {
				t.Errorf("signed data = %q, want %q", signedData, test.wantData)
			}
			if keyID != test.wantKeyID {
				t.Errorf("key ID = %d, want %d", keyID, test.wantKeyID)
			}
			if string(parsedSignature) != "signature" {
				t.Errorf("signature = %q, want %q", parsedSignature, "signature")
			}
		})
	}
}

func TestFreeInvoiceQuota(t *testing.T) {
	tests := []struct {
		name          string
		used          int
		bonus         int
		wantLimit     int
		wantRemaining int
	}{
		{name: "base quota", used: 0, wantLimit: 3, wantRemaining: 3},
		{name: "bonus credits", used: 4, bonus: 3, wantLimit: 6, wantRemaining: 2},
		{name: "exhausted quota", used: 6, bonus: 3, wantLimit: 6},
		{name: "usage beyond limit", used: 8, bonus: 3, wantLimit: 6},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quota := freeInvoiceQuota(test.used, test.bonus)
			if quota.Plan != "free" || quota.Used != test.used || quota.BonusCredits != test.bonus {
				t.Errorf("unexpected quota details: %+v", quota)
			}
			if quota.Limit != test.wantLimit || quota.Remaining != test.wantRemaining {
				t.Errorf("limit/remaining = %d/%d, want %d/%d", quota.Limit, quota.Remaining, test.wantLimit, test.wantRemaining)
			}
		})
	}
}

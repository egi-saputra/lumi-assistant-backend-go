package utils

import "strings"

func NormalizePhone(p string) string {
	phone := strings.TrimSpace(p)
	if strings.HasPrefix(phone, "0") {
		phone = "62" + phone[1:]
	}
	return phone
}

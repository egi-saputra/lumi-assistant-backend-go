package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

func SendWhatsapp(phone, message string) {
	url := "http://localhost:3001/send-message"

	payload := map[string]string{
		"phone":   phone,
		"message": message,
	}

	body, _ := json.Marshal(payload)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Println("Failed to send WhatsApp:", err)
		return
	}
	defer resp.Body.Close()
}

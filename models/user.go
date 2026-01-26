package models

type User struct {
    ID       int    `json:"id"`
    Name string `json:"name"`
    Email string `json:"email"`
    Phone string `json:"phone"`
    Password string `json:"-"`
    Role     string `json:"role"`
}

type WhatsappOtp struct {
    ID        int
    Phone     string
    OTP       string
    ExpiresAt string
}

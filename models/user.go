package models

type User struct {
	ID string `json:"id" db:"id"` // ULID

	TenantID string `json:"tenant_id" db:"tenant_id"` // ULID

	Name  string `json:"name" db:"name"`
	Email string `json:"email,omitempty" db:"email"`
	Phone string `json:"phone" db:"phone"`

	PasswordHash string `json:"-" db:"password_hash"`
	Role         string `json:"role" db:"role"`

	CreatedAt string `json:"created_at,omitempty" db:"created_at"`
}
package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"assistant/utils"

	"github.com/gin-gonic/gin"
)

type contactRequest struct {
	Name    string `json:"name" binding:"required"`
	Phone   string `json:"phone" binding:"required"`
	Address string `json:"address"`
}

type contactResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Address   *string   `json:"address"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func ListContacts(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	rows, err := conn.Query(`
		SELECT id, name, phone, address, created_at, updated_at
		FROM contact
		ORDER BY name ASC
	`)
	if err != nil {
		log.Printf("list tenant contacts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load contacts"})
		return
	}
	defer rows.Close()

	contacts := make([]contactResponse, 0)
	for rows.Next() {
		var contact contactResponse
		var address sql.NullString
		if err := rows.Scan(
			&contact.ID,
			&contact.Name,
			&contact.Phone,
			&address,
			&contact.CreatedAt,
			&contact.UpdatedAt,
		); err != nil {
			log.Printf("scan tenant contact: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load contacts"})
			return
		}
		if address.Valid {
			contact.Address = &address.String
		}
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate tenant contacts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load contacts"})
		return
	}

	c.JSON(http.StatusOK, contacts)
}

func CreateContact(c *gin.Context) {
	var request contactRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and phone are required"})
		return
	}
	if err := validateContact(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().UTC()
	id := utils.NewULID()
	conn := c.MustGet("tenant_db").(*sql.DB)
	if _, err := conn.Exec(`
		INSERT INTO contact (id, name, phone, address, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, request.Name, request.Phone, nullableContactAddress(request.Address), now, now); err != nil {
		log.Printf("create tenant contact: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create contact"})
		return
	}

	c.JSON(http.StatusCreated, contactResponse{
		ID: id, Name: request.Name, Phone: request.Phone,
		Address: nullableContactAddress(request.Address), CreatedAt: now, UpdatedAt: now,
	})
}

func UpdateContact(c *gin.Context) {
	id := c.Param("id")
	if id == "" || len(id) > 32 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid contact ID"})
		return
	}

	var request contactRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and phone are required"})
		return
	}
	if err := validateContact(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().UTC()
	conn := c.MustGet("tenant_db").(*sql.DB)
	var createdAt time.Time
	if err := conn.QueryRow("SELECT created_at FROM contact WHERE id = ?", id).Scan(&createdAt); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Contact not found"})
		} else {
			log.Printf("load tenant contact before update: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update contact"})
		}
		return
	}
	if _, err := conn.Exec(
		`
		UPDATE contact SET name = ?, phone = ?, address = ?, updated_at = ?
		WHERE id = ?
		`,
		request.Name, request.Phone, nullableContactAddress(request.Address), now, id,
	); err != nil {
		log.Printf("update tenant contact: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update contact"})
		return
	}

	c.JSON(http.StatusOK, contactResponse{
		ID: id, Name: request.Name, Phone: request.Phone,
		Address: nullableContactAddress(request.Address), CreatedAt: createdAt, UpdatedAt: now,
	})
}

func DeleteContact(c *gin.Context) {
	id := c.Param("id")
	if id == "" || len(id) > 32 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid contact ID"})
		return
	}

	conn := c.MustGet("tenant_db").(*sql.DB)
	result, err := conn.Exec("DELETE FROM contact WHERE id = ?", id)
	if err != nil {
		log.Printf("delete tenant contact: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete contact"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		log.Printf("check deleted tenant contact: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete contact"})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Contact not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func validateContact(request *contactRequest) error {
	request.Name = strings.TrimSpace(request.Name)
	request.Phone = strings.TrimSpace(request.Phone)
	request.Address = strings.TrimSpace(request.Address)
	if request.Name == "" || request.Phone == "" {
		return errors.New("name and phone are required")
	}
	if utf8.RuneCountInString(request.Name) > 255 || utf8.RuneCountInString(request.Phone) > 20 {
		return errors.New("name must be at most 255 characters and phone at most 20 characters")
	}
	return nil
}

func nullableContactAddress(address string) *string {
	if address == "" {
		return nil
	}
	return &address
}

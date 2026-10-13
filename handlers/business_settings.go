package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

type businessProfileResponse struct {
	Name    string  `json:"name"`
	Store   string  `json:"store"`
	Phone   string  `json:"phone"`
	Address *string `json:"address"`
}

type businessProfileRequest struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}

type socialLinkRequest struct {
	Platform string `json:"platform" binding:"required"`
	Username string `json:"username" binding:"required"`
	URL      string `json:"url" binding:"required"`
}

type socialLinkResponse struct {
	ID       string  `json:"id"`
	Platform string  `json:"platform"`
	Username string  `json:"username"`
	URL      *string `json:"url"`
}

type paymentMethodRequest struct {
	MethodType    string `json:"method_type" binding:"required"`
	ProviderName  string `json:"provider_name" binding:"required"`
	AccountNumber string `json:"account_number" binding:"required"`
	AccountHolder string `json:"account_holder" binding:"required"`
}

type paymentMethodResponse struct {
	ID            string `json:"id"`
	MethodType    string `json:"method_type"`
	ProviderName  string `json:"provider_name"`
	AccountNumber string `json:"account_number"`
	AccountHolder string `json:"account_holder"`
}

func GetBusinessProfile(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	tenantID := c.GetString("tenant_id")
	var response businessProfileResponse
	var userPhone sql.NullString
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT name, phone FROM users
		WHERE tenant_id = ? AND role = 'owner'
		ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END, created_at
		LIMIT 1
	`, tenantID, c.GetString("user_id")).Scan(&response.Name, &userPhone); err != nil {
		log.Printf("load business profile owner: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load business profile"})
		return
	}
	if userPhone.Valid {
		response.Phone = userPhone.String
	}
	var tenantName string
	if err := tenantNameByID(tenantID, &tenantName); err != nil {
		log.Printf("load business profile tenant name: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load business profile"})
		return
	}
	response.Store = tenantName
	var phone, address sql.NullString
	err := conn.QueryRowContext(c.Request.Context(), `
		SELECT phone, address FROM profile ORDER BY created_at LIMIT 1
	`).Scan(&phone, &address)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("load business profile details: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load business profile"})
		return
	}
	if phone.Valid && phone.String != "" {
		response.Phone = phone.String
	}
	if address.Valid {
		response.Address = &address.String
	}
	c.JSON(http.StatusOK, response)
}

func UpdateBusinessProfile(c *gin.Context) {
	var request businessProfileRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid business profile"})
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Phone = strings.TrimSpace(request.Phone)
	request.Address = strings.TrimSpace(request.Address)
	if utf8.RuneCountInString(request.Name) > 100 ||
		utf8.RuneCountInString(request.Phone) > 20 ||
		utf8.RuneCountInString(request.Address) > 5000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name, phone, or address is too long"})
		return
	}
	conn := c.MustGet("tenant_db").(*sql.DB)
	profileID, err := ensureBusinessProfile(c, conn)
	if err != nil {
		log.Printf("ensure business profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save business profile"})
		return
	}
	if request.Name == "" {
		if _, err := conn.ExecContext(c.Request.Context(), `
			UPDATE profile SET phone = ?, address = ?, updated_at = ? WHERE id = ?
		`, request.Phone, request.Address, time.Now().UTC(), profileID); err != nil {
			log.Printf("update business profile: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save business profile"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Business profile updated"})
		return
	}
	ownerID := c.GetString("user_id")
	tenantTx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant owner profile update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save business profile"})
		return
	}
	defer tenantTx.Rollback()
	masterTx, err := db.Master.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin account owner profile update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save owner name"})
		return
	}
	defer masterTx.Rollback()
	var ownerExists bool
	if err := tenantTx.QueryRowContext(c.Request.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM users
			WHERE id = ? AND tenant_id = ? AND role = 'owner'
		)
	`, ownerID, c.GetString("tenant_id")).Scan(&ownerExists); err != nil {
		log.Printf("verify tenant owner profile update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify owner access"})
		return
	}
	if !ownerExists {
		c.JSON(http.StatusForbidden, gin.H{"error": "Owner access is required to update the owner name"})
		return
	}
	var accountExists bool
	if err := masterTx.QueryRowContext(c.Request.Context(), `
		SELECT EXISTS(SELECT 1 FROM accounts WHERE id = ?)
	`, ownerID).Scan(&accountExists); err != nil {
		log.Printf("verify owner account profile update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify owner account"})
		return
	}
	if !accountExists {
		log.Printf("owner account not found for profile update: %s", ownerID)
		c.JSON(http.StatusConflict, gin.H{"error": "Owner account is not linked to this store. Please contact support."})
		return
	}
	if _, err := tenantTx.ExecContext(c.Request.Context(), `
		UPDATE users SET name = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND role = 'owner'
	`, request.Name, time.Now().UTC(), ownerID, c.GetString("tenant_id")); err != nil {
		log.Printf("update tenant owner name: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save owner name"})
		return
	}
	if _, err := masterTx.ExecContext(c.Request.Context(), `
		UPDATE accounts SET name = ?, updated_at = ? WHERE id = ?
	`, request.Name, time.Now().UTC(), ownerID); err != nil {
		log.Printf("update owner account name: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save owner name"})
		return
	}
	if _, err := tenantTx.ExecContext(c.Request.Context(), `
		UPDATE profile SET name = ?, phone = ?, address = ?, updated_at = ? WHERE id = ?
	`, request.Name, request.Phone, request.Address, time.Now().UTC(), profileID); err != nil {
		log.Printf("update business profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save business profile"})
		return
	}
	if err := tenantTx.Commit(); err != nil {
		log.Printf("commit tenant business profile update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save business profile"})
		return
	}
	if err := masterTx.Commit(); err != nil {
		log.Printf("commit owner account name update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Owner name was updated for this store but not the account. Please retry."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Business profile updated"})
}

func ListSocialLinks(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	rows, err := conn.QueryContext(c.Request.Context(), `
		SELECT link.id, link.platform, link.username, link.url
		FROM social_links AS link
		JOIN profile ON profile.id = link.profile_id
		ORDER BY link.created_at DESC
	`)
	if err != nil {
		log.Printf("list social links: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load social links"})
		return
	}
	defer rows.Close()
	links := make([]socialLinkResponse, 0)
	for rows.Next() {
		var link socialLinkResponse
		var url sql.NullString
		if err := rows.Scan(&link.ID, &link.Platform, &link.Username, &url); err != nil {
			log.Printf("scan social link: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load social links"})
			return
		}
		link.URL = nullableString(url)
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate social links: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load social links"})
		return
	}
	c.JSON(http.StatusOK, links)
}

func SaveSocialLink(c *gin.Context) {
	var request socialLinkRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Platform, username and URL are required"})
		return
	}
	request.Platform = strings.TrimSpace(request.Platform)
	request.Username = strings.TrimSpace(request.Username)
	request.URL = strings.TrimSpace(request.URL)
	if err := validateSocialLink(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	conn := c.MustGet("tenant_db").(*sql.DB)
	profileID, err := ensureBusinessProfile(c, conn)
	if err != nil {
		log.Printf("ensure profile for social link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save social link"})
		return
	}
	var linkID string
	err = conn.QueryRowContext(c.Request.Context(), `
		SELECT id FROM social_links WHERE profile_id = ? ORDER BY created_at LIMIT 1
	`, profileID).Scan(&linkID)
	if err == sql.ErrNoRows {
		linkID = utils.NewULID()
		now := time.Now().UTC()
		_, err = conn.ExecContext(c.Request.Context(), `
			INSERT INTO social_links (id, profile_id, platform, username, url, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, linkID, profileID, request.Platform, request.Username, request.URL, now, now)
	} else if err == nil {
		_, err = conn.ExecContext(c.Request.Context(), `
			UPDATE social_links SET platform = ?, username = ?, url = ?, updated_at = ?
			WHERE id = ? AND profile_id = ?
		`, request.Platform, request.Username, request.URL, time.Now().UTC(), linkID, profileID)
	}
	if err != nil {
		log.Printf("save social link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save social link"})
		return
	}
	c.JSON(http.StatusOK, socialLinkResponse{
		ID: linkID, Platform: request.Platform, Username: request.Username, URL: &request.URL,
	})
}

func DeleteSocialLink(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	result, err := conn.ExecContext(c.Request.Context(), "DELETE FROM social_links WHERE id = ?", c.Param("id"))
	if err != nil {
		log.Printf("delete social link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete social link"})
		return
	}
	if affected, err := result.RowsAffected(); err != nil {
		log.Printf("check deleted social link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete social link"})
		return
	} else if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Social link not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func ListPaymentMethods(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	rows, err := conn.QueryContext(c.Request.Context(), `
		SELECT id, method_type, provider_name, account_number, account_holder
		FROM payments ORDER BY created_at DESC
	`)
	if err != nil {
		log.Printf("list payment methods: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load payment methods"})
		return
	}
	defer rows.Close()
	methods := make([]paymentMethodResponse, 0)
	for rows.Next() {
		var method paymentMethodResponse
		var accountNumber, accountHolder sql.NullString
		if err := rows.Scan(
			&method.ID, &method.MethodType, &method.ProviderName,
			&accountNumber, &accountHolder,
		); err != nil {
			log.Printf("scan payment method: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load payment methods"})
			return
		}
		if accountNumber.Valid {
			method.AccountNumber = accountNumber.String
		}
		if accountHolder.Valid {
			method.AccountHolder = accountHolder.String
		}
		methods = append(methods, method)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate payment methods: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load payment methods"})
		return
	}
	c.JSON(http.StatusOK, methods)
}

func CreatePaymentMethod(c *gin.Context) {
	var request paymentMethodRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Payment method details are required"})
		return
	}
	request.MethodType = strings.ToLower(strings.TrimSpace(request.MethodType))
	request.ProviderName = strings.TrimSpace(request.ProviderName)
	request.AccountNumber = strings.TrimSpace(request.AccountNumber)
	request.AccountHolder = strings.TrimSpace(request.AccountHolder)
	if err := validatePaymentMethod(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	conn := c.MustGet("tenant_db").(*sql.DB)
	profileID, err := ensureBusinessProfile(c, conn)
	if err != nil {
		log.Printf("ensure profile for payment method: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create payment method"})
		return
	}
	now := time.Now().UTC()
	method := paymentMethodResponse{
		ID: utils.NewULID(), MethodType: request.MethodType,
		ProviderName: request.ProviderName, AccountNumber: request.AccountNumber,
		AccountHolder: request.AccountHolder,
	}
	if _, err := conn.ExecContext(c.Request.Context(), `
		INSERT INTO payments (
			id, profile_id, method_type, provider_name, account_number, account_holder,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, method.ID, profileID, method.MethodType, method.ProviderName,
		method.AccountNumber, method.AccountHolder, now, now); err != nil {
		log.Printf("insert payment method: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create payment method"})
		return
	}
	c.JSON(http.StatusCreated, method)
}

func DeletePaymentMethod(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	result, err := conn.ExecContext(c.Request.Context(), "DELETE FROM payments WHERE id = ?", c.Param("id"))
	if err != nil {
		log.Printf("delete payment method: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete payment method"})
		return
	}
	if affected, err := result.RowsAffected(); err != nil {
		log.Printf("check deleted payment method: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete payment method"})
		return
	} else if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment method not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func ensureBusinessProfile(c *gin.Context, conn *sql.DB) (string, error) {
	var profileID string
	err := conn.QueryRowContext(c.Request.Context(), `
		SELECT id FROM profile ORDER BY created_at LIMIT 1
	`).Scan(&profileID)
	if err == nil {
		return profileID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var name string
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT name FROM users WHERE tenant_id = ? AND role = 'owner'
		ORDER BY created_at LIMIT 1
	`, c.GetString("tenant_id")).Scan(&name); err != nil {
		return "", err
	}
	var store string
	if err := tenantNameByID(c.GetString("tenant_id"), &store); err != nil {
		return "", err
	}
	profileID = utils.NewULID()
	now := time.Now().UTC()
	_, err = conn.ExecContext(c.Request.Context(), `
		INSERT INTO profile (id, name, store, phone, address, file_path, created_at, updated_at)
		VALUES (?, ?, ?, '', NULL, NULL, ?, ?)
	`, profileID, name, store, now, now)
	return profileID, err
}

func tenantNameByID(tenantID string, name *string) error {
	return db.Master.QueryRow("SELECT name FROM tenants WHERE id = ?", tenantID).Scan(name)
}

func validateSocialLink(request socialLinkRequest) error {
	if request.Platform == "" || utf8.RuneCountInString(request.Platform) > 100 {
		return errors.New("platform must be between 1 and 100 characters")
	}
	if request.Username == "" || utf8.RuneCountInString(request.Username) > 255 {
		return errors.New("username must be between 1 and 255 characters")
	}
	if request.URL == "" || utf8.RuneCountInString(request.URL) > 2000 {
		return errors.New("URL must be between 1 and 2000 characters")
	}
	return nil
}

func validatePaymentMethod(request paymentMethodRequest) error {
	if request.MethodType != "bank" && request.MethodType != "ewallet" {
		return errors.New("method_type must be bank or ewallet")
	}
	if request.ProviderName == "" || utf8.RuneCountInString(request.ProviderName) > 255 {
		return errors.New("provider_name must be between 1 and 255 characters")
	}
	if request.AccountNumber == "" || utf8.RuneCountInString(request.AccountNumber) > 255 {
		return errors.New("account_number must be between 1 and 255 characters")
	}
	if request.AccountHolder == "" || utf8.RuneCountInString(request.AccountHolder) > 255 {
		return errors.New("account_holder must be between 1 and 255 characters")
	}
	return nil
}

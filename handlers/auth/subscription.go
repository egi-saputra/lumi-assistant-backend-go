package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

const (
	monthlyPremiumPrice = int64(23750)
	yearlyPremiumPrice  = int64(249799)
	freeMaxUsers        = 5
	premiumMaxUsers     = 10
	premiumMaxStores    = 3
)

type subscriptionStatus struct {
	Plan          string     `json:"plan"`
	Period        string     `json:"period,omitempty"`
	Active        bool       `json:"active"`
	Start         *time.Time `json:"start,omitempty"`
	End           *time.Time `json:"end,omitempty"`
	MaxUsers      int        `json:"max_users"`
	MaxStores     int        `json:"max_stores"`
	CurrentUsers  int        `json:"current_users"`
	CurrentStores int        `json:"current_stores"`
	Monthly       int64      `json:"monthly_price"`
	Yearly        int64      `json:"yearly_price"`
}

type snapRequest struct {
	TransactionDetails struct {
		OrderID     string `json:"order_id"`
		GrossAmount int64  `json:"gross_amount"`
	} `json:"transaction_details"`
	ItemDetails []struct {
		ID       string `json:"id"`
		Price    int64  `json:"price"`
		Quantity int    `json:"quantity"`
		Name     string `json:"name"`
	} `json:"item_details"`
	CustomerDetails struct {
		FirstName string `json:"first_name"`
		Email     string `json:"email"`
	} `json:"customer_details"`
}

type snapResponse struct {
	Token       string `json:"token"`
	RedirectURL string `json:"redirect_url"`
}

type midtransNotification struct {
	OrderID           string `json:"order_id"`
	StatusCode        string `json:"status_code"`
	GrossAmount       string `json:"gross_amount"`
	SignatureKey      string `json:"signature_key"`
	TransactionStatus string `json:"transaction_status"`
	FraudStatus       string `json:"fraud_status"`
	TransactionID     string `json:"transaction_id"`
	PaymentType       string `json:"payment_type"`
}

type midtransTransactionStatus struct {
	OrderID           string `json:"order_id"`
	StatusCode        string `json:"status_code"`
	GrossAmount       string `json:"gross_amount"`
	TransactionStatus string `json:"transaction_status"`
	FraudStatus       string `json:"fraud_status"`
	TransactionID     string `json:"transaction_id"`
	PaymentType       string `json:"payment_type"`
}

func GetSubscription(c *gin.Context) {
	accountID := c.GetString("user_id")
	if accountID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !ensureLegacyTenantOwners(accountID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare store ownership"})
		return
	}

	status, err := readSubscription(accountID, time.Now())
	if err != nil {
		log.Printf("load subscription for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load subscription"})
		return
	}
	if tenantID := c.GetString("tenant_id"); tenantID != "" {
		if err := db.Master.QueryRow(
			"SELECT COUNT(*) FROM users_index WHERE tenant_id = ?",
			tenantID,
		).Scan(&status.CurrentUsers); err != nil {
			log.Printf("count users for subscription account %s: %v", accountID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load subscription usage"})
			return
		}
	}
	status.CurrentStores, err = countOwnedStores(accountID)
	if err != nil {
		log.Printf("count stores for subscription account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load subscription usage"})
		return
	}
	c.JSON(http.StatusOK, status)
}

func GetSubscriptionPaymentHistory(c *gin.Context) {
	accountID := c.GetString("user_id")
	if accountID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	rows, err := db.Master.QueryContext(c.Request.Context(), `
		SELECT id, order_id, plan_period, amount, status,
			COALESCE(payment_type, ''), COALESCE(transaction_id, ''), paid_at, created_at
		FROM subscription_payments
		WHERE account_id = ?
		ORDER BY created_at DESC, id DESC
	`, accountID)
	if err != nil {
		log.Printf("load subscription payment history for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load subscription payment history"})
		return
	}
	defer rows.Close()

	type paymentHistoryItem struct {
		ID            string     `json:"id"`
		OrderID       string     `json:"order_id"`
		PlanPeriod    string     `json:"plan_period"`
		Amount        int64      `json:"amount"`
		Status        string     `json:"status"`
		PaymentType   string     `json:"payment_type"`
		TransactionID string     `json:"transaction_id"`
		PaidAt        *time.Time `json:"paid_at"`
		CreatedAt     time.Time  `json:"created_at"`
	}
	payments := make([]paymentHistoryItem, 0)
	for rows.Next() {
		var payment paymentHistoryItem
		var paidAt sql.NullTime
		if err := rows.Scan(
			&payment.ID,
			&payment.OrderID,
			&payment.PlanPeriod,
			&payment.Amount,
			&payment.Status,
			&payment.PaymentType,
			&payment.TransactionID,
			&paidAt,
			&payment.CreatedAt,
		); err != nil {
			log.Printf("scan subscription payment history for account %s: %v", accountID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read subscription payment history"})
			return
		}
		if paidAt.Valid {
			payment.PaidAt = &paidAt.Time
		}
		payments = append(payments, payment)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate subscription payment history for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read subscription payment history"})
		return
	}
	c.JSON(http.StatusOK, payments)
}

func CreateSubscriptionCheckout(c *gin.Context) {
	accountID := c.GetString("user_id")
	if accountID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !ensureLegacyTenantOwners(accountID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare store ownership"})
		return
	}

	var req struct {
		Period string `json:"period" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Period != "monthly" && req.Period != "yearly") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Period must be monthly or yearly"})
		return
	}

	serverKey := strings.TrimSpace(os.Getenv("MIDTRANS_SERVER_KEY"))
	if serverKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Midtrans Sandbox belum dikonfigurasi. Isi MIDTRANS_SERVER_KEY dengan Sandbox Server Key."})
		return
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MIDTRANS_IS_PRODUCTION")), "true") {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Endpoint produksi belum diaktifkan untuk alur Sandbox"})
		return
	}

	var account struct {
		Name  string
		Email string
	}
	if err := db.Master.QueryRow(
		"SELECT name, email FROM accounts WHERE id = ?",
		accountID,
	).Scan(&account.Name, &account.Email); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Account not found"})
			return
		}
		log.Printf("load account for subscription checkout %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		return
	}

	amount := subscriptionPrice(req.Period)
	orderID := newSubscriptionOrderID()
	paymentID := utils.NewULID()
	if _, err := db.Master.Exec(`
		INSERT INTO subscription_payments
			(id, order_id, account_id, plan_period, amount, status)
		VALUES (?, ?, ?, ?, ?, 'pending')
	`, paymentID, orderID, accountID, req.Period, amount); err != nil {
		log.Printf("create subscription payment for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create payment"})
		return
	}

	snap, err := requestSnapTransaction(c.Request.Context(), &http.Client{Timeout: 8 * time.Second}, serverKey, "https://app.sandbox.midtrans.com/snap/v1/transactions", orderID, amount, req.Period, account.Name, account.Email)
	if err != nil {
		_, updateErr := db.Master.Exec(
			"UPDATE subscription_payments SET status = 'failed' WHERE id = ? AND status = 'pending'",
			paymentID,
		)
		if updateErr != nil {
			log.Printf("mark failed subscription payment %s: %v", paymentID, updateErr)
		}
		log.Printf("create Midtrans Snap transaction %s: %v", orderID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Gagal membuat sesi pembayaran Midtrans Sandbox"})
		return
	}
	if snap.Token == "" || snap.RedirectURL == "" {
		if _, updateErr := db.Master.Exec(
			"UPDATE subscription_payments SET status = 'failed' WHERE id = ? AND status = 'pending'",
			paymentID,
		); updateErr != nil {
			log.Printf("mark incomplete Snap payment %s as failed: %v", paymentID, updateErr)
		}
		log.Printf("Midtrans returned an incomplete Snap response for order %s", orderID)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Midtrans mengembalikan sesi pembayaran yang tidak lengkap"})
		return
	}
	if _, err := db.Master.Exec(`
		UPDATE subscription_payments
		SET snap_token = ?, redirect_url = ?
		WHERE id = ? AND status = 'pending'
	`, snap.Token, snap.RedirectURL, paymentID); err != nil {
		log.Printf("save Midtrans Snap session for order %s: %v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save payment session"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"order_id":     orderID,
		"redirect_url": snap.RedirectURL,
	})
}

func MidtransNotification(c *gin.Context) {
	serverKey := strings.TrimSpace(os.Getenv("MIDTRANS_SERVER_KEY"))
	if serverKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Payment notification is not configured"})
		return
	}
	var notification midtransNotification
	if err := c.ShouldBindJSON(&notification); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid notification payload"})
		return
	}
	if notification.OrderID == "" || !validMidtransSignature(notification, serverKey) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid payment signature"})
		return
	}
	verified, err := fetchMidtransTransactionStatus(
		c.Request.Context(),
		&http.Client{Timeout: 8 * time.Second},
		serverKey,
		"https://api.sandbox.midtrans.com/v2/",
		notification.OrderID,
	)
	if err != nil {
		log.Printf("verify Midtrans transaction %s: %v", notification.OrderID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Payment status could not be verified"})
		return
	}
	if verified.OrderID != notification.OrderID || !sameGrossAmount(verified.GrossAmount, notification.GrossAmount) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Payment status order does not match notification"})
		return
	}

	var payload bytes.Buffer
	if err := json.NewEncoder(&payload).Encode(verified); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode notification"})
		return
	}
	verifiedNotification := midtransNotification{
		OrderID:           verified.OrderID,
		StatusCode:        verified.StatusCode,
		GrossAmount:       verified.GrossAmount,
		TransactionStatus: verified.TransactionStatus,
		FraudStatus:       verified.FraudStatus,
		TransactionID:     verified.TransactionID,
		PaymentType:       verified.PaymentType,
	}
	if err := processMidtransNotification(c.Request.Context(), db.Master, verifiedNotification, payload.Bytes()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Payment order not found"})
			return
		}
		log.Printf("process Midtrans notification for order %s: %v", notification.OrderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process payment notification"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Notification processed"})
}

func fetchMidtransTransactionStatus(ctx context.Context, client *http.Client, serverKey, endpoint, orderID string) (midtransTransactionStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+url.PathEscape(orderID)+"/status", nil)
	if err != nil {
		return midtransTransactionStatus{}, fmt.Errorf("build status request: %w", err)
	}
	request.SetBasicAuth(serverKey, "")
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return midtransTransactionStatus{}, fmt.Errorf("send status request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return midtransTransactionStatus{}, fmt.Errorf("status API returned HTTP %d", response.StatusCode)
	}
	var result midtransTransactionStatus
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result); err != nil {
		return midtransTransactionStatus{}, fmt.Errorf("decode status response: %w", err)
	}
	if result.OrderID == "" || result.GrossAmount == "" || result.TransactionStatus == "" {
		return midtransTransactionStatus{}, errors.New("status API response is missing transaction fields")
	}
	return result, nil
}

func requestSnapTransaction(ctx context.Context, client *http.Client, serverKey, endpoint, orderID string, amount int64, period, name, email string) (snapResponse, error) {
	label := "Starter Pack bulanan"
	if period == "yearly" {
		label = "Starter Pack tahunan"
	}
	var body snapRequest
	body.TransactionDetails.OrderID = orderID
	body.TransactionDetails.GrossAmount = amount
	body.ItemDetails = append(body.ItemDetails, struct {
		ID       string `json:"id"`
		Price    int64  `json:"price"`
		Quantity int    `json:"quantity"`
		Name     string `json:"name"`
	}{ID: "starter-pack-" + period, Price: amount, Quantity: 1, Name: label})
	body.CustomerDetails.FirstName = name
	body.CustomerDetails.Email = email

	encoded, err := json.Marshal(body)
	if err != nil {
		return snapResponse{}, fmt.Errorf("encode Snap request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return snapResponse{}, fmt.Errorf("build Snap request: %w", err)
	}
	request.SetBasicAuth(serverKey, "")
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return snapResponse{}, fmt.Errorf("send Snap request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return snapResponse{}, fmt.Errorf("Snap API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result snapResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result); err != nil {
		return snapResponse{}, fmt.Errorf("decode Snap response: %w", err)
	}
	return result, nil
}

func validMidtransSignature(notification midtransNotification, serverKey string) bool {
	expected := sha512.Sum512([]byte(notification.OrderID + notification.StatusCode + notification.GrossAmount + serverKey))
	expectedHex := hex.EncodeToString(expected[:])
	return hmac.Equal([]byte(strings.ToLower(notification.SignatureKey)), []byte(expectedHex))
}

func processMidtransNotification(ctx context.Context, database *sql.DB, notification midtransNotification, payload []byte) error {
	amountFloat, err := strconv.ParseFloat(notification.GrossAmount, 64)
	if err != nil || math.IsNaN(amountFloat) || math.IsInf(amountFloat, 0) {
		return fmt.Errorf("invalid gross amount %q", notification.GrossAmount)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payment notification transaction: %w", err)
	}
	defer tx.Rollback()

	var payment struct {
		AccountID string
		Period    string
		Amount    int64
		Status    string
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT account_id, plan_period, amount, status
		FROM subscription_payments
		WHERE order_id = ?
		FOR UPDATE
	`, notification.OrderID).Scan(&payment.AccountID, &payment.Period, &payment.Amount, &payment.Status); err != nil {
		return err
	}
	if amountFloat != float64(payment.Amount) || payment.Amount != subscriptionPrice(payment.Period) {
		return fmt.Errorf("gross amount does not match payment order %s", notification.OrderID)
	}
	if payment.Status == "paid" {
		return tx.Commit()
	}

	nextStatus := notification.TransactionStatus
	switch nextStatus {
	case "settlement":
		nextStatus = "paid"
	case "capture":
		if notification.FraudStatus == "accept" || notification.FraudStatus == "" {
			nextStatus = "paid"
		} else if notification.FraudStatus == "deny" {
			nextStatus = "deny"
		}
	case "pending", "deny", "cancel", "expire", "failure", "refund", "partial_refund", "chargeback", "partial_chargeback", "authorize":
	default:
		return fmt.Errorf("unsupported Midtrans transaction status %q", nextStatus)
	}

	if nextStatus == "paid" {
		now := time.Now()
		var currentPlan string
		var currentStart, currentEnd sql.NullTime
		if err := tx.QueryRowContext(ctx, `
			SELECT subscription_plan, subscription_start, subscription_end
			FROM accounts
			WHERE id = ?
			FOR UPDATE
		`, payment.AccountID).Scan(&currentPlan, &currentStart, &currentEnd); err != nil {
			return fmt.Errorf("lock subscription account: %w", err)
		}
		start := now
		if currentPlan == "premium" && currentEnd.Valid && currentEnd.Time.After(now) {
			start = currentEnd.Time
		}
		end := subscriptionPeriodEnd(start, payment.Period)
		if _, err := tx.ExecContext(ctx, `
			UPDATE accounts
			SET subscription_plan = 'premium',
				subscription_period = ?,
				subscription_start = IF(subscription_plan = 'premium' AND subscription_end > ?, subscription_start, ?),
				subscription_end = ?
			WHERE id = ?
		`, payment.Period, now, now, end, payment.AccountID); err != nil {
			return fmt.Errorf("activate account subscription: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tenants SET max_users = ?
			WHERE owner_account_id = ? AND status = 'active'
		`, premiumMaxUsers, payment.AccountID); err != nil {
			return fmt.Errorf("update owned store plan limits: %w", err)
		}
	}

	paidAt := any(nil)
	if nextStatus == "paid" {
		paidAt = time.Now()
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subscription_payments
		SET status = ?, transaction_id = NULLIF(?, ''),
			payment_type = NULLIF(?, ''), notification_payload = ?, paid_at = COALESCE(paid_at, ?)
		WHERE order_id = ?
	`, nextStatus, notification.TransactionID, notification.PaymentType, string(payload), paidAt, notification.OrderID); err != nil {
		return fmt.Errorf("save payment notification: %w", err)
	}
	return tx.Commit()
}

func readSubscription(accountID string, now time.Time) (subscriptionStatus, error) {
	var plan string
	var period sql.NullString
	var start, end sql.NullTime
	if err := db.Master.QueryRow(`
		SELECT subscription_plan, subscription_period, subscription_start, subscription_end
		FROM accounts WHERE id = ?
	`, accountID).Scan(&plan, &period, &start, &end); err != nil {
		return subscriptionStatus{}, err
	}

	active := plan == "premium" && end.Valid && end.Time.After(now)
	result := subscriptionStatus{
		Plan:      "free",
		Active:    false,
		MaxUsers:  freeMaxUsers,
		MaxStores: 1,
		Monthly:   monthlyPremiumPrice,
		Yearly:    yearlyPremiumPrice,
	}
	if active {
		result.Plan = "premium"
		result.Active = true
		result.MaxUsers = premiumMaxUsers
		result.MaxStores = premiumMaxStores
		if period.Valid {
			result.Period = period.String
		}
		if start.Valid {
			value := start.Time
			result.Start = &value
		}
		if end.Valid {
			value := end.Time
			result.End = &value
		}
	}
	return result, nil
}

func countOwnedStores(accountID string) (int, error) {
	var count int
	err := db.Master.QueryRow(`
		SELECT COUNT(*) FROM tenants
		WHERE owner_account_id = ? AND status IN ('active', 'provisioning')
	`, accountID).Scan(&count)
	return count, err
}

func effectiveStoreMaxUsers(tenantID string) (int, error) {
	var maxUsers int
	err := db.Master.QueryRow(`
		SELECT CASE
			WHEN a.subscription_plan = 'premium' AND a.subscription_end > CURRENT_TIMESTAMP THEN ?
			ELSE ?
		END
		FROM tenants t
		JOIN accounts a ON a.id = t.owner_account_id
		WHERE t.id = ? AND t.status = 'active'
	`, premiumMaxUsers, freeMaxUsers, tenantID).Scan(&maxUsers)
	return maxUsers, err
}

func subscriptionPrice(period string) int64 {
	if period == "yearly" {
		return yearlyPremiumPrice
	}
	return monthlyPremiumPrice
}

func newSubscriptionOrderID() string {
	return "LUMI-" + utils.NewULID()
}

func sameGrossAmount(left, right string) bool {
	leftAmount, leftErr := strconv.ParseFloat(left, 64)
	rightAmount, rightErr := strconv.ParseFloat(right, 64)
	return leftErr == nil && rightErr == nil &&
		!math.IsNaN(leftAmount) && !math.IsInf(leftAmount, 0) &&
		!math.IsNaN(rightAmount) && !math.IsInf(rightAmount, 0) &&
		leftAmount == rightAmount
}

func subscriptionPeriodEnd(start time.Time, period string) time.Time {
	year, month, day := start.Date()
	if period == "yearly" {
		year++
	} else {
		month++
	}
	firstOfTargetMonth := time.Date(year, month, 1, 0, 0, 0, 0, start.Location())
	lastDay := firstOfTargetMonth.AddDate(0, 1, -1).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), start.Location())
}

func ensureLegacyTenantOwners(accountID string) bool {
	rows, err := db.Master.Query(`
		SELECT id, db_name, plan, subscription_start, subscription_end
		FROM tenants
		WHERE owner_account_id IS NULL
			AND id IN (SELECT tenant_id FROM users_index WHERE user_id = ?)
	`, accountID)
	if err != nil {
		log.Printf("load legacy tenant owners for %s: %v", accountID, err)
		return false
	}
	type legacyTenant struct {
		id, dbName, plan string
		start, end       sql.NullTime
	}
	tenants := make([]legacyTenant, 0)
	for rows.Next() {
		var tenant legacyTenant
		if err := rows.Scan(&tenant.id, &tenant.dbName, &tenant.plan, &tenant.start, &tenant.end); err != nil {
			_ = rows.Close()
			log.Printf("read legacy tenant owner: %v", err)
			return false
		}
		tenants = append(tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		log.Printf("iterate legacy tenant owners: %v", err)
		return false
	}
	if err := rows.Close(); err != nil {
		log.Printf("close legacy tenant owners: %v", err)
		return false
	}

	for _, tenant := range tenants {
		_, err := resolveLegacyTenantOwner(tenant.id, tenant.dbName, tenant.plan, tenant.start, tenant.end)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			log.Printf("find owner for legacy tenant %s: %v", tenant.id, err)
			return false
		}
	}
	return true
}

func resolveLegacyTenantOwner(tenantID, dbName, plan string, start, end sql.NullTime) (string, error) {
	tenantDB, err := db.GetTenantDB(dbName)
	if err != nil {
		return "", fmt.Errorf("connect legacy tenant database %s: %w", tenantID, err)
	}
	var ownerID string
	if err := tenantDB.QueryRow(
		"SELECT id FROM users WHERE tenant_id = ? AND role = 'owner' LIMIT 1",
		tenantID,
	).Scan(&ownerID); err != nil {
		return "", err
	}
	if _, err := db.Master.Exec(
		"UPDATE tenants SET owner_account_id = ? WHERE id = ? AND owner_account_id IS NULL",
		ownerID, tenantID,
	); err != nil {
		return "", fmt.Errorf("save owner for legacy tenant %s: %w", tenantID, err)
	}
	if !end.Valid || !end.Time.After(time.Now()) ||
		(plan != "month" && plan != "quarter" && plan != "year" && plan != "premium") {
		return ownerID, nil
	}
	period := "monthly"
	if plan == "year" || (start.Valid && end.Time.Sub(start.Time) > 300*24*time.Hour) {
		period = "yearly"
	}
	if _, err := db.Master.Exec(`
		UPDATE accounts
		SET subscription_plan = 'premium', subscription_period = ?,
			subscription_start = ?, subscription_end = ?
		WHERE id = ? AND (subscription_plan = 'free' OR subscription_end < ? OR subscription_end IS NULL)
	`, period, start, end, ownerID, end.Time); err != nil {
		return "", fmt.Errorf("preserve legacy premium subscription for account %s: %w", ownerID, err)
	}
	if _, err := db.Master.Exec(
		"UPDATE tenants SET max_users = ? WHERE owner_account_id = ? AND status = 'active'",
		premiumMaxUsers,
		ownerID,
	); err != nil {
		return "", fmt.Errorf("preserve legacy store member limits for account %s: %w", ownerID, err)
	}
	return ownerID, nil
}

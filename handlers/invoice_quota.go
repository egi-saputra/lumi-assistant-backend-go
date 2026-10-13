package handlers

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

const (
	freeInvoiceBaseLimit = 3
	invoiceRewardUnits   = 3
	fallbackWait         = 60 * time.Second
	fallbackHeartbeatMax = 10 * time.Second
	quotaReservationTTL  = 10 * time.Minute
	rewardAttemptTTL     = 30 * time.Minute
	rewardItemName       = "invoice_credit"
)

type invoiceQuota struct {
	Plan         string `json:"plan"`
	Used         int    `json:"used"`
	BonusCredits int    `json:"bonus_credits"`
	Limit        int    `json:"limit"`
	Remaining    int    `json:"remaining"`
}

type rewardAttemptResponse struct {
	CustomData string `json:"custom_data"`
	UserID     string `json:"user_id"`
	AdUnitID   string `json:"ad_unit_id"`
}

type admobVerificationKey struct {
	KeyID  int64  `json:"keyId"`
	Base64 string `json:"base64"`
}

type admobVerificationKeys struct {
	Keys []admobVerificationKey `json:"keys"`
}

type cachedAdMobKeys struct {
	keys      map[int64]*ecdsa.PublicKey
	expiresAt time.Time
}

var adMobKeyCache struct {
	sync.Mutex
	value cachedAdMobKeys
}

func GetInvoiceQuota(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tenantDB := c.MustGet("tenant_db").(*sql.DB)
	accountID, err := ownerAccountForInvoiceQuota(tenantID, tenantDB)
	if err != nil {
		log.Printf("resolve invoice quota owner for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load invoice quota"})
		return
	}
	quota, err := readInvoiceQuota(accountID)
	if err != nil {
		log.Printf("read invoice quota for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load invoice quota"})
		return
	}
	c.JSON(http.StatusOK, quota)
}

func StartInvoiceAdReward(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tenantDB := c.MustGet("tenant_db").(*sql.DB)
	accountID, err := ownerAccountForInvoiceQuota(tenantID, tenantDB)
	if err != nil {
		log.Printf("resolve reward owner for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start reward"})
		return
	}
	var req struct {
		Platform string `json:"platform" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Platform != "android" && req.Platform != "ios") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Platform must be android or ios"})
		return
	}
	adUnitID := strings.TrimSpace(os.Getenv("ADMOB_REWARDED_AD_UNIT_ID_" + strings.ToUpper(req.Platform)))
	if adUnitID == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Rewarded ads are not configured"})
		return
	}
	attemptID, err := startRewardAttempt(c.Request.Context(), accountID, "ad")
	if errors.Is(err, errPremiumInvoicePlan) {
		c.JSON(http.StatusConflict, gin.H{"code": "PREMIUM_PLAN", "error": "Premium accounts do not need free invoice credits"})
		return
	}
	if errors.Is(err, errInvoiceQuotaAvailable) {
		c.JSON(http.StatusConflict, gin.H{"code": "INVOICE_QUOTA_AVAILABLE", "error": "Free invoice credits are still available"})
		return
	}
	if err != nil {
		log.Printf("start ad reward attempt for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start reward"})
		return
	}
	c.JSON(http.StatusCreated, rewardAttemptResponse{
		CustomData: attemptID,
		UserID:     accountID,
		AdUnitID:   adUnitID,
	})
}

func StartInvoiceFallback(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tenantDB := c.MustGet("tenant_db").(*sql.DB)
	accountID, err := ownerAccountForInvoiceQuota(tenantID, tenantDB)
	if err != nil {
		log.Printf("resolve fallback owner for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start fallback"})
		return
	}
	attemptID, err := startRewardAttempt(c.Request.Context(), accountID, "fallback")
	if errors.Is(err, errPremiumInvoicePlan) || errors.Is(err, errInvoiceQuotaAvailable) {
		c.JSON(http.StatusConflict, gin.H{"error": "An invoice reward is not currently needed"})
		return
	}
	if err != nil {
		log.Printf("start fallback attempt for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start fallback"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"attempt_id": attemptID, "duration_seconds": int(fallbackWait.Seconds())})
}

func HeartbeatInvoiceFallback(c *gin.Context) {
	accountID, _, err := currentInvoiceQuotaOwner(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify fallback"})
		return
	}
	var req struct {
		AttemptID string `json:"attempt_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid fallback attempt"})
		return
	}
	result, err := db.Master.ExecContext(c.Request.Context(), `
		UPDATE invoice_quota_reward_attempts
		SET last_heartbeat_at = CURRENT_TIMESTAMP
		WHERE id = ? AND account_id = ? AND reward_type = 'fallback'
			AND status = 'started'
			AND started_at > DATE_SUB(CURRENT_TIMESTAMP, INTERVAL 2 HOUR)
	`, req.AttemptID, accountID)
	if err != nil {
		log.Printf("heartbeat invoice fallback for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to continue fallback"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify fallback"})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Fallback timer expired or was cancelled"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fallback continues"})
}

func CompleteInvoiceFallback(c *gin.Context) {
	accountID, _, err := currentInvoiceQuotaOwner(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify fallback"})
		return
	}
	var req struct {
		AttemptID string `json:"attempt_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid fallback attempt"})
		return
	}
	if err := completeFallback(c.Request.Context(), accountID, req.AttemptID, time.Now()); err != nil {
		if errors.Is(err, errFallbackNotComplete) {
			c.JSON(http.StatusConflict, gin.H{"error": "Keep the app open until the timer finishes"})
			return
		}
		log.Printf("complete invoice fallback for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to complete fallback"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"credits_added": invoiceRewardUnits})
}

func CancelInvoiceFallback(c *gin.Context) {
	accountID, _, err := currentInvoiceQuotaOwner(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel fallback"})
		return
	}
	var req struct {
		AttemptID string `json:"attempt_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid fallback attempt"})
		return
	}
	if _, err := db.Master.ExecContext(c.Request.Context(), `
		UPDATE invoice_quota_reward_attempts SET status = 'cancelled'
		WHERE id = ? AND account_id = ? AND reward_type = 'fallback' AND status = 'started'
	`, req.AttemptID, accountID); err != nil {
		log.Printf("cancel invoice fallback for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel fallback"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fallback cancelled"})
}

func CancelInvoiceAdReward(c *gin.Context) {
	accountID, _, err := currentInvoiceQuotaOwner(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel rewarded ad"})
		return
	}
	var req struct {
		AttemptID string `json:"attempt_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid reward attempt"})
		return
	}
	if _, err := db.Master.ExecContext(c.Request.Context(), `
		UPDATE invoice_quota_reward_attempts SET status = 'cancelled'
		WHERE id = ? AND account_id = ? AND reward_type = 'ad' AND status = 'started'
	`, req.AttemptID, accountID); err != nil {
		log.Printf("cancel AdMob reward attempt for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel rewarded ad"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Reward attempt cancelled"})
}

func AdMobInvoiceRewardSSV(c *gin.Context) {
	rawQuery := c.Request.URL.RawQuery
	signedData, keyID, signature, err := parseAdMobSignedQuery(rawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid reward callback"})
		return
	}
	keys, err := getAdMobVerificationKeys(c.Request.Context(), &http.Client{Timeout: 8 * time.Second})
	if err != nil {
		log.Printf("load AdMob reward verification keys: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not verify reward callback"})
		return
	}
	publicKey := keys[keyID]
	if publicKey == nil {
		adMobKeyCache.Lock()
		adMobKeyCache.value.expiresAt = time.Time{}
		adMobKeyCache.Unlock()
		keys, err = getAdMobVerificationKeys(c.Request.Context(), &http.Client{Timeout: 8 * time.Second})
		if err != nil {
			log.Printf("refresh AdMob reward verification keys: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "Could not verify reward callback"})
			return
		}
		publicKey = keys[keyID]
	}
	hash := sha256.Sum256([]byte(signedData))
	if publicKey == nil || !ecdsa.VerifyASN1(publicKey, hash[:], signature) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid reward signature"})
		return
	}

	params, err := url.ParseQuery(signedData)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid signed reward data"})
		return
	}
	customData := params.Get("custom_data")
	accountID := params.Get("user_id")
	transactionID := params.Get("transaction_id")
	rewardAmount, amountErr := strconv.Atoi(params.Get("reward_amount"))
	rewardAdUnit := params.Get("ad_unit")
	if customData == "" || accountID == "" || transactionID == "" ||
		amountErr != nil || rewardAmount != 1 ||
		params.Get("reward_item") != rewardItemName ||
		!isConfiguredAdUnit(rewardAdUnit) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Reward callback values are invalid"})
		return
	}
	if err := grantAdMobInvoiceReward(c.Request.Context(), accountID, customData, transactionID, time.Now()); err != nil {
		if errors.Is(err, errRewardAlreadyProcessed) {
			c.JSON(http.StatusOK, gin.H{"message": "Reward already processed"})
			return
		}
		if errors.Is(err, errRewardAttemptInvalid) {
			c.JSON(http.StatusConflict, gin.H{"error": "Reward attempt is expired or invalid"})
			return
		}
		log.Printf("grant AdMob invoice reward for account %s: %v", accountID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not grant reward"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Reward granted"})
}

var (
	errPremiumInvoicePlan     = errors.New("premium plan does not use free invoice quota")
	errInvoiceQuotaAvailable  = errors.New("free invoice quota is still available")
	errFallbackNotComplete    = errors.New("fallback timer is not complete")
	errRewardAlreadyProcessed = errors.New("reward transaction already processed")
	errRewardAttemptInvalid   = errors.New("reward attempt is invalid")
)

func currentInvoiceQuotaOwner(c *gin.Context) (string, string, error) {
	tenantID := c.GetString("tenant_id")
	tenantDB, ok := c.Get("tenant_db")
	if tenantID == "" || !ok {
		return "", "", errors.New("tenant context is missing")
	}
	database, ok := tenantDB.(*sql.DB)
	if !ok {
		return "", "", errors.New("tenant database context is invalid")
	}
	accountID, err := ownerAccountForInvoiceQuota(tenantID, database)
	return accountID, tenantID, err
}

func ownerAccountForInvoiceQuota(tenantID string, tenantDB *sql.DB) (string, error) {
	var ownerID sql.NullString
	if err := db.Master.QueryRow(
		"SELECT owner_account_id FROM tenants WHERE id = ?",
		tenantID,
	).Scan(&ownerID); err != nil {
		return "", err
	}
	if ownerID.Valid && ownerID.String != "" {
		return ownerID.String, nil
	}
	if err := tenantDB.QueryRow(
		"SELECT id FROM users WHERE tenant_id = ? AND role = 'owner' LIMIT 1",
		tenantID,
	).Scan(&ownerID); err != nil {
		return "", fmt.Errorf("find legacy store owner: %w", err)
	}
	if _, err := db.Master.Exec(
		"UPDATE tenants SET owner_account_id = ? WHERE id = ? AND owner_account_id IS NULL",
		ownerID.String,
		tenantID,
	); err != nil {
		return "", fmt.Errorf("save legacy store owner: %w", err)
	}
	return ownerID.String, nil
}

func readInvoiceQuota(accountID string) (invoiceQuota, error) {
	var plan string
	var end sql.NullTime
	var bonus int
	if err := db.Master.QueryRow(`
		SELECT subscription_plan, subscription_end, free_invoice_bonus_credits
		FROM accounts WHERE id = ?
	`, accountID).Scan(&plan, &end, &bonus); err != nil {
		return invoiceQuota{}, err
	}
	if plan == "premium" && end.Valid && end.Time.After(time.Now()) {
		return invoiceQuota{Plan: "premium", Limit: -1, Remaining: -1, BonusCredits: bonus}, nil
	}
	used, err := countInvoiceQuotaUse(db.Master, accountID, time.Now())
	if err != nil {
		return invoiceQuota{}, err
	}
	return freeInvoiceQuota(used, bonus), nil
}

func freeInvoiceQuota(used, bonus int) invoiceQuota {
	limit := freeInvoiceBaseLimit + bonus
	return invoiceQuota{
		Plan:         "free",
		Used:         used,
		BonusCredits: bonus,
		Limit:        limit,
		Remaining:    max(0, limit-used),
	}
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func countInvoiceQuotaUse(database queryRower, accountID string, now time.Time) (int, error) {
	var used int
	err := database.QueryRow(`
		SELECT COUNT(*) FROM invoice_quota_uses
		WHERE account_id = ?
			AND (status = 'consumed' OR (status = 'reserved' AND reserved_until > ?))
	`, accountID, now).Scan(&used)
	return used, err
}

func reserveInvoiceQuota(ctx context.Context, tenantID string) (string, string, bool, invoiceQuota, error) {
	var ownerID string
	if err := db.Master.QueryRow(
		"SELECT owner_account_id FROM tenants WHERE id = ? AND status = 'active'",
		tenantID,
	).Scan(&ownerID); err != nil {
		return "", "", false, invoiceQuota{}, err
	}
	tx, err := db.Master.BeginTx(ctx, nil)
	if err != nil {
		return "", "", false, invoiceQuota{}, err
	}
	defer tx.Rollback()

	var plan string
	var end sql.NullTime
	var bonus int
	if err := tx.QueryRowContext(ctx, `
		SELECT subscription_plan, subscription_end, free_invoice_bonus_credits
		FROM accounts WHERE id = ? FOR UPDATE
	`, ownerID).Scan(&plan, &end, &bonus); err != nil {
		return "", "", false, invoiceQuota{}, err
	}
	if plan == "premium" && end.Valid && end.Time.After(time.Now()) {
		if err := tx.Commit(); err != nil {
			return "", "", false, invoiceQuota{}, err
		}
		return ownerID, "", true, invoiceQuota{Plan: "premium", Limit: -1, Remaining: -1, BonusCredits: bonus}, nil
	}
	used, err := countInvoiceQuotaUse(tx, ownerID, time.Now())
	if err != nil {
		return "", "", false, invoiceQuota{}, err
	}
	limit := freeInvoiceBaseLimit + bonus
	quota := freeInvoiceQuota(used, bonus)
	if used >= limit {
		if err := tx.Commit(); err != nil {
			return "", "", false, invoiceQuota{}, err
		}
		return ownerID, "", false, quota, nil
	}
	reservationID := utils.NewULID()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoice_quota_uses (id, account_id, tenant_id, status, reserved_until)
		VALUES (?, ?, ?, 'reserved', ?)
	`, reservationID, ownerID, tenantID, time.Now().Add(quotaReservationTTL)); err != nil {
		return "", "", false, invoiceQuota{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", "", false, invoiceQuota{}, err
	}
	quota.Used++
	quota.Remaining--
	return ownerID, reservationID, true, quota, nil
}

func settleInvoiceQuota(ctx context.Context, reservationID, invoiceID string) error {
	if reservationID == "" {
		return nil
	}
	result, err := db.Master.ExecContext(ctx, `
		UPDATE invoice_quota_uses SET status = 'consumed', invoice_id = ?
		WHERE id = ? AND status = 'reserved' AND reserved_until > CURRENT_TIMESTAMP
	`, invoiceID, reservationID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("invoice quota reservation expired before settlement")
	}
	return nil
}

func releaseInvoiceQuota(ctx context.Context, reservationID string) error {
	if reservationID == "" {
		return nil
	}
	_, err := db.Master.ExecContext(ctx, `
		UPDATE invoice_quota_uses SET status = 'released'
		WHERE id = ? AND status = 'reserved'
	`, reservationID)
	return err
}

func startRewardAttempt(ctx context.Context, accountID, rewardType string) (string, error) {
	tx, err := db.Master.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var plan string
	var end sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT subscription_plan, subscription_end
		FROM accounts WHERE id = ? FOR UPDATE
	`, accountID).Scan(&plan, &end); err != nil {
		return "", err
	}
	if plan == "premium" && end.Valid && end.Time.After(time.Now()) {
		return "", errPremiumInvoicePlan
	}
	used, err := countInvoiceQuotaUse(tx, accountID, time.Now())
	if err != nil {
		return "", err
	}
	var bonus int
	if err := tx.QueryRowContext(ctx, "SELECT free_invoice_bonus_credits FROM accounts WHERE id = ?", accountID).Scan(&bonus); err != nil {
		return "", err
	}
	if used < freeInvoiceBaseLimit+bonus {
		return "", errInvoiceQuotaAvailable
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE invoice_quota_reward_attempts SET status = 'cancelled'
		WHERE account_id = ? AND status = 'started'
	`, accountID); err != nil {
		return "", err
	}
	attemptID := utils.NewULID()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoice_quota_reward_attempts
			(id, account_id, reward_type, status, started_at)
		VALUES (?, ?, ?, 'started', CURRENT_TIMESTAMP)
	`, attemptID, accountID, rewardType); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return attemptID, nil
}

func completeFallback(ctx context.Context, accountID, attemptID string, now time.Time) error {
	tx, err := db.Master.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var startedAt, heartbeatAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT started_at, last_heartbeat_at
		FROM invoice_quota_reward_attempts
		WHERE id = ? AND account_id = ? AND reward_type = 'fallback' AND status = 'started'
		FOR UPDATE
	`, attemptID, accountID).Scan(&startedAt, &heartbeatAt); err != nil {
		return err
	}
	if !startedAt.Valid || !heartbeatAt.Valid ||
		now.Sub(startedAt.Time) < fallbackWait ||
		now.Sub(heartbeatAt.Time) > fallbackHeartbeatMax ||
		now.Sub(startedAt.Time) > rewardAttemptTTL {
		return errFallbackNotComplete
	}
	var plan string
	var end sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT subscription_plan, subscription_end
		FROM accounts WHERE id = ? FOR UPDATE
	`, accountID).Scan(&plan, &end); err != nil {
		return err
	}
	if plan == "premium" && end.Valid && end.Time.After(now) {
		return errRewardAttemptInvalid
	}
	used, err := countInvoiceQuotaUse(tx, accountID, now)
	if err != nil {
		return err
	}
	var bonus int
	if err := tx.QueryRowContext(ctx, `
		SELECT free_invoice_bonus_credits FROM accounts WHERE id = ?
	`, accountID).Scan(&bonus); err != nil {
		return err
	}
	if used < freeInvoiceBaseLimit+bonus {
		return errInvoiceQuotaAvailable
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE accounts SET free_invoice_bonus_credits = free_invoice_bonus_credits + ?
		WHERE id = ?
	`, invoiceRewardUnits, accountID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE invoice_quota_reward_attempts SET status = 'completed'
		WHERE id = ? AND account_id = ? AND status = 'started'
	`, attemptID, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func grantAdMobInvoiceReward(ctx context.Context, accountID, attemptID, transactionID string, now time.Time) error {
	tx, err := db.Master.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingAttemptID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM invoice_quota_reward_attempts WHERE transaction_id = ?
	`, transactionID).Scan(&existingAttemptID)
	if err == nil {
		return errRewardAlreadyProcessed
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var startedAt time.Time
	var rewardType string
	var tenantID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT started_at, reward_type, tenant_id FROM invoice_quota_reward_attempts
		WHERE id = ? AND account_id = ? AND reward_type = 'ad' AND status = 'started'
		FOR UPDATE
	`, attemptID, accountID).Scan(&startedAt, &rewardType, &tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.QueryRowContext(ctx, `
				SELECT started_at, reward_type, tenant_id FROM invoice_quota_reward_attempts
				WHERE id = ? AND account_id = ?
					AND reward_type IN ('product_slots', 'smart_engine') AND status = 'started'
				FOR UPDATE
			`, attemptID, accountID).Scan(&startedAt, &rewardType, &tenantID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return errRewardAttemptInvalid
				}
				return err
			}
		} else {
			return err
		}
	}
	if now.Sub(startedAt) < 0 || now.Sub(startedAt) > rewardAttemptTTL {
		return errRewardAttemptInvalid
	}
	var plan string
	var end sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT subscription_plan, subscription_end FROM accounts WHERE id = ? FOR UPDATE
	`, accountID).Scan(&plan, &end); err != nil {
		return err
	}
	if plan == "premium" && end.Valid && end.Time.After(now) {
		return errRewardAttemptInvalid
	}
	switch rewardType {
	case "ad":
		if _, err := tx.ExecContext(ctx, `
			UPDATE accounts SET free_invoice_bonus_credits = free_invoice_bonus_credits + ?
			WHERE id = ?
		`, invoiceRewardUnits, accountID); err != nil {
			return err
		}
	case "product_slots":
		if !tenantID.Valid {
			return errRewardAttemptInvalid
		}
		var bonusSlots int
		if err := tx.QueryRowContext(ctx, `
			SELECT free_product_bonus_slots FROM tenants
			WHERE id = ? AND owner_account_id = ? FOR UPDATE
		`, tenantID.String, accountID).Scan(&bonusSlots); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errRewardAttemptInvalid
			}
			return err
		}
		if bonusSlots >= productRewardMaxSlots {
			return errRewardAttemptInvalid
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tenants
			SET free_product_bonus_slots = LEAST(?, free_product_bonus_slots + ?)
			WHERE id = ? AND owner_account_id = ?
		`, productRewardMaxSlots, productRewardSlots, tenantID.String, accountID); err != nil {
			return err
		}
	case "smart_engine":
		if !tenantID.Valid {
			return errRewardAttemptInvalid
		}
		var credits int
		if err := tx.QueryRowContext(ctx, `
			SELECT free_smart_engine_credits FROM tenants
			WHERE id = ? AND owner_account_id = ? FOR UPDATE
		`, tenantID.String, accountID).Scan(&credits); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errRewardAttemptInvalid
			}
			return err
		}
		if credits > 0 {
			return errRewardAttemptInvalid
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tenants SET free_smart_engine_credits = free_smart_engine_credits + 1
			WHERE id = ? AND owner_account_id = ?
		`, tenantID.String, accountID); err != nil {
			return err
		}
	default:
		return errRewardAttemptInvalid
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE invoice_quota_reward_attempts
		SET status = 'completed', transaction_id = ?
		WHERE id = ? AND status = 'started'
	`, transactionID, attemptID); err != nil {
		return err
	}
	return tx.Commit()
}

func parseAdMobSignedQuery(rawQuery string) (string, int64, []byte, error) {
	signatureMarker := "&signature="
	signatureIndex := strings.LastIndex(rawQuery, signatureMarker)
	if signatureIndex <= 0 {
		return "", 0, nil, errors.New("missing AdMob signature")
	}
	signedData := rawQuery[:signatureIndex]
	tail := rawQuery[signatureIndex+len(signatureMarker):]
	tailParts := strings.Split(tail, "&key_id=")
	if len(tailParts) != 2 || strings.Contains(tailParts[1], "&") {
		return "", 0, nil, errors.New("invalid AdMob signature fields")
	}
	signature, err := base64.RawURLEncoding.DecodeString(tailParts[0])
	if err != nil {
		signature, err = base64.URLEncoding.DecodeString(tailParts[0])
		if err != nil {
			return "", 0, nil, errors.New("invalid AdMob signature encoding")
		}
	}
	keyID, err := strconv.ParseInt(tailParts[1], 10, 64)
	if err != nil || keyID <= 0 {
		return "", 0, nil, errors.New("invalid AdMob key ID")
	}
	return signedData, keyID, signature, nil
}

func getAdMobVerificationKeys(ctx context.Context, client *http.Client) (map[int64]*ecdsa.PublicKey, error) {
	adMobKeyCache.Lock()
	defer adMobKeyCache.Unlock()
	if time.Now().Before(adMobKeyCache.value.expiresAt) && len(adMobKeyCache.value.keys) > 0 {
		return adMobKeyCache.value.keys, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.gstatic.com/admob/reward/verifier-keys.json", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AdMob key server returned HTTP %d", response.StatusCode)
	}
	var document admobVerificationKeys
	if err := json.NewDecoder(io.LimitReader(response.Body, 256*1024)).Decode(&document); err != nil {
		return nil, err
	}
	keys := make(map[int64]*ecdsa.PublicKey, len(document.Keys))
	for _, item := range document.Keys {
		encoded, err := base64.StdEncoding.DecodeString(item.Base64)
		if err != nil {
			continue
		}
		parsed, err := x509.ParsePKIXPublicKey(encoded)
		if err != nil {
			block, _ := pem.Decode(encoded)
			if block == nil {
				continue
			}
			parsed, err = x509.ParsePKIXPublicKey(block.Bytes)
		}
		key, ok := parsed.(*ecdsa.PublicKey)
		if err == nil && ok && item.KeyID > 0 {
			keys[item.KeyID] = key
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("AdMob key server returned no usable keys")
	}
	adMobKeyCache.value = cachedAdMobKeys{keys: keys, expiresAt: time.Now().Add(12 * time.Hour)}
	return keys, nil
}

func isConfiguredAdUnit(adUnitID string) bool {
	return adUnitID != "" &&
		(adUnitID == strings.TrimSpace(os.Getenv("ADMOB_REWARDED_AD_UNIT_ID_ANDROID")) ||
			adUnitID == strings.TrimSpace(os.Getenv("ADMOB_REWARDED_AD_UNIT_ID_IOS")))
}

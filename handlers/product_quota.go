package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

const (
	freeProductBaseLimit  = 10
	freeProductMaxLimit   = 20
	premiumProductLimit   = 50
	productRewardSlots    = 2
	productRewardMaxSlots = freeProductMaxLimit - freeProductBaseLimit
)

type productQuota struct {
	Plan               string `json:"plan"`
	Count              int    `json:"count"`
	Limit              int    `json:"limit"`
	Remaining          int    `json:"remaining"`
	BonusSlots         int    `json:"bonus_slots"`
	SmartEngineCredits int    `json:"smart_engine_credits"`
}

type lockedProductQuota struct {
	quota     productQuota
	accountID string
	tx        *sql.Tx
}

var (
	errFreeProductLimit        = errors.New("free product limit reached")
	errSmartEngineRewardNeeded = errors.New("smart engine reward is required")
)

func GetProductQuota(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tenantDB := c.MustGet("tenant_db").(*sql.DB)
	_, err := ownerAccountForInvoiceQuota(tenantID, tenantDB)
	if err != nil {
		log.Printf("load product quota for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product quota"})
		return
	}
	var bonusSlots, smartEngineCredits int
	var plan string
	var subscriptionEnd sql.NullTime
	if err := db.Master.QueryRowContext(c.Request.Context(), `
		SELECT t.free_product_bonus_slots, t.free_smart_engine_credits,
			a.subscription_plan, a.subscription_end
		FROM tenants AS t
		JOIN accounts AS a ON a.id = t.owner_account_id
		WHERE t.id = ?
	`, tenantID).Scan(&bonusSlots, &smartEngineCredits, &plan, &subscriptionEnd); err != nil {
		log.Printf("read product plan for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product quota"})
		return
	}
	var count int
	if err := tenantDB.QueryRowContext(c.Request.Context(), `
		SELECT COUNT(*) FROM products WHERE is_deleted = 0
	`).Scan(&count); err != nil {
		log.Printf("count products for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product quota"})
		return
	}
	quotaPlan, limit := productLimit(plan, subscriptionEnd, bonusSlots, time.Now())
	c.JSON(http.StatusOK, productQuota{
		Plan: quotaPlan, Count: count, Limit: limit, Remaining: max(0, limit-count),
		BonusSlots: bonusSlots, SmartEngineCredits: smartEngineCredits,
	})
}

func StartProductReward(c *gin.Context) {
	var request struct {
		Kind     string `json:"kind" binding:"required"`
		Platform string `json:"platform" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil ||
		(request.Kind != "product_slots" && request.Kind != "smart_engine") ||
		(request.Platform != "android" && request.Platform != "ios") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product reward request"})
		return
	}
	adUnitID := strings.TrimSpace(getRewardedAdUnit(request.Platform))
	if adUnitID == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Rewarded ads are not configured"})
		return
	}

	tenantID := c.GetString("tenant_id")
	tenantDB := c.MustGet("tenant_db").(*sql.DB)
	locked, err := lockProductQuota(c.Request.Context(), tenantID, tenantDB)
	if err != nil {
		log.Printf("lock product quota for reward in tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start product reward"})
		return
	}
	defer locked.tx.Rollback()
	if locked.quota.Plan == "premium" {
		c.JSON(http.StatusConflict, gin.H{"code": "PREMIUM_PLAN", "error": "Premium accounts do not need product rewards"})
		return
	}
	switch request.Kind {
	case "product_slots":
		if locked.quota.Count < locked.quota.Limit || locked.quota.BonusSlots >= productRewardMaxSlots {
			c.JSON(http.StatusConflict, gin.H{"code": "PRODUCT_REWARD_NOT_READY", "error": "A product reward is not currently available"})
			return
		}
	case "smart_engine":
		if locked.quota.SmartEngineCredits > 0 {
			c.JSON(http.StatusConflict, gin.H{"code": "SMART_ENGINE_CREDIT_AVAILABLE", "error": "A Smart Engine activation is already available"})
			return
		}
	}

	attemptID := utils.NewULID()
	if _, err := locked.tx.ExecContext(c.Request.Context(), `
		INSERT INTO invoice_quota_reward_attempts
			(id, account_id, tenant_id, reward_type, status, started_at)
		VALUES (?, ?, ?, ?, 'started', CURRENT_TIMESTAMP)
	`, attemptID, locked.accountID, tenantID, request.Kind); err != nil {
		log.Printf("create product reward attempt for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start product reward"})
		return
	}
	if err := locked.tx.Commit(); err != nil {
		log.Printf("commit product reward attempt for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start product reward"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"custom_data": attemptID,
		"user_id":     locked.accountID,
		"ad_unit_id":  adUnitID,
	})
}

func CancelProductReward(c *gin.Context) {
	var request struct {
		AttemptID string `json:"attempt_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product reward attempt"})
		return
	}
	accountID, _, err := currentInvoiceQuotaOwner(c)
	if err != nil {
		log.Printf("resolve product reward owner: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel product reward"})
		return
	}
	if _, err := db.Master.ExecContext(c.Request.Context(), `
		UPDATE invoice_quota_reward_attempts SET status = 'cancelled'
		WHERE id = ? AND account_id = ? AND tenant_id = ?
			AND reward_type IN ('product_slots', 'smart_engine') AND status = 'started'
	`, request.AttemptID, accountID, c.GetString("tenant_id")); err != nil {
		log.Printf("cancel product reward attempt for tenant %s: %v", c.GetString("tenant_id"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel product reward"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Product reward cancelled"})
}

func lockProductQuota(ctx context.Context, tenantID string, tenantDB *sql.DB) (*lockedProductQuota, error) {
	accountID, err := ownerAccountForInvoiceQuota(tenantID, tenantDB)
	if err != nil {
		return nil, err
	}
	tx, err := db.Master.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	locked := &lockedProductQuota{tx: tx, accountID: accountID}
	var bonusSlots, smartEngineCredits int
	if err := tx.QueryRowContext(ctx, `
		SELECT free_product_bonus_slots, free_smart_engine_credits
		FROM tenants
		WHERE id = ? AND owner_account_id = ?
		FOR UPDATE
	`, tenantID, accountID).Scan(&bonusSlots, &smartEngineCredits); err != nil {
		tx.Rollback()
		return nil, err
	}
	var plan string
	var subscriptionEnd sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT subscription_plan, subscription_end
		FROM accounts
		WHERE id = ?
	`, accountID).Scan(&plan, &subscriptionEnd); err != nil {
		tx.Rollback()
		return nil, err
	}
	quotaPlan, limit := productLimit(plan, subscriptionEnd, bonusSlots, time.Now())
	if err := tenantDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM products WHERE is_deleted = 0
	`).Scan(&locked.quota.Count); err != nil {
		tx.Rollback()
		return nil, err
	}
	locked.quota.Plan = quotaPlan
	locked.quota.Limit = limit
	locked.quota.Remaining = max(0, limit-locked.quota.Count)
	locked.quota.BonusSlots = bonusSlots
	locked.quota.SmartEngineCredits = smartEngineCredits
	return locked, nil
}

func getRewardedAdUnit(platform string) string {
	platform = strings.ToUpper(platform)
	return strings.TrimSpace(os.Getenv("ADMOB_REWARDED_AD_UNIT_ID_" + platform))
}

func productLimit(plan string, subscriptionEnd sql.NullTime, bonusSlots int, now time.Time) (string, int) {
	if plan == "premium" && subscriptionEnd.Valid && subscriptionEnd.Time.After(now) {
		return "premium", premiumProductLimit
	}
	return "free", freeProductBaseLimit + min(max(bonusSlots, 0), productRewardMaxSlots)
}

func isPremiumTenant(ctx context.Context, tenantID string, tenantDB *sql.DB) (bool, error) {
	accountID, err := ownerAccountForInvoiceQuota(tenantID, tenantDB)
	if err != nil {
		return false, err
	}
	var plan string
	var end sql.NullTime
	if err := db.Master.QueryRowContext(ctx, `
		SELECT subscription_plan, subscription_end FROM accounts WHERE id = ?
	`, accountID).Scan(&plan, &end); err != nil {
		return false, err
	}
	return plan == "premium" && end.Valid && end.Time.After(time.Now()), nil
}

func writeProductQuotaError(c *gin.Context, err error, quota productQuota) bool {
	switch {
	case errors.Is(err, errFreeProductLimit):
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code":  "FREE_PRODUCT_QUOTA",
			"error": "Batas produk paket Free sudah tercapai. Tonton iklan untuk membuka 2 slot produk berikutnya atau upgrade ke Starter Pack.",
			"quota": quota,
		})
	case errors.Is(err, errSmartEngineRewardNeeded):
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code":  "SMART_ENGINE_REWARD_REQUIRED",
			"error": "Aktivasi Smart Engine memerlukan rewarded ad atau paket Starter Pack.",
			"quota": quota,
		})
	default:
		return false
	}
	return true
}

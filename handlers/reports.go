package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type reportSummaryResponse struct {
	TotalSales     int64 `json:"total_sales"`
	TotalIncome    int64 `json:"total_income"`
	CashIncome     int64 `json:"cash_income"`
	TransferIncome int64 `json:"transfer_income"`
	TotalExpenses  int64 `json:"total_expenses"`
	InventoryValue int64 `json:"inventory_value"`
	InvoiceCount   int64 `json:"invoice_count"`
}

type paymentHistoryResponse struct {
	ID        string    `json:"id"`
	InvoiceID string    `json:"invoice_id"`
	Code      *string   `json:"code"`
	Customer  *string   `json:"customer"`
	Amount    int64     `json:"amount"`
	Method    *string   `json:"method"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	Type      string    `json:"type"`
}

func GetReportSummary(c *gin.Context) {
	start, end, err := parseReportDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dateClause, dateArgs := reportDateClause("created_at", start, end)
	conn := c.MustGet("tenant_db").(*sql.DB)
	var summary reportSummaryResponse
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT COALESCE(SUM(total_amount), 0), COUNT(*)
		FROM invoices WHERE `+dateClause, dateArgs...).Scan(
		&summary.TotalSales, &summary.InvoiceCount,
	); err != nil {
		log.Printf("load report invoice summary: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load report summary"})
		return
	}
	paymentClause, paymentArgs := reportDateClause("created_at", start, end)
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'IN' THEN amount ELSE -amount END), 0),
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(method, '')) = 'cash'
				THEN CASE WHEN type = 'IN' THEN amount ELSE -amount END ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(method, '')) = 'transfer'
				THEN CASE WHEN type = 'IN' THEN amount ELSE -amount END ELSE 0 END), 0)
		FROM invoice_payments WHERE `+paymentClause, paymentArgs...).Scan(
		&summary.TotalIncome, &summary.CashIncome, &summary.TransferIncome,
	); err != nil {
		log.Printf("load report payment summary: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load report summary"})
		return
	}
	expenseClause, expenseArgs := reportDateClause("created_at", start, end)
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT COALESCE(SUM(amount), 0) FROM expenses WHERE `+expenseClause,
		expenseArgs...,
	).Scan(&summary.TotalExpenses); err != nil {
		log.Printf("load report expense summary: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load report summary"})
		return
	}
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT COALESCE(SUM(stock * cost_price), 0)
		FROM products WHERE is_deleted = 0
	`).Scan(&summary.InventoryValue); err != nil {
		log.Printf("load report inventory valuation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load inventory valuation"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func ListPaymentHistory(c *gin.Context) {
	start, end, err := parseReportDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dateClause, args := reportDateClause("payment.created_at", start, end)
	conn := c.MustGet("tenant_db").(*sql.DB)
	rows, err := conn.QueryContext(c.Request.Context(), `
		SELECT payment.id, payment.invoice_id, invoice.code, invoice.customer,
		       payment.amount, payment.method, payment.note, payment.created_at, payment.type
		FROM invoice_payments AS payment
		LEFT JOIN invoices AS invoice ON invoice.id = payment.invoice_id
		WHERE `+dateClause+`
		ORDER BY payment.created_at DESC, payment.id DESC
	`, args...)
	if err != nil {
		log.Printf("list tenant payment history: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load payment history"})
		return
	}
	defer rows.Close()
	payments := make([]paymentHistoryResponse, 0)
	for rows.Next() {
		var payment paymentHistoryResponse
		var code, customer, method, note sql.NullString
		if err := rows.Scan(
			&payment.ID, &payment.InvoiceID, &code, &customer,
			&payment.Amount, &method, &note, &payment.CreatedAt, &payment.Type,
		); err != nil {
			log.Printf("scan tenant payment history: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load payment history"})
			return
		}
		payment.Code = nullableString(code)
		payment.Customer = nullableString(customer)
		payment.Method = nullableString(method)
		payment.Note = nullableString(note)
		payments = append(payments, payment)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate tenant payment history: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load payment history"})
		return
	}
	c.JSON(http.StatusOK, payments)
}

func parseReportDateRange(c *gin.Context) (*time.Time, *time.Time, error) {
	startValue := c.Query("start_at")
	endValue := c.Query("end_at")
	if startValue == "" && endValue == "" {
		return nil, nil, nil
	}
	if startValue == "" || endValue == "" {
		return nil, nil, errors.New("both start_at and end_at are required")
	}
	start, err := time.Parse(time.RFC3339Nano, startValue)
	if err != nil {
		return nil, nil, errors.New("start_at and end_at must use RFC3339 format")
	}
	end, err := time.Parse(time.RFC3339Nano, endValue)
	if err != nil {
		return nil, nil, errors.New("start_at and end_at must use RFC3339 format")
	}
	if !end.After(start) {
		return nil, nil, errors.New("end_at must be after start_at")
	}
	return &start, &end, nil
}

func reportDateClause(column string, start, end *time.Time) (string, []interface{}) {
	if start == nil || end == nil {
		return "1 = 1", nil
	}
	return fmt.Sprintf("%s >= ? AND %s < ?", column, column),
		[]interface{}{start.UTC(), end.UTC()}
}

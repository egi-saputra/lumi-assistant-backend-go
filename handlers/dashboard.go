package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type dashboardTransaction struct {
	ID          string    `json:"id"`
	ProductName string    `json:"product_name"`
	Quantity    int64     `json:"qty"`
	Price       int64     `json:"price"`
	Total       int64     `json:"total"`
	CreatedAt   time.Time `json:"created_at"`
}

type dashboardSummaryResponse struct {
	TotalSales   int64                  `json:"total_sales"`
	TotalIncome  int64                  `json:"total_income"`
	InvoiceCount int64                  `json:"invoice_count"`
	Transactions []dashboardTransaction `json:"transactions"`
}

func GetDashboardSummary(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	var summary dashboardSummaryResponse
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT COALESCE(SUM(total_amount), 0), COUNT(*)
		FROM invoices
	`).Scan(&summary.TotalSales, &summary.InvoiceCount); err != nil {
		log.Printf("load dashboard sales summary: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load dashboard summary"})
		return
	}
	if err := conn.QueryRowContext(c.Request.Context(), `
		SELECT COALESCE(SUM(CASE WHEN type = 'IN' THEN amount ELSE -amount END), 0)
		FROM invoice_payments
	`).Scan(&summary.TotalIncome); err != nil {
		log.Printf("load dashboard payment summary: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load dashboard summary"})
		return
	}

	rows, err := conn.QueryContext(c.Request.Context(), `
		SELECT item.id, item.product_name, item.qty, item.price,
		       item.qty * item.price, invoice.created_at
		FROM invoice_items AS item
		JOIN invoices AS invoice ON invoice.id = item.invoice_id
		ORDER BY invoice.created_at DESC, item.id DESC
		LIMIT 5
	`)
	if err != nil {
		log.Printf("load recent dashboard transactions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load dashboard transactions"})
		return
	}
	defer rows.Close()
	summary.Transactions = make([]dashboardTransaction, 0)
	for rows.Next() {
		var transaction dashboardTransaction
		if err := rows.Scan(
			&transaction.ID, &transaction.ProductName, &transaction.Quantity,
			&transaction.Price, &transaction.Total, &transaction.CreatedAt,
		); err != nil {
			log.Printf("scan recent dashboard transaction: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load dashboard transactions"})
			return
		}
		summary.Transactions = append(summary.Transactions, transaction)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate recent dashboard transactions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load dashboard transactions"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

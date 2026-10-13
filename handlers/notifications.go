package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type notificationProduct struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Stock    int64  `json:"stock"`
	MinStock int64  `json:"min_stock"`
}

type notificationInvoice struct {
	ID      string    `json:"id"`
	Code    string    `json:"code"`
	DueDate time.Time `json:"due_date"`
}

type notificationInstallment struct {
	ID          string    `json:"id"`
	InvoiceID   string    `json:"invoice_id"`
	InvoiceCode string    `json:"invoice_code"`
	DueDate     time.Time `json:"due_date"`
}

func GetNotificationSnapshot(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	snapshot := struct {
		Products     []notificationProduct     `json:"products"`
		Invoices     []notificationInvoice     `json:"invoices"`
		Installments []notificationInstallment `json:"installments"`
	}{
		Products:     make([]notificationProduct, 0),
		Invoices:     make([]notificationInvoice, 0),
		Installments: make([]notificationInstallment, 0),
	}

	productRows, err := conn.QueryContext(c.Request.Context(), `
		SELECT id, name, COALESCE(stock, 0), COALESCE(min_stock, 0)
		FROM products WHERE is_deleted = 0
	`)
	if err != nil {
		log.Printf("load notification products: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}
	for productRows.Next() {
		var product notificationProduct
		if err := productRows.Scan(&product.ID, &product.Name, &product.Stock, &product.MinStock); err != nil {
			_ = productRows.Close()
			log.Printf("scan notification product: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
			return
		}
		snapshot.Products = append(snapshot.Products, product)
	}
	if err := productRows.Err(); err != nil {
		_ = productRows.Close()
		log.Printf("iterate notification products: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}
	if err := productRows.Close(); err != nil {
		log.Printf("close notification products: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}

	invoiceRows, err := conn.QueryContext(c.Request.Context(), `
		SELECT id, code, due_date
		FROM invoices WHERE status != 'PAID' AND due_date IS NOT NULL
	`)
	if err != nil {
		log.Printf("load notification invoices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}
	for invoiceRows.Next() {
		var invoice notificationInvoice
		if err := invoiceRows.Scan(&invoice.ID, &invoice.Code, &invoice.DueDate); err != nil {
			_ = invoiceRows.Close()
			log.Printf("scan notification invoice: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
			return
		}
		snapshot.Invoices = append(snapshot.Invoices, invoice)
	}
	if err := invoiceRows.Err(); err != nil {
		_ = invoiceRows.Close()
		log.Printf("iterate notification invoices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}
	if err := invoiceRows.Close(); err != nil {
		log.Printf("close notification invoices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}

	installmentRows, err := conn.QueryContext(c.Request.Context(), `
		SELECT installment.id, installment.invoice_id, invoice.code, installment.due_date
		FROM invoice_installments AS installment
		JOIN invoices AS invoice ON invoice.id = installment.invoice_id
		WHERE installment.status != 'PAID' AND installment.due_date IS NOT NULL
	`)
	if err != nil {
		log.Printf("load notification installments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}
	for installmentRows.Next() {
		var installment notificationInstallment
		if err := installmentRows.Scan(
			&installment.ID, &installment.InvoiceID, &installment.InvoiceCode,
			&installment.DueDate,
		); err != nil {
			_ = installmentRows.Close()
			log.Printf("scan notification installment: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
			return
		}
		snapshot.Installments = append(snapshot.Installments, installment)
	}
	if err := installmentRows.Err(); err != nil {
		_ = installmentRows.Close()
		log.Printf("iterate notification installments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}
	if err := installmentRows.Close(); err != nil {
		log.Printf("close notification installments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification data"})
		return
	}

	c.JSON(http.StatusOK, snapshot)
}

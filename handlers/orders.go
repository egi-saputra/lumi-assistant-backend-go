package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"assistant/utils"

	"github.com/gin-gonic/gin"
)

type createOrderRequest struct {
	Code             string            `json:"code" binding:"required"`
	Customer         string            `json:"customer"`
	Contact          string            `json:"contact"`
	Address          string            `json:"address"`
	PaymentMethod    string            `json:"payment_method" binding:"required"`
	DiscountType     string            `json:"discount_type"`
	Discount         int64             `json:"discount"`
	Tax              int64             `json:"tax"`
	DueDate          *string           `json:"due_date"`
	InstallmentTenor int               `json:"installment_tenor"`
	InstallmentStart *string           `json:"installment_start_date"`
	Items            []createOrderItem `json:"items" binding:"required"`
}

type createOrderItem struct {
	ProductID string `json:"product_id" binding:"required"`
	Quantity  int64  `json:"qty" binding:"required"`
}

type createdOrderResponse struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	TotalAmount int64  `json:"total_amount"`
	Status      string `json:"status"`
}

type orderListResponse struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Customer    *string   `json:"customer"`
	Contact     *string   `json:"contact"`
	Status      string    `json:"status"`
	TotalAmount int64     `json:"total_amount"`
	Paid        int64     `json:"paid"`
	CreatedAt   time.Time `json:"created_at"`
}

type orderItemResponse struct {
	ID             string  `json:"id"`
	ProductID      *string `json:"product_id"`
	ProductName    string  `json:"product_name"`
	ProductBarcode *string `json:"product_barcode"`
	Quantity       int64   `json:"qty"`
	Price          int64   `json:"price"`
	CostPrice      int64   `json:"cost_price"`
	Profit         int64   `json:"profit"`
}

type orderPaymentResponse struct {
	ID        string    `json:"id"`
	Amount    int64     `json:"amount"`
	Method    *string   `json:"method"`
	Note      *string   `json:"note"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type orderInstallmentResponse struct {
	ID                string     `json:"id"`
	InstallmentNumber int        `json:"installment_number"`
	DueDate           time.Time  `json:"due_date"`
	Amount            int64      `json:"amount"`
	PaidAmount        int64      `json:"paid_amount"`
	Status            string     `json:"status"`
	PaidAt            *time.Time `json:"paid_at"`
}

type orderDetailResponse struct {
	ID            string                     `json:"id"`
	Code          string                     `json:"code"`
	Customer      *string                    `json:"customer"`
	Contact       *string                    `json:"contact"`
	Address       *string                    `json:"address"`
	PaymentMethod string                     `json:"payment_method"`
	DiscountType  string                     `json:"discount_type"`
	Discount      int64                      `json:"discount"`
	Tax           int64                      `json:"tax"`
	TotalAmount   int64                      `json:"total_amount"`
	Status        string                     `json:"status"`
	DueDate       *time.Time                 `json:"due_date"`
	CreatedAt     time.Time                  `json:"created_at"`
	Items         []orderItemResponse        `json:"items"`
	Payments      []orderPaymentResponse     `json:"payments"`
	Installments  []orderInstallmentResponse `json:"installments"`
}

type createPaymentRequest struct {
	Amount int64  `json:"amount" binding:"required"`
	Method string `json:"method"`
	Note   string `json:"note"`
}

type lockedOrderProduct struct {
	ID        string
	Name      string
	Barcode   sql.NullString
	Price     int64
	CostPrice int64
	Stock     int64
	Quantity  int64
}

func ListOrders(c *gin.Context) {
	limit := 50
	offset := 0
	if value := c.Query("limit"); value != "" {
		parsed, err := parseBoundedInt(value, 1, 100)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Limit must be between 1 and 100"})
			return
		}
		limit = parsed
	}
	if value := c.Query("offset"); value != "" {
		parsed, err := parseBoundedInt(value, 0, 1000000)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid offset"})
			return
		}
		offset = parsed
	}
	status := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	if status != "" && status != "UNPAID" && status != "PARTIAL" && status != "PAID" && status != "RETURNED" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order status"})
		return
	}
	conn := c.MustGet("tenant_db").(*sql.DB)
	query := `
		SELECT invoice.id, invoice.code, invoice.customer, invoice.contact,
		       invoice.status, invoice.total_amount,
		       COALESCE(SUM(CASE WHEN payment.type = 'IN' THEN payment.amount ELSE -payment.amount END), 0) AS paid,
		       invoice.created_at
		FROM invoices AS invoice
		LEFT JOIN invoice_payments AS payment ON payment.invoice_id = invoice.id
	`
	args := make([]interface{}, 0, 3)
	if status != "" {
		query += " WHERE invoice.status = ?"
		args = append(args, status)
	}
	query += `
		GROUP BY invoice.id
		ORDER BY invoice.created_at DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, limit, offset)

	rows, err := conn.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		log.Printf("list tenant orders: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load orders"})
		return
	}
	defer rows.Close()
	orders := make([]orderListResponse, 0)
	for rows.Next() {
		var order orderListResponse
		var customer, contact sql.NullString
		if err := rows.Scan(
			&order.ID, &order.Code, &customer, &contact, &order.Status,
			&order.TotalAmount, &order.Paid, &order.CreatedAt,
		); err != nil {
			log.Printf("scan tenant order: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load orders"})
			return
		}
		order.Customer = nullableString(customer)
		order.Contact = nullableString(contact)
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate tenant orders: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load orders"})
		return
	}
	c.JSON(http.StatusOK, orders)
}

func GetOrder(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	var order orderDetailResponse
	var customer, contact, address sql.NullString
	var dueDate sql.NullTime
	err := conn.QueryRowContext(c.Request.Context(), `
		SELECT id, code, customer, contact, address, payment_method,
		       discount_type, discount, tax, total_amount, status, due_date, created_at
		FROM invoices WHERE id = ?
	`, c.Param("id")).Scan(
		&order.ID, &order.Code, &customer, &contact, &address, &order.PaymentMethod,
		&order.DiscountType, &order.Discount, &order.Tax, &order.TotalAmount,
		&order.Status, &dueDate, &order.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		} else {
			log.Printf("load tenant order: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load order"})
		}
		return
	}
	order.Customer = nullableString(customer)
	order.Contact = nullableString(contact)
	order.Address = nullableString(address)
	if dueDate.Valid {
		order.DueDate = &dueDate.Time
	}
	order.Items = make([]orderItemResponse, 0)
	order.Payments = make([]orderPaymentResponse, 0)
	order.Installments = make([]orderInstallmentResponse, 0)

	itemRows, err := conn.QueryContext(c.Request.Context(), `
		SELECT id, product_id, product_name, product_barcode, qty, price, cost_price, profit
		FROM invoice_items WHERE invoice_id = ? ORDER BY created_at, id
	`, order.ID)
	if err != nil {
		log.Printf("load tenant order items: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load order items"})
		return
	}
	for itemRows.Next() {
		var item orderItemResponse
		var productID, barcode sql.NullString
		if err := itemRows.Scan(
			&item.ID, &productID, &item.ProductName, &barcode, &item.Quantity,
			&item.Price, &item.CostPrice, &item.Profit,
		); err != nil {
			itemRows.Close()
			log.Printf("scan tenant order item: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load order items"})
			return
		}
		item.ProductID = nullableString(productID)
		item.ProductBarcode = nullableString(barcode)
		order.Items = append(order.Items, item)
	}
	if err := itemRows.Err(); err != nil {
		itemRows.Close()
		log.Printf("iterate tenant order items: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load order items"})
		return
	}
	itemRows.Close()

	paymentRows, err := conn.QueryContext(c.Request.Context(), `
		SELECT id, amount, method, note, type, created_at
		FROM invoice_payments WHERE invoice_id = ? ORDER BY created_at DESC
	`, order.ID)
	if err != nil {
		log.Printf("load tenant order payments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load order payments"})
		return
	}
	for paymentRows.Next() {
		var payment orderPaymentResponse
		var method, note sql.NullString
		if err := paymentRows.Scan(
			&payment.ID, &payment.Amount, &method, &note, &payment.Type, &payment.CreatedAt,
		); err != nil {
			paymentRows.Close()
			log.Printf("scan tenant order payment: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load order payments"})
			return
		}
		payment.Method = nullableString(method)
		payment.Note = nullableString(note)
		order.Payments = append(order.Payments, payment)
	}
	if err := paymentRows.Err(); err != nil {
		paymentRows.Close()
		log.Printf("iterate tenant order payments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load order payments"})
		return
	}
	paymentRows.Close()

	installmentRows, err := conn.QueryContext(c.Request.Context(), `
		SELECT id, installment_number, due_date, amount, paid_amount, status, paid_at
		FROM invoice_installments WHERE invoice_id = ? ORDER BY installment_number
	`, order.ID)
	if err != nil {
		log.Printf("load tenant order installments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load installment schedule"})
		return
	}
	for installmentRows.Next() {
		var installment orderInstallmentResponse
		var paidAt sql.NullTime
		if err := installmentRows.Scan(
			&installment.ID, &installment.InstallmentNumber, &installment.DueDate,
			&installment.Amount, &installment.PaidAmount, &installment.Status, &paidAt,
		); err != nil {
			installmentRows.Close()
			log.Printf("scan tenant installment: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load installment schedule"})
			return
		}
		if paidAt.Valid {
			installment.PaidAt = &paidAt.Time
		}
		order.Installments = append(order.Installments, installment)
	}
	if err := installmentRows.Err(); err != nil {
		installmentRows.Close()
		log.Printf("iterate tenant installments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load installment schedule"})
		return
	}
	installmentRows.Close()

	c.JSON(http.StatusOK, order)
}

func ReturnOrder(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant order return: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to return order"})
		return
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(c.Request.Context(), `
			SELECT status FROM invoices WHERE id = ? FOR UPDATE
		`, c.Param("id")).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		} else {
			log.Printf("lock invoice for return: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to return order"})
		}
		return
	}
	if status == "RETURNED" {
		c.JSON(http.StatusConflict, gin.H{"error": "Order has already been returned"})
		return
	}

	rows, err := tx.QueryContext(c.Request.Context(), `
			SELECT product_id, qty FROM invoice_items WHERE invoice_id = ?
		`, c.Param("id"))
	if err != nil {
		log.Printf("load invoice items for return: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to return order"})
		return
	}
	type returnedItem struct {
		productID sql.NullString
		quantity  int64
	}
	items := make([]returnedItem, 0)
	for rows.Next() {
		var item returnedItem
		if err := rows.Scan(&item.productID, &item.quantity); err != nil {
			rows.Close()
			log.Printf("scan invoice item for return: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to return order"})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		log.Printf("iterate invoice items for return: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to return order"})
		return
	}
	rows.Close()

	now := time.Now().UTC()
	for _, item := range items {
		if !item.productID.Valid {
			continue
		}
		if _, err := tx.ExecContext(c.Request.Context(), `
				UPDATE products SET stock = stock + ?, updated_at = ?
				WHERE id = ? AND is_deleted = 0
			`, item.quantity, now, item.productID.String); err != nil {
			log.Printf("restore returned product stock: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore returned stock"})
			return
		}
		if _, err := tx.ExecContext(c.Request.Context(), `
				INSERT INTO stock_movements (
					id, product_id, type, qty, purchase_price, remaining_qty, note, created_at, updated_at
				) VALUES (?, ?, 'IN', ?, 0, 0, ?, ?, ?)
			`, utils.NewULID(), item.productID.String, item.quantity,
			"Retur invoice #"+c.Param("id"), now, now); err != nil {
			log.Printf("record returned stock movement: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record returned stock"})
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
			UPDATE invoices SET status = 'RETURNED', updated_at = ? WHERE id = ?
		`, now, c.Param("id")); err != nil {
		log.Printf("mark tenant invoice returned: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to return order"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit tenant order return: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to return order"})
		return
	}
	c.Status(http.StatusNoContent)
}

func DeleteOrder(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant order cancellation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel order"})
		return
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(c.Request.Context(), `
			SELECT status FROM invoices WHERE id = ? FOR UPDATE
		`, c.Param("id")).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		} else {
			log.Printf("lock invoice for cancellation: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel order"})
		}
		return
	}
	var paymentCount int
	if err := tx.QueryRowContext(c.Request.Context(), `
			SELECT COUNT(*) FROM invoice_payments WHERE invoice_id = ?
		`, c.Param("id")).Scan(&paymentCount); err != nil {
		log.Printf("check payments before cancellation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel order"})
		return
	}
	if paymentCount > 0 || status == "RETURNED" {
		c.JSON(http.StatusConflict, gin.H{"error": "Only orders without payments and returns can be deleted"})
		return
	}

	rows, err := tx.QueryContext(c.Request.Context(), `
			SELECT product_id, qty FROM invoice_items WHERE invoice_id = ?
		`, c.Param("id"))
	if err != nil {
		log.Printf("load items before cancellation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel order"})
		return
	}
	type cancelledItem struct {
		productID sql.NullString
		quantity  int64
	}
	items := make([]cancelledItem, 0)
	for rows.Next() {
		var item cancelledItem
		if err := rows.Scan(&item.productID, &item.quantity); err != nil {
			rows.Close()
			log.Printf("scan items before cancellation: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel order"})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		log.Printf("iterate items before cancellation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel order"})
		return
	}
	rows.Close()

	now := time.Now().UTC()
	for _, item := range items {
		if !item.productID.Valid {
			continue
		}
		if _, err := tx.ExecContext(c.Request.Context(), `
				UPDATE products SET stock = stock + ?, updated_at = ? WHERE id = ?
			`, item.quantity, now, item.productID.String); err != nil {
			log.Printf("restore stock for cancelled order: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore stock"})
			return
		}
		if _, err := tx.ExecContext(c.Request.Context(), `
				INSERT INTO stock_movements (
					id, product_id, type, qty, purchase_price, remaining_qty, note, created_at, updated_at
				) VALUES (?, ?, 'IN', ?, 0, 0, ?, ?, ?)
			`, utils.NewULID(), item.productID.String, item.quantity,
			"Pembatalan pesanan "+c.Param("id"), now, now); err != nil {
			log.Printf("record stock for cancelled order: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record stock movement"})
			return
		}
	}
	for _, table := range []string{"invoice_installments", "invoice_items", "invoices"} {
		if _, err := tx.ExecContext(c.Request.Context(), "DELETE FROM "+table+" WHERE "+map[string]string{
			"invoice_installments": "invoice_id",
			"invoice_items":        "invoice_id",
			"invoices":             "id",
		}[table]+" = ?", c.Param("id")); err != nil {
			log.Printf("delete cancelled order from %s: %v", table, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete order"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit tenant order cancellation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete order"})
		return
	}
	c.Status(http.StatusNoContent)
}

func RefundOrderPayments(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant payment refund: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refund payments"})
		return
	}
	defer tx.Rollback()

	var code, status string
	if err := tx.QueryRowContext(c.Request.Context(), `
			SELECT code, status FROM invoices WHERE id = ? FOR UPDATE
		`, c.Param("id")).Scan(&code, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		} else {
			log.Printf("lock invoice for refund: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refund payments"})
		}
		return
	}
	var paid int64
	if err := tx.QueryRowContext(c.Request.Context(), `
			SELECT COALESCE(SUM(amount), 0) FROM invoice_payments
			WHERE invoice_id = ? AND type = 'IN'
		`, c.Param("id")).Scan(&paid); err != nil {
		log.Printf("sum invoice payments for refund: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate refund"})
		return
	}
	if paid <= 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "There are no payments to refund"})
		return
	}
	now := time.Now().UTC()
	note := "Refund pembayaran untuk invoice #" + code
	paymentID := utils.NewULID()
	if _, err := tx.ExecContext(c.Request.Context(), `
			INSERT INTO invoice_payments (id, invoice_id, amount, note, type, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'OUT', ?, ?)
		`, paymentID, c.Param("id"), paid, note, now, now); err != nil {
		log.Printf("insert refund payment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refund payments"})
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
			INSERT INTO cash_flows (id, type, source, reference_id, amount, note, created_at)
			VALUES (?, 'OUT', 'INVOICE_PAYMENT', ?, ?, ?, ?)
		`, utils.NewULID(), paymentID, paid, note, now); err != nil {
		log.Printf("insert refund cash flow: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refund payments"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit payment refund: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refund payments"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": paymentID, "amount": paid, "status": status})
}

func CreateOrderPayment(c *gin.Context) {
	var request createPaymentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A positive payment amount is required"})
		return
	}
	request.Method = strings.TrimSpace(request.Method)
	request.Note = strings.TrimSpace(request.Note)
	if request.Amount <= 0 || utf8.RuneCountInString(request.Method) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Payment amount must be positive and method at most 100 characters"})
		return
	}

	conn := c.MustGet("tenant_db").(*sql.DB)
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant order payment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record payment"})
		return
	}
	defer tx.Rollback()

	var total int64
	var status string
	if err := tx.QueryRowContext(c.Request.Context(), `
		SELECT total_amount, status FROM invoices WHERE id = ? FOR UPDATE
	`, c.Param("id")).Scan(&total, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		} else {
			log.Printf("lock tenant invoice for payment: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record payment"})
		}
		return
	}
	if status == "RETURNED" {
		c.JSON(http.StatusConflict, gin.H{"error": "A returned order cannot receive a payment"})
		return
	}

	var paid int64
	if err := tx.QueryRowContext(c.Request.Context(), `
		SELECT COALESCE(SUM(CASE WHEN type = 'IN' THEN amount ELSE -amount END), 0)
		FROM invoice_payments WHERE invoice_id = ?
	`, c.Param("id")).Scan(&paid); err != nil {
		log.Printf("sum tenant invoice payments: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate payment"})
		return
	}
	if paid < 0 || request.Amount > total-paid {
		c.JSON(http.StatusConflict, gin.H{"error": "Payment exceeds the remaining invoice balance"})
		return
	}

	if status == "PARTIAL" || status == "UNPAID" {
		rows, err := tx.QueryContext(c.Request.Context(), `
			SELECT id, amount, COALESCE(paid_amount, 0)
			FROM invoice_installments WHERE invoice_id = ?
			ORDER BY installment_number FOR UPDATE
		`, c.Param("id"))
		if err != nil {
			log.Printf("load installments for payment: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to allocate payment"})
			return
		}
		type installmentBalance struct {
			id     string
			amount int64
			paid   int64
		}
		installments := make([]installmentBalance, 0)
		for rows.Next() {
			var installment installmentBalance
			if err := rows.Scan(&installment.id, &installment.amount, &installment.paid); err != nil {
				rows.Close()
				log.Printf("scan installment for payment: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to allocate payment"})
				return
			}
			installments = append(installments, installment)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			log.Printf("iterate installments for payment: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to allocate payment"})
			return
		}
		rows.Close()

		remainingPayment := request.Amount
		now := time.Now().UTC()
		for _, installment := range installments {
			if remainingPayment == 0 {
				break
			}
			needed := installment.amount - installment.paid
			if needed <= 0 {
				continue
			}
			applied := remainingPayment
			if applied > needed {
				applied = needed
			}
			newPaid := installment.paid + applied
			newStatus := "PARTIAL"
			var paidAt interface{}
			if newPaid >= installment.amount {
				newStatus = "PAID"
				paidAt = now
			}
			if _, err := tx.ExecContext(c.Request.Context(), `
				UPDATE invoice_installments
				SET paid_amount = ?, status = ?, paid_at = ?, updated_at = ?
				WHERE id = ?
			`, newPaid, newStatus, paidAt, now, installment.id); err != nil {
				log.Printf("update paid installment: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to allocate payment"})
				return
			}
			remainingPayment -= applied
		}
		if remainingPayment != 0 && len(installments) > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "Installment schedule does not match the remaining invoice balance"})
			return
		}
	}

	now := time.Now().UTC()
	paymentID := utils.NewULID()
	if _, err := tx.ExecContext(c.Request.Context(), `
		INSERT INTO invoice_payments (id, invoice_id, amount, method, note, type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'IN', ?, ?)
	`, paymentID, c.Param("id"), request.Amount, nullableOrderText(request.Method),
		nullableOrderText(request.Note), now, now); err != nil {
		log.Printf("insert tenant invoice payment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record payment"})
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
		INSERT INTO cash_flows (id, type, source, reference_id, amount, note, created_at)
		VALUES (?, 'IN', 'INVOICE_PAYMENT', ?, ?, ?, ?)
	`, utils.NewULID(), paymentID, request.Amount, nullableOrderText(request.Note), now); err != nil {
		log.Printf("insert invoice payment cash flow: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record payment"})
		return
	}

	newPaid := paid + request.Amount
	newStatus := "PARTIAL"
	if newPaid >= total {
		newStatus = "PAID"
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
		UPDATE invoices SET status = ?, updated_at = ? WHERE id = ?
	`, newStatus, now, c.Param("id")); err != nil {
		log.Printf("update invoice payment status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update invoice status"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit tenant invoice payment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record payment"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id": paymentID, "amount": request.Amount, "paid": newPaid,
		"remaining": total - newPaid, "status": newStatus, "created_at": now,
	})
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func CreateOrder(c *gin.Context) {
	var request createOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order"})
		return
	}
	if err := validateCreateOrder(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	conn := c.MustGet("tenant_db").(*sql.DB)
	if _, err := ownerAccountForInvoiceQuota(tenantID, conn); err != nil {
		log.Printf("resolve invoice quota owner for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check invoice quota"})
		return
	}
	_, quotaReservationID, allowed, quota, err := reserveInvoiceQuota(c.Request.Context(), tenantID)
	if err != nil {
		log.Printf("reserve invoice quota for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check invoice quota"})
		return
	}
	if !allowed {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code":          "FREE_INVOICE_QUOTA",
			"error":         "Kuota invoice gratis habis. Tonton iklan untuk mendapat 3 invoice tambahan atau upgrade ke Premium.",
			"used":          quota.Used,
			"limit":         quota.Limit,
			"bonus_credits": quota.BonusCredits,
		})
		return
	}
	quotaSettled := quotaReservationID == ""
	defer func() {
		if !quotaSettled {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := releaseInvoiceQuota(ctx, quotaReservationID); err != nil {
				log.Printf("release invoice quota reservation %s: %v", quotaReservationID, err)
			}
		}
	}()

	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant order: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create order"})
		return
	}
	defer tx.Rollback()

	quantities := make(map[string]int64, len(request.Items))
	for _, item := range request.Items {
		if quantities[item.ProductID] > math.MaxInt64-item.Quantity {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Product quantity is too large"})
			return
		}
		quantities[item.ProductID] += item.Quantity
	}
	productIDs := make([]string, 0, len(quantities))
	for id := range quantities {
		productIDs = append(productIDs, id)
	}
	sort.Strings(productIDs)

	products := make(map[string]lockedOrderProduct, len(productIDs))
	for _, id := range productIDs {
		var product lockedOrderProduct
		if err := tx.QueryRowContext(c.Request.Context(), `
			SELECT id, name, barcode, price, cost_price, stock
			FROM products
			WHERE id = ? AND is_deleted = 0
			FOR UPDATE
		`, id).Scan(&product.ID, &product.Name, &product.Barcode, &product.Price, &product.CostPrice, &product.Stock); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.JSON(http.StatusConflict, gin.H{"error": "A selected product is no longer available"})
			} else {
				log.Printf("lock order product %s: %v", id, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate order products"})
			}
			return
		}
		product.Quantity = quantities[id]
		if product.Quantity > product.Stock {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Insufficient stock for %s", product.Name)})
			return
		}
		products[id] = product
	}

	subtotal := int64(0)
	for _, id := range productIDs {
		product := products[id]
		lineTotal, ok := multiplyNonNegative(product.Price, product.Quantity)
		if !ok || subtotal > math.MaxInt64-lineTotal {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Order total is too large"})
			return
		}
		subtotal += lineTotal
	}
	discountAmount, err := calculateOrderDiscount(subtotal, request.DiscountType, request.Discount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	afterDiscount := subtotal - discountAmount
	taxAmount := percentOf(afterDiscount, request.Tax)
	if afterDiscount > math.MaxInt64-taxAmount {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Order total is too large"})
		return
	}
	total := afterDiscount + taxAmount

	now := time.Now().UTC()
	invoiceID := utils.NewULID()
	status := "UNPAID"
	if request.PaymentMethod == "installment" {
		status = "PARTIAL"
	}
	var dueDate interface{}
	if request.DueDate != nil {
		parsed, _ := time.Parse("2006-01-02", *request.DueDate)
		dueDate = parsed
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
		INSERT INTO invoices (
			id, code, customer, contact, address, payment_method, discount_type, discount, tax,
			total_amount, status, due_date, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, invoiceID, request.Code, nullableOrderText(request.Customer), nullableOrderText(request.Contact),
		nullableOrderText(request.Address), request.PaymentMethod, request.DiscountType, request.Discount,
		request.Tax, total, status, dueDate, now, now); err != nil {
		log.Printf("insert tenant invoice: %v", err)
		c.JSON(http.StatusConflict, gin.H{"error": "Invoice code is already used or order could not be saved"})
		return
	}

	for _, id := range productIDs {
		product := products[id]
		lineTotal, _ := multiplyNonNegative(product.Price, product.Quantity)
		lineDiscount := int64(0)
		if subtotal > 0 && discountAmount > 0 {
			lineDiscount = proportionalAmount(discountAmount, lineTotal, subtotal)
		}
		profit, ok := multiplySigned(product.Price-product.CostPrice, product.Quantity)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Order profit is too large"})
			return
		}
		if profit < math.MinInt64+lineDiscount {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Order profit is too large"})
			return
		}
		profit -= lineDiscount

		itemID := utils.NewULID()
		if _, err := tx.ExecContext(c.Request.Context(), `
			INSERT INTO invoice_items (
				id, invoice_id, product_id, product_name, product_barcode, qty, price, cost_price, profit, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, itemID, invoiceID, product.ID, product.Name, nullableOrderBarcode(product.Barcode),
			product.Quantity, product.Price, product.CostPrice, profit, now, now); err != nil {
			log.Printf("insert tenant invoice item: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save order items"})
			return
		}
		if _, err := tx.ExecContext(c.Request.Context(), `
			UPDATE products SET stock = stock - ?, updated_at = ? WHERE id = ?
		`, product.Quantity, now, product.ID); err != nil {
			log.Printf("decrease product stock for invoice: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product stock"})
			return
		}
		movementID := utils.NewULID()
		if _, err := tx.ExecContext(c.Request.Context(), `
			INSERT INTO stock_movements (
				id, product_id, type, qty, purchase_price, remaining_qty, note, created_at, updated_at
			) VALUES (?, ?, 'OUT', ?, 0, 0, ?, ?, ?)
		`, movementID, product.ID, product.Quantity, "Penjualan "+request.Code, now, now); err != nil {
			log.Printf("insert sale stock movement: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record stock movement"})
			return
		}
	}

	if request.PaymentMethod == "installment" {
		base := total / int64(request.InstallmentTenor)
		remainder := total % int64(request.InstallmentTenor)
		startDate, _ := time.Parse("2006-01-02", *request.InstallmentStart)
		for index := 0; index < request.InstallmentTenor; index++ {
			amount := base
			if int64(index) < remainder {
				amount++
			}
			due := startDate.AddDate(0, index, 0)
			if _, err := tx.ExecContext(c.Request.Context(), `
				INSERT INTO invoice_installments (
					id, invoice_id, installment_number, due_date, amount, paid_amount,
					status, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, 0, 'UNPAID', ?, ?)
			`, utils.NewULID(), invoiceID, index+1, due, amount, now, now); err != nil {
				log.Printf("insert invoice installment: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create installment schedule"})
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit tenant order: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create order"})
		return
	}
	quotaSettled = true
	if err := settleInvoiceQuota(context.Background(), quotaReservationID, invoiceID); err != nil {
		log.Printf("settle invoice quota reservation %s for invoice %s: %v", quotaReservationID, invoiceID, err)
	}

	c.JSON(http.StatusCreated, createdOrderResponse{
		ID: invoiceID, Code: request.Code, TotalAmount: total, Status: status,
	})
}

func validateCreateOrder(request *createOrderRequest) error {
	request.Code = strings.TrimSpace(request.Code)
	request.Customer = strings.TrimSpace(request.Customer)
	request.Contact = strings.TrimSpace(request.Contact)
	request.Address = strings.TrimSpace(request.Address)
	request.PaymentMethod = strings.ToLower(strings.TrimSpace(request.PaymentMethod))
	request.DiscountType = strings.ToLower(strings.TrimSpace(request.DiscountType))
	if request.Code == "" || utf8.RuneCountInString(request.Code) > 255 {
		return errors.New("invoice code is required and must be at most 255 characters")
	}
	if utf8.RuneCountInString(request.Customer) > 255 || utf8.RuneCountInString(request.Contact) > 255 {
		return errors.New("customer name or contact is too long")
	}
	if request.PaymentMethod != "cash" && request.PaymentMethod != "transfer" && request.PaymentMethod != "installment" {
		return errors.New("payment method must be cash, transfer or installment")
	}
	if request.DiscountType == "" {
		request.DiscountType = "percent"
	}
	if request.DiscountType != "percent" && request.DiscountType != "amount" {
		return errors.New("discount type must be percent or amount")
	}
	if request.Discount < 0 || request.Tax < 0 || request.Tax > 100 {
		return errors.New("discount or tax is out of range")
	}
	if len(request.Items) == 0 || len(request.Items) > 500 {
		return errors.New("order must contain between 1 and 500 items")
	}
	for _, item := range request.Items {
		if (len(item.ProductID) != 26 && len(item.ProductID) != 32) || item.Quantity <= 0 {
			return errors.New("each item must include a valid product ID and positive quantity")
		}
	}
	if request.DueDate != nil {
		if _, err := time.Parse("2006-01-02", *request.DueDate); err != nil {
			return errors.New("due date must use YYYY-MM-DD format")
		}
	}
	if request.PaymentMethod == "installment" {
		if request.InstallmentTenor < 1 || request.InstallmentTenor > 24 || request.InstallmentStart == nil {
			return errors.New("installment tenor must be 1 to 24 and start date is required")
		}
		if _, err := time.Parse("2006-01-02", *request.InstallmentStart); err != nil {
			return errors.New("installment start date must use YYYY-MM-DD format")
		}
	} else if request.InstallmentTenor != 0 || request.InstallmentStart != nil {
		return errors.New("installment schedule is only valid for installment payment method")
	}
	return nil
}

func calculateOrderDiscount(subtotal int64, discountType string, discount int64) (int64, error) {
	if discountType == "percent" {
		if discount > 100 {
			return 0, errors.New("percentage discount cannot exceed 100")
		}
		return percentOf(subtotal, discount), nil
	}
	if discount > subtotal {
		return 0, errors.New("discount exceeds order subtotal")
	}
	return discount, nil
}

func percentOf(amount, percent int64) int64 {
	return (amount/100)*percent + (amount%100)*percent/100
}

func proportionalAmount(amount, part, total int64) int64 {
	numerator := new(big.Int).Mul(big.NewInt(amount), big.NewInt(part))
	numerator.Quo(numerator, big.NewInt(total))
	return numerator.Int64()
}

func multiplyNonNegative(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || (right != 0 && left > math.MaxInt64/right) {
		return 0, false
	}
	return left * right, true
}

func multiplySigned(left, right int64) (int64, bool) {
	if left == 0 || right == 0 {
		return 0, true
	}
	if left == math.MinInt64 || right == math.MinInt64 {
		if left == -1 || right == -1 {
			return 0, false
		}
	}
	result := left * right
	if result/right != left {
		return 0, false
	}
	return result, true
}

func nullableOrderText(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func nullableOrderBarcode(value sql.NullString) interface{} {
	if !value.Valid {
		return nil
	}
	return value.String
}

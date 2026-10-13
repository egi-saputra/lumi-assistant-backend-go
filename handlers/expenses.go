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

type expenseRequest struct {
	Name   string `json:"name" binding:"required"`
	Amount int64  `json:"amount" binding:"required"`
}

type expenseResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Amount    int64     `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

type cashFlowResponse struct {
	Type   string `json:"type"`
	Amount int64  `json:"amount"`
}

func ListExpenses(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	rows, err := conn.Query(`
		SELECT id, name, amount, created_at
		FROM expenses
		ORDER BY created_at DESC
	`)
	if err != nil {
		log.Printf("list tenant expenses: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load expenses"})
		return
	}
	defer rows.Close()

	expenses := make([]expenseResponse, 0)
	for rows.Next() {
		var expense expenseResponse
		var name sql.NullString
		if err := rows.Scan(&expense.ID, &name, &expense.Amount, &expense.CreatedAt); err != nil {
			log.Printf("scan tenant expense: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load expenses"})
			return
		}
		if name.Valid {
			expense.Name = name.String
		}
		expenses = append(expenses, expense)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate tenant expenses: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load expenses"})
		return
	}
	c.JSON(http.StatusOK, expenses)
}

func CreateExpense(c *gin.Context) {
	var request expenseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and positive amount are required"})
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if err := validateExpense(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	conn := c.MustGet("tenant_db").(*sql.DB)
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin create tenant expense: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create expense"})
		return
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	expenseID := utils.NewULID()
	cashFlowID := utils.NewULID()
	if _, err := tx.ExecContext(c.Request.Context(), `
		INSERT INTO expenses (id, name, amount, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, expenseID, request.Name, request.Amount, now, now); err != nil {
		log.Printf("insert tenant expense: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create expense"})
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
		INSERT INTO cash_flows (id, type, source, reference_id, amount, note, created_at)
		VALUES (?, 'OUT', 'EXPENSE', ?, ?, ?, ?)
	`, cashFlowID, expenseID, request.Amount, request.Name, now); err != nil {
		log.Printf("insert tenant expense cash flow: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create expense"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit tenant expense: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create expense"})
		return
	}

	c.JSON(http.StatusCreated, expenseResponse{
		ID: expenseID, Name: request.Name, Amount: request.Amount, CreatedAt: now,
	})
}

func ListCashFlows(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	rows, err := conn.Query("SELECT type, amount FROM cash_flows")
	if err != nil {
		log.Printf("list tenant cash flows: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load cash flows"})
		return
	}
	defer rows.Close()

	flows := make([]cashFlowResponse, 0)
	for rows.Next() {
		var flow cashFlowResponse
		if err := rows.Scan(&flow.Type, &flow.Amount); err != nil {
			log.Printf("scan tenant cash flow: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load cash flows"})
			return
		}
		flows = append(flows, flow)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate tenant cash flows: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load cash flows"})
		return
	}
	c.JSON(http.StatusOK, flows)
}

func validateExpense(request expenseRequest) error {
	if request.Name == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(request.Name) > 255 {
		return errors.New("name must be at most 255 characters")
	}
	if request.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}

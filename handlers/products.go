package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"assistant/utils"

	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
)

type productRequest struct {
	Name            string `json:"name" binding:"required"`
	Unit            string `json:"unit"`
	Price           int64  `json:"price"`
	CostPrice       int64  `json:"cost_price"`
	ValuationMethod string `json:"valuation_method"`
	Barcode         string `json:"barcode"`
	ProductType     string `json:"jenis_produk"`
	Stock           int64  `json:"stock"`
	MinStock        int64  `json:"min_stock"`
	Discount        int64  `json:"discount"`
	Tax             int64  `json:"tax"`
	Description     string `json:"description"`
	SmartEngine     bool   `json:"smart_engine"`
}

type productResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	HasImage        bool      `json:"has_image"`
	Unit            string    `json:"unit"`
	Price           int64     `json:"price"`
	CostPrice       int64     `json:"cost_price"`
	ValuationMethod string    `json:"valuation_method"`
	Barcode         *string   `json:"barcode"`
	ProductType     string    `json:"jenis_produk"`
	Stock           int64     `json:"stock"`
	MinStock        int64     `json:"min_stock"`
	Discount        int64     `json:"discount"`
	Tax             int64     `json:"tax"`
	IsDeleted       bool      `json:"is_deleted"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type stockMovementRequest struct {
	Type          string `json:"type" binding:"required"`
	Quantity      int64  `json:"qty" binding:"required"`
	Note          string `json:"note" binding:"required"`
	MinStock      *int64 `json:"min_stock"`
	PurchasePrice int64  `json:"purchase_price"`
}

type stockMovementResponse struct {
	ID          string    `json:"id"`
	ProductID   string    `json:"product_id"`
	ProductName string    `json:"product_name"`
	Type        string    `json:"type"`
	Quantity    int64     `json:"qty"`
	Note        *string   `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
}

func ListProducts(c *gin.Context) {
	limit := 30
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

	search := strings.TrimSpace(c.Query("search"))
	if utf8.RuneCountInString(search) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Search query is too long"})
		return
	}
	barcode := strings.TrimSpace(c.Query("barcode"))
	if utf8.RuneCountInString(barcode) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Barcode is too long"})
		return
	}
	conn := c.MustGet("tenant_db").(*sql.DB)
	query := `
		SELECT id, name, COALESCE(unit, 'item'), price, cost_price,
		       COALESCE(valuation_method, 'AVG'), barcode, COALESCE(description, ''), (image_path IS NOT NULL AND image_path <> ''), COALESCE(jenis_produk, 'Finished'),
		       COALESCE(stock, 0), COALESCE(min_stock, 0), COALESCE(discount, 0),
		       COALESCE(tax, 0), COALESCE(is_deleted, 0), created_at, updated_at
		FROM products
		WHERE is_deleted = 0 AND name LIKE ? AND (? = '' OR barcode = ?)
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`
	rows, err := conn.Query(query, "%"+search+"%", barcode, barcode, limit, offset)
	if err != nil {
		log.Printf("list tenant products: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
		return
	}
	defer rows.Close()

	products := make([]productResponse, 0)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			log.Printf("scan tenant product: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
			return
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate tenant products: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
		return
	}
	c.JSON(http.StatusOK, products)
}

func GetProduct(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	product, err := scanProduct(conn.QueryRow(`
		SELECT id, name, COALESCE(unit, 'item'), price, cost_price,
		       COALESCE(valuation_method, 'AVG'), barcode, COALESCE(description, ''), (image_path IS NOT NULL AND image_path <> ''), COALESCE(jenis_produk, 'Finished'),
		       COALESCE(stock, 0), COALESCE(min_stock, 0), COALESCE(discount, 0),
		       COALESCE(tax, 0), COALESCE(is_deleted, 0), created_at, updated_at
		FROM products WHERE id = ? AND is_deleted = 0
	`, c.Param("id")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
			return
		}
		log.Printf("load tenant product: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product"})
		return
	}
	c.JSON(http.StatusOK, product)
}

func CreateProduct(c *gin.Context) {
	var request productRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product"})
		return
	}
	if err := validateProduct(&request, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().UTC()
	id := utils.NewULID()
	conn := c.MustGet("tenant_db").(*sql.DB)
	locked, err := lockProductQuota(c.Request.Context(), c.GetString("tenant_id"), conn)
	if err != nil {
		log.Printf("lock product quota before creation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create product"})
		return
	}
	defer locked.tx.Rollback()
	if locked.quota.Plan == "free" && locked.quota.Count >= locked.quota.Limit {
		writeProductQuotaError(c, errFreeProductLimit, locked.quota)
		return
	}
	if locked.quota.Plan == "free" && request.SmartEngine && locked.quota.SmartEngineCredits < 1 {
		writeProductQuotaError(c, errSmartEngineRewardNeeded, locked.quota)
		return
	}
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant product creation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Product operation failed"})
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(c.Request.Context(), `
		INSERT INTO products (
			id, name, unit, price, cost_price, valuation_method, barcode, description, jenis_produk,
			stock, min_stock, discount, tax, is_deleted, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, 0, ?, ?)
	`, id, request.Name, request.Unit, request.Price, request.CostPrice,
		request.ValuationMethod, nullableProductBarcode(request.Barcode), request.Description, request.ProductType,
		request.Stock, request.MinStock, request.Discount, request.Tax, now, now)
	if err != nil {
		writeProductError(c, "create tenant product", err)
		return
	}
	if request.Stock > 0 {
		if _, err := tx.ExecContext(c.Request.Context(), `
			INSERT INTO stock_movements (
				id, product_id, type, qty, purchase_price, remaining_qty, note, created_at, updated_at
			) VALUES (?, ?, 'INITIAL', ?, ?, ?, 'Saldo awal', ?, ?)
		`, utils.NewULID(), id, request.Stock, request.CostPrice, request.Stock, now, now); err != nil {
			log.Printf("record initial tenant product stock: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record initial stock"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit tenant product creation: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Product operation failed"})
		return
	}
	if locked.quota.Plan == "free" && request.SmartEngine {
		if _, err := locked.tx.ExecContext(c.Request.Context(), `
			UPDATE tenants
			SET free_smart_engine_credits = free_smart_engine_credits - 1
			WHERE id = ? AND owner_account_id = ? AND free_smart_engine_credits > 0
		`, c.GetString("tenant_id"), locked.accountID); err != nil {
			log.Printf("consume Smart Engine credit for tenant %s: %v", c.GetString("tenant_id"), err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to activate Smart Engine"})
			return
		}
	}
	if err := locked.tx.Commit(); err != nil {
		log.Printf("commit product quota for tenant %s: %v", c.GetString("tenant_id"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create product"})
		return
	}

	c.JSON(http.StatusCreated, productResponse{
		ID: id, Name: request.Name, Description: request.Description, Unit: request.Unit, Price: request.Price,
		CostPrice: request.CostPrice, ValuationMethod: request.ValuationMethod,
		Barcode: nullableProductBarcode(request.Barcode), ProductType: request.ProductType,
		Stock: request.Stock, MinStock: request.MinStock, Discount: request.Discount,
		Tax: request.Tax, CreatedAt: now, UpdatedAt: now,
	})
}

func UpdateProduct(c *gin.Context) {
	var request productRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product"})
		return
	}
	if err := validateProduct(&request, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().UTC()
	conn := c.MustGet("tenant_db").(*sql.DB)
	locked, err := lockProductQuota(c.Request.Context(), c.GetString("tenant_id"), conn)
	if err != nil {
		log.Printf("lock product quota before update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product"})
		return
	}
	defer locked.tx.Rollback()
	if locked.quota.Plan == "free" && request.SmartEngine && locked.quota.SmartEngineCredits < 1 {
		writeProductQuotaError(c, errSmartEngineRewardNeeded, locked.quota)
		return
	}
	result, err := conn.ExecContext(c.Request.Context(), `
		UPDATE products
		SET name = ?, unit = ?, price = ?, cost_price = ?, valuation_method = ?,
		    barcode = ?, description = NULLIF(?, ''), jenis_produk = ?, discount = ?, tax = ?, updated_at = ?
		WHERE id = ? AND is_deleted = 0
	`, request.Name, request.Unit, request.Price, request.CostPrice, request.ValuationMethod,
		nullableProductBarcode(request.Barcode), request.Description, request.ProductType, request.Discount, request.Tax,
		now, c.Param("id"))
	if err != nil {
		writeProductError(c, "update tenant product", err)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		log.Printf("check updated tenant product: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product"})
		return
	}
	if affected == 0 {
		var exists bool
		if err := conn.QueryRow("SELECT EXISTS(SELECT 1 FROM products WHERE id = ? AND is_deleted = 0)", c.Param("id")).Scan(&exists); err != nil {
			log.Printf("check tenant product existence: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product"})
			return
		}
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
			return
		}
	}
	if locked.quota.Plan == "free" && request.SmartEngine {
		if _, err := locked.tx.ExecContext(c.Request.Context(), `
			UPDATE tenants
			SET free_smart_engine_credits = free_smart_engine_credits - 1
			WHERE id = ? AND owner_account_id = ? AND free_smart_engine_credits > 0
		`, c.GetString("tenant_id"), locked.accountID); err != nil {
			log.Printf("consume Smart Engine credit for tenant %s: %v", c.GetString("tenant_id"), err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to activate Smart Engine"})
			return
		}
	}
	if err := locked.tx.Commit(); err != nil {
		log.Printf("commit product update quota for tenant %s: %v", c.GetString("tenant_id"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "updated_at": now})
}

const maxProductImageSize = 5 << 20

func UploadProductImage(c *gin.Context) {
	productID := c.Param("id")
	conn := c.MustGet("tenant_db").(*sql.DB)
	premium, err := isPremiumTenant(c.Request.Context(), c.GetString("tenant_id"), conn)
	if err != nil {
		log.Printf("verify product image plan for tenant %s: %v", c.GetString("tenant_id"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify product image access"})
		return
	}
	if !premium {
		c.JSON(http.StatusForbidden, gin.H{"code": "PREMIUM_PRODUCT_IMAGE", "error": "Foto produk hanya tersedia dengan paket Starter Pack"})
		return
	}
	var oldPath sql.NullString
	if err := conn.QueryRow("SELECT image_path FROM products WHERE id = ? AND is_deleted = 0", productID).Scan(&oldPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		} else {
			log.Printf("load product before image upload: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload product image"})
		}
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxProductImageSize+64*1024)
	file, _, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "An image file is required"})
		return
	}
	defer file.Close()

	imageBytes, err := io.ReadAll(io.LimitReader(file, maxProductImageSize+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read image"})
		return
	}
	if len(imageBytes) == 0 || len(imageBytes) > maxProductImageSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Product images must be 5 MB or smaller"})
		return
	}
	contentType, supported := supportedImageContentType(imageBytes)
	extensionByType := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	}
	extension := extensionByType[contentType]
	if !supported {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Only JPEG, PNG, and WebP product images are supported"})
		return
	}

	tenantID := c.GetString("tenant_id")
	if tenantID == "" || filepath.Base(tenantID) != tenantID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid store"})
		return
	}
	filename := productID + "-" + utils.NewULID() + extension
	relativePath := filepath.ToSlash(filepath.Join(tenantID, filename))
	objectKey := "product-images/" + relativePath
	if err := uploadImageToR2(c, objectKey, imageBytes, contentType); err != nil {
		log.Printf("upload product image to R2 for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store product image"})
		return
	}

	result, err := conn.ExecContext(c.Request.Context(), `
		UPDATE products SET image_path = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND is_deleted = 0
	`, relativePath, productID)
	if err != nil {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 product image %q: %v", objectKey, deleteErr)
		}
		log.Printf("save product image path: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save product image"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 product image %q: %v", objectKey, deleteErr)
		}
		log.Printf("check saved product image: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save product image"})
		return
	}
	if affected == 0 {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 product image %q: %v", objectKey, deleteErr)
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	response := gin.H{"image_url": fmt.Sprintf("/products/%s/image", productID)}
	if oldPath.Valid && oldPath.String != relativePath {
		if cleanupErr := removeProductImage(c.Request.Context(), tenantID, productID, oldPath.String); cleanupErr != nil {
			log.Printf("remove replaced product image %q: %v", oldPath.String, cleanupErr)
			response["cleanup_warning"] = "New image saved, but the previous image could not be removed from storage."
		}
	}
	c.JSON(http.StatusOK, response)
}

func GetProductImage(c *gin.Context) {
	productID := c.Param("id")
	conn := c.MustGet("tenant_db").(*sql.DB)
	premium, err := isPremiumTenant(c.Request.Context(), c.GetString("tenant_id"), conn)
	if err != nil {
		log.Printf("verify product image plan for tenant %s: %v", c.GetString("tenant_id"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify product image access"})
		return
	}
	if !premium {
		c.JSON(http.StatusForbidden, gin.H{"code": "PREMIUM_PRODUCT_IMAGE", "error": "Foto produk hanya tersedia dengan paket Starter Pack"})
		return
	}
	var imagePath sql.NullString
	if err := conn.QueryRow("SELECT image_path FROM products WHERE id = ? AND is_deleted = 0", productID).Scan(&imagePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		} else {
			log.Printf("load product image path: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product image"})
		}
		return
	}
	tenantID := c.GetString("tenant_id")
	if !imagePath.Valid || !validProductImagePath(tenantID, productID, imagePath.String) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product image not found"})
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	serveR2Image(
		c,
		"product-images/"+imagePath.String,
		filepath.Join(productImageRoot(), tenantID, filepath.Base(imagePath.String)),
		maxProductImageSize,
		"private, max-age=3600",
	)
}

func productImageRoot() string {
	if root := strings.TrimSpace(os.Getenv("PRODUCT_IMAGE_DIR")); root != "" {
		return root
	}
	return filepath.Join("uploads", "product-images")
}

func validProductImagePath(tenantID, productID, relativePath string) bool {
	if tenantID == "" || filepath.Base(tenantID) != tenantID {
		return false
	}
	if filepath.ToSlash(relativePath) != tenantID+"/"+filepath.Base(relativePath) {
		return false
	}
	base := filepath.Base(relativePath)
	return strings.HasPrefix(base, productID+"-") &&
		(filepath.Ext(base) == ".jpg" || filepath.Ext(base) == ".png" || filepath.Ext(base) == ".webp")
}

func deleteProductImageObject(ctx context.Context, tenantID, productID, relativePath string) error {
	if !validProductImagePath(tenantID, productID, relativePath) {
		return errors.New("invalid product image path")
	}
	if err := deleteImageFromR2(ctx, "product-images/"+relativePath); err != nil {
		return fmt.Errorf("remove R2 product image %q: %w", relativePath, err)
	}
	return nil
}

func removeProductImage(ctx context.Context, tenantID, productID, relativePath string) error {
	if err := deleteProductImageObject(ctx, tenantID, productID, relativePath); err != nil {
		return err
	}
	removeLegacyImage(filepath.Join(productImageRoot(), tenantID, filepath.Base(relativePath)))
	return nil
}

func DeleteProduct(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin product deletion: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}
	defer tx.Rollback()

	var imagePath sql.NullString
	if err := tx.QueryRowContext(c.Request.Context(), `
		SELECT image_path FROM products WHERE id = ? AND is_deleted = 0
		FOR UPDATE
	`, c.Param("id")).Scan(&imagePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		} else {
			log.Printf("load product before deletion: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		}
		return
	}
	if imagePath.Valid {
		if err := deleteProductImageObject(c.Request.Context(), c.GetString("tenant_id"), c.Param("id"), imagePath.String); err != nil {
			log.Printf("delete image before deleting product %s: %v", c.Param("id"), err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove product image; product was not deleted"})
			return
		}
	}
	result, err := tx.ExecContext(c.Request.Context(), `
		UPDATE products SET is_deleted = 1, updated_at = ?
		WHERE id = ? AND is_deleted = 0
	`, time.Now().UTC(), c.Param("id"))
	if err != nil {
		log.Printf("delete tenant product: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		log.Printf("check deleted tenant product: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit product deletion: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}
	if imagePath.Valid {
		removeLegacyImage(filepath.Join(productImageRoot(), c.GetString("tenant_id"), filepath.Base(imagePath.String)))
	}
	c.Status(http.StatusNoContent)
}

func CreateStockMovement(c *gin.Context) {
	var request stockMovementRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid stock movement"})
		return
	}
	request.Type = strings.ToUpper(strings.TrimSpace(request.Type))
	request.Note = strings.TrimSpace(request.Note)
	if err := validateStockMovement(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	conn := c.MustGet("tenant_db").(*sql.DB)
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		log.Printf("begin tenant stock movement: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
		return
	}
	defer tx.Rollback()

	var stock int64
	var name string
	if err := tx.QueryRowContext(c.Request.Context(), `
		SELECT name, stock FROM products WHERE id = ? AND is_deleted = 0 FOR UPDATE
	`, c.Param("id")).Scan(&name, &stock); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		} else {
			log.Printf("lock tenant product stock: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
		}
		return
	}
	if request.Type == "OUT" && request.Quantity > stock {
		c.JSON(http.StatusConflict, gin.H{"error": "Insufficient stock"})
		return
	}

	now := time.Now().UTC()
	movementID := utils.NewULID()
	delta := request.Quantity
	if request.Type == "OUT" {
		delta = -delta
	}
	var updateResult sql.Result
	if request.MinStock == nil {
		updateResult, err = tx.ExecContext(c.Request.Context(), `
			UPDATE products SET stock = stock + ?, updated_at = ? WHERE id = ?
		`, delta, now, c.Param("id"))
	} else {
		updateResult, err = tx.ExecContext(c.Request.Context(), `
			UPDATE products SET stock = stock + ?, min_stock = ?, updated_at = ? WHERE id = ?
		`, delta, *request.MinStock, now, c.Param("id"))
	}
	if err != nil {
		log.Printf("update tenant product stock: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
		return
	}
	if _, err := updateResult.RowsAffected(); err != nil {
		log.Printf("check updated tenant product stock: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
		INSERT INTO stock_movements
			(id, product_id, type, qty, purchase_price, remaining_qty, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)
	`, movementID, c.Param("id"), request.Type, request.Quantity,
		request.PurchasePrice, request.Note, now, now); err != nil {
		log.Printf("insert tenant stock movement: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("commit tenant stock movement: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
		return
	}
	c.JSON(http.StatusCreated, stockMovementResponse{
		ID: movementID, ProductID: c.Param("id"), ProductName: name,
		Type: request.Type, Quantity: request.Quantity, Note: &request.Note, CreatedAt: now,
	})
}

func ListStockMovements(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	rows, err := conn.Query(`
		SELECT movement.id, movement.product_id, product.name, movement.type,
		       movement.qty, movement.note, movement.created_at
		FROM stock_movements AS movement
		LEFT JOIN products AS product ON product.id = movement.product_id
		ORDER BY movement.created_at DESC
	`)
	if err != nil {
		log.Printf("list tenant stock movements: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load stock history"})
		return
	}
	defer rows.Close()
	movements := make([]stockMovementResponse, 0)
	for rows.Next() {
		var movement stockMovementResponse
		var name, note sql.NullString
		if err := rows.Scan(
			&movement.ID, &movement.ProductID, &name, &movement.Type,
			&movement.Quantity, &note, &movement.CreatedAt,
		); err != nil {
			log.Printf("scan tenant stock movement: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load stock history"})
			return
		}
		if name.Valid {
			movement.ProductName = name.String
		}
		if note.Valid {
			movement.Note = &note.String
		}
		movements = append(movements, movement)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate tenant stock movements: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load stock history"})
		return
	}
	c.JSON(http.StatusOK, movements)
}

func validateProduct(request *productRequest, creating bool) error {
	request.Name = strings.TrimSpace(request.Name)
	request.Unit = strings.TrimSpace(request.Unit)
	request.Barcode = strings.TrimSpace(request.Barcode)
	request.ProductType = strings.TrimSpace(request.ProductType)
	request.ValuationMethod = strings.ToUpper(strings.TrimSpace(request.ValuationMethod))

	if request.Name == "" || utf8.RuneCountInString(request.Name) > 255 {
		return errors.New("name is required and must be at most 255 characters")
	}
	if request.Unit == "" {
		request.Unit = "item"
	}
	if utf8.RuneCountInString(request.Unit) > 100 {
		return errors.New("unit must be at most 100 characters")
	}
	if request.Price < 0 || request.CostPrice < 0 || request.Discount < 0 || request.Tax < 0 {
		return errors.New("price, cost, discount and tax cannot be negative")
	}
	if creating && (request.Stock < 0 || request.MinStock < 0) {
		return errors.New("stock values cannot be negative")
	}
	if request.ValuationMethod == "" {
		request.ValuationMethod = "AVG"
	}
	if request.ValuationMethod != "AVG" && request.ValuationMethod != "FIFO" {
		return errors.New("valuation_method must be AVG or FIFO")
	}
	if request.ProductType == "" {
		request.ProductType = "Finished"
	}
	if utf8.RuneCountInString(request.ProductType) > 32 || utf8.RuneCountInString(request.Barcode) > 255 {
		return errors.New("product type or barcode is too long")
	}
	request.Description = strings.TrimSpace(request.Description)
	if utf8.RuneCountInString(request.Description) > 5000 {
		return errors.New("description must be at most 5000 characters")
	}
	return nil
}

func validateStockMovement(request stockMovementRequest) error {
	if request.Type != "IN" && request.Type != "OUT" {
		return errors.New("type must be IN or OUT")
	}
	if request.Quantity <= 0 {
		return errors.New("quantity must be positive")
	}
	if request.Note == "" {
		return errors.New("note is required")
	}
	if request.PurchasePrice < 0 || (request.MinStock != nil && *request.MinStock < 0) {
		return errors.New("stock values cannot be negative")
	}
	return nil
}

type productScanner interface {
	Scan(dest ...any) error
}

func scanProduct(scanner productScanner) (productResponse, error) {
	var product productResponse
	var barcode sql.NullString
	var description sql.NullString
	var hasImage bool
	var deleted bool
	err := scanner.Scan(
		&product.ID, &product.Name, &product.Unit, &product.Price, &product.CostPrice,
		&product.ValuationMethod, &barcode, &description, &hasImage, &product.ProductType, &product.Stock,
		&product.MinStock, &product.Discount, &product.Tax, &deleted,
		&product.CreatedAt, &product.UpdatedAt,
	)
	if err != nil {
		return productResponse{}, err
	}
	product.IsDeleted = deleted
	product.HasImage = hasImage
	if description.Valid {
		product.Description = description.String
	}
	if barcode.Valid {
		product.Barcode = &barcode.String
	}
	return product, nil
}

func nullableProductBarcode(barcode string) *string {
	if barcode == "" {
		return nil
	}
	return &barcode
}

func writeProductError(c *gin.Context, operation string, err error) {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		c.JSON(http.StatusConflict, gin.H{"error": "Barcode is already assigned to another product"})
		return
	}
	log.Printf("%s: %v", operation, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Product operation failed"})
}

func parseBoundedInt(value string, minimum, maximum int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, errors.New("integer out of range")
	}
	return parsed, nil
}

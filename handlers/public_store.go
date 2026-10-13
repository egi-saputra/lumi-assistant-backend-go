package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"assistant/db"

	"github.com/gin-gonic/gin"
)

type publicStore struct {
	ID     string
	Name   string
	DBName string
	Code   string
}

type publicProduct struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit"`
	Price       int64  `json:"price"`
	ImageURL    string `json:"image_url,omitempty"`
}

func PublicStoreProducts(c *gin.Context) {
	store, err := findPublicStore(c.Param("slug"))
	if err != nil {
		if errors.Is(err, errPublicStoreNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Toko tidak ditemukan."})
		} else if errors.Is(err, errAmbiguousPublicStoreSlug) {
			c.JSON(http.StatusConflict, gin.H{"error": "Nama toko tidak unik. Link katalog memerlukan kode toko."})
		} else {
			log.Printf("find public store: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store"})
		}
		return
	}

	limit := 100
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

	tenantDB, err := db.GetTenantDB(store.DBName)
	if err != nil {
		log.Printf("connect public store database %s: %v", store.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
		return
	}
	rows, err := tenantDB.QueryContext(c.Request.Context(), `
		SELECT id, name, COALESCE(description, ''), COALESCE(unit, 'item'),
		       price, COALESCE(image_path, '')
		FROM products
		WHERE is_deleted = 0
		ORDER BY name ASC, id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		log.Printf("query public products for store %s: %v", store.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
		return
	}
	defer rows.Close()

	products := make([]publicProduct, 0)
	imagePaths := make([]string, 0)
	for rows.Next() {
		var product publicProduct
		var imagePath string
		if err := rows.Scan(&product.ID, &product.Name, &product.Description, &product.Unit, &product.Price, &imagePath); err != nil {
			log.Printf("scan public product for store %s: %v", store.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
			return
		}
		products = append(products, product)
		imagePaths = append(imagePaths, imagePath)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate public products for store %s: %v", store.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
		return
	}
	if err := rows.Close(); err != nil {
		log.Printf("close public products rows for store %s: %v", store.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load products"})
		return
	}
	for index := range products {
		products[index].ImageURL = publicProductImageURL(c, store, products[index].ID, imagePaths[index])
	}

	logoURL := ""
	var logoPath sql.NullString
	if err := tenantDB.QueryRowContext(c.Request.Context(), `
		SELECT file_path FROM profile ORDER BY created_at LIMIT 1
	`).Scan(&logoPath); err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("load public store logo path for store %s: %v", store.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store"})
		return
	}
	if logoPath.Valid && validStoreLogoPath(store.ID, logoPath.String) {
		objectKey := "store-logos/" + logoPath.String
		logoURL = ensurePublicR2ImageURL(
			c,
			objectKey,
			filepath.Join(storeLogoRoot(), store.ID, filepath.Base(logoPath.String)),
			maxStoreLogoSize,
		)
		if logoURL == "" {
			logoURL = fmt.Sprintf(
				"/public/stores/%s/logo?v=%s",
				url.PathEscape(c.Param("slug")),
				url.QueryEscape(filepath.Base(logoPath.String)),
			)
		}
	}

	c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	c.JSON(http.StatusOK, gin.H{
		"store":    gin.H{"name": store.Name, "slug": c.Param("slug"), "logo_url": logoURL},
		"products": products,
	})
}

func publicProductImageURL(c *gin.Context, store publicStore, productID, imagePath string) string {
	if imagePath == "" || !validProductImagePath(store.ID, productID, imagePath) {
		return ""
	}
	if imageURL := ensurePublicR2ImageURL(
		c,
		"product-images/"+imagePath,
		filepath.Join(productImageRoot(), store.ID, filepath.Base(imagePath)),
		maxProductImageSize,
	); imageURL != "" {
		return imageURL
	}
	return publicProductImageProxyURL(c.Param("slug"), productID)
}

func publicProductImageProxyURL(slug, productID string) string {
	return fmt.Sprintf(
		"/public/stores/%s/product-images/%s",
		url.PathEscape(slug),
		url.PathEscape(productID),
	)
}

func PublicStoreLogo(c *gin.Context) {
	store, err := findPublicStore(c.Param("slug"))
	if err != nil {
		if errors.Is(err, errPublicStoreNotFound) || errors.Is(err, errAmbiguousPublicStoreSlug) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Store logo not found"})
		} else {
			log.Printf("find public store logo: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store logo"})
		}
		return
	}

	tenantDB, err := db.GetTenantDB(store.DBName)
	if err != nil {
		log.Printf("connect public store logo database %s: %v", store.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store logo"})
		return
	}
	var logoPath sql.NullString
	if err := tenantDB.QueryRowContext(c.Request.Context(), `
		SELECT file_path FROM profile ORDER BY created_at LIMIT 1
	`).Scan(&logoPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Store logo not found"})
		} else {
			log.Printf("load public store logo path for store %s: %v", store.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store logo"})
		}
		return
	}
	if !logoPath.Valid || !validStoreLogoPath(store.ID, logoPath.String) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Store logo not found"})
		return
	}

	serveR2Image(
		c,
		"store-logos/"+logoPath.String,
		filepath.Join(storeLogoRoot(), store.ID, filepath.Base(logoPath.String)),
		maxStoreLogoSize,
		"public, max-age=300, stale-while-revalidate=3600",
	)
}

func PublicStoreProductImage(c *gin.Context) {
	store, err := findPublicStore(c.Param("slug"))
	if err != nil {
		if errors.Is(err, errPublicStoreNotFound) || errors.Is(err, errAmbiguousPublicStoreSlug) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product image not found"})
		} else {
			log.Printf("find public store product image: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product image"})
		}
		return
	}

	tenantDB, err := db.GetTenantDB(store.DBName)
	if err != nil {
		log.Printf("connect public product image database %s: %v", store.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product image"})
		return
	}
	var imagePath sql.NullString
	if err := tenantDB.QueryRowContext(c.Request.Context(), `
		SELECT image_path FROM products WHERE id = ? AND is_deleted = 0
	`, c.Param("productID")).Scan(&imagePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product image not found"})
		} else {
			log.Printf("load public product image path for store %s: %v", store.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load product image"})
		}
		return
	}
	if !imagePath.Valid || !validProductImagePath(store.ID, c.Param("productID"), imagePath.String) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product image not found"})
		return
	}

	serveR2Image(
		c,
		"product-images/"+imagePath.String,
		filepath.Join(productImageRoot(), store.ID, filepath.Base(imagePath.String)),
		maxProductImageSize,
		"public, max-age=300, stale-while-revalidate=3600",
	)
}

var (
	errPublicStoreNotFound      = errors.New("public store not found")
	errAmbiguousPublicStoreSlug = errors.New("ambiguous public store slug")
)

func findPublicStore(slug string) (publicStore, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return publicStore{}, errPublicStoreNotFound
	}
	if db.Master == nil {
		return publicStore{}, errors.New("master database is not connected")
	}
	rows, err := db.Master.Query(`
		SELECT id, name, db_name, code
		FROM tenants
		WHERE status = 'active'
	`)
	if err != nil {
		return publicStore{}, err
	}
	defer rows.Close()

	var found publicStore
	for rows.Next() {
		var candidate publicStore
		if err := rows.Scan(&candidate.ID, &candidate.Name, &candidate.DBName, &candidate.Code); err != nil {
			return publicStore{}, err
		}
		baseSlug := storeSlug(candidate.Name)
		codeSlug := baseSlug + "-" + strings.ToLower(candidate.Code)
		if slug != baseSlug && slug != codeSlug {
			continue
		}
		if found.ID != "" {
			return publicStore{}, errAmbiguousPublicStoreSlug
		}
		found = candidate
	}
	if err := rows.Err(); err != nil {
		return publicStore{}, err
	}
	if found.ID == "" {
		return publicStore{}, errPublicStoreNotFound
	}
	return found, nil
}

func storeSlug(value string) string {
	var slug strings.Builder
	previousSeparator := true
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			slug.WriteRune(character)
			previousSeparator = false
			continue
		}
		if !previousSeparator {
			slug.WriteByte('-')
			previousSeparator = true
		}
	}
	return strings.Trim(slug.String(), "-")
}

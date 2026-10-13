package handlers

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"assistant/utils"

	"github.com/gin-gonic/gin"
)

const maxStoreLogoSize = 5 << 20

func UploadBusinessLogo(c *gin.Context) {
	conn := c.MustGet("tenant_db").(*sql.DB)
	tenantID := c.GetString("tenant_id")
	if tenantID == "" || filepath.Base(tenantID) != tenantID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid store"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxStoreLogoSize+64*1024)
	file, _, err := c.Request.FormFile("image")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Store logos must be 5 MB or smaller"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "An image file is required"})
		return
	}
	defer file.Close()

	imageBytes, err := io.ReadAll(io.LimitReader(file, maxStoreLogoSize+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read store logo"})
		return
	}
	if len(imageBytes) == 0 || len(imageBytes) > maxStoreLogoSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Store logos must be 5 MB or smaller"})
		return
	}

	extensionByType := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	}
	contentType, supported := supportedImageContentType(imageBytes)
	if !supported {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Only JPEG, PNG, and WebP store logos are supported"})
		return
	}
	extension := extensionByType[contentType]

	filename := tenantID + "-" + utils.NewULID() + extension
	relativePath := filepath.ToSlash(filepath.Join(tenantID, filename))
	objectKey := "store-logos/" + relativePath
	if err := uploadImageToR2(c, objectKey, imageBytes, contentType); err != nil {
		log.Printf("upload store logo to R2 for tenant %s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store store logo"})
		return
	}

	profileID, err := ensureBusinessProfile(c, conn)
	if err != nil {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 store logo %q: %v", objectKey, deleteErr)
		}
		log.Printf("ensure business profile for logo upload: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save store logo"})
		return
	}
	tx, err := conn.BeginTx(c.Request.Context(), nil)
	if err != nil {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 store logo %q: %v", objectKey, deleteErr)
		}
		log.Printf("begin store logo update transaction: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save store logo"})
		return
	}
	defer tx.Rollback()

	var oldPath sql.NullString
	if err := tx.QueryRowContext(c.Request.Context(), `
		SELECT file_path FROM profile WHERE id = ? FOR UPDATE
	`, profileID).Scan(&oldPath); err != nil {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 store logo %q: %v", objectKey, deleteErr)
		}
		log.Printf("load old store logo path: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save store logo"})
		return
	}

	if _, err := tx.ExecContext(c.Request.Context(), `
		UPDATE profile SET file_path = ?, updated_at = ? WHERE id = ?
	`, relativePath, time.Now().UTC(), profileID); err != nil {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 store logo %q: %v", objectKey, deleteErr)
		}
		log.Printf("save store logo path: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save store logo"})
		return
	}
	if err := tx.Commit(); err != nil {
		if deleteErr := deleteImageFromR2(c.Request.Context(), objectKey); deleteErr != nil {
			log.Printf("remove unreferenced R2 store logo %q: %v", objectKey, deleteErr)
		}
		log.Printf("commit store logo update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save store logo"})
		return
	}
	cleanupWarning := ""
	if oldPath.Valid && oldPath.String != relativePath && validStoreLogoPath(tenantID, oldPath.String) {
		oldKey := "store-logos/" + oldPath.String
		if err := deleteImageFromR2(c.Request.Context(), oldKey); err != nil {
			log.Printf("remove replaced R2 store logo %q: %v", oldKey, err)
			cleanupWarning = "Logo updated, but the previous logo could not be removed from storage."
		} else {
			removeLegacyImage(filepath.Join(storeLogoRoot(), tenantID, filepath.Base(oldPath.String)))
		}
	}
	response := gin.H{"message": "Store logo updated"}
	if cleanupWarning != "" {
		response["cleanup_warning"] = cleanupWarning
	}
	c.JSON(http.StatusOK, response)
}

func storeLogoRoot() string {
	if root := strings.TrimSpace(os.Getenv("STORE_LOGO_DIR")); root != "" {
		return root
	}
	return filepath.Join("uploads", "store-logos")
}

func validStoreLogoPath(tenantID, relativePath string) bool {
	if tenantID == "" || filepath.Base(tenantID) != tenantID {
		return false
	}
	if filepath.ToSlash(relativePath) != tenantID+"/"+filepath.Base(relativePath) {
		return false
	}
	base := filepath.Base(relativePath)
	return strings.HasPrefix(base, tenantID+"-") &&
		(filepath.Ext(base) == ".jpg" || filepath.Ext(base) == ".png" || filepath.Ext(base) == ".webp")
}

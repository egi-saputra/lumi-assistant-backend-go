package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

	"assistant/storage"

	"github.com/gin-gonic/gin"
)

func uploadImageToR2(c *gin.Context, key string, image []byte, contentType string) error {
	r2, err := storage.Default()
	if err != nil {
		return err
	}
	return r2.Put(c.Request.Context(), key, bytes.NewReader(image), contentType)
}

func deleteImageFromR2(ctx context.Context, key string) error {
	r2, err := storage.Default()
	if err != nil {
		return err
	}
	return r2.DeleteIfExists(ctx, key)
}

func ensurePublicR2ImageURL(c *gin.Context, key, legacyPath string, maxSize int64) string {
	r2, err := storage.Default()
	if err != nil {
		return ""
	}
	publicURL := r2.PublicURL(key)
	if publicURL == "" {
		return ""
	}

	err = r2.Exists(c.Request.Context(), key)
	if err == nil {
		return publicURL
	}
	if !storage.IsNotFound(err) {
		log.Printf("check public R2 image %q: %v", key, err)
		return ""
	}

	file, err := os.Open(legacyPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("open legacy public image %q: %v", key, err)
		}
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		log.Printf("stat legacy public image %q: %v", key, err)
		return ""
	}
	if info.Size() > maxSize {
		log.Printf("legacy public image %q exceeds size limit", key)
		return ""
	}
	image, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		log.Printf("read legacy public image %q: %v", key, err)
		return ""
	}
	if int64(len(image)) > maxSize {
		log.Printf("legacy public image %q exceeds size limit", key)
		return ""
	}
	contentType, supported := supportedImageContentType(image)
	if !supported {
		log.Printf("legacy public image %q has unsupported content type", key)
		return ""
	}
	if err := r2.Put(c.Request.Context(), key, bytes.NewReader(image), contentType); err != nil {
		log.Printf("migrate legacy public image %q to R2: %v", key, err)
		return ""
	}
	removeLegacyImage(legacyPath)
	return publicURL
}

func serveR2Image(c *gin.Context, key, legacyPath string, maxSize int64, cacheControl string) {
	r2, err := storage.Default()
	if err != nil {
		serveLegacyImage(c, legacyPath, maxSize, cacheControl)
		return
	}

	body, contentType, err := r2.Get(c.Request.Context(), key)
	if err == nil {
		defer body.Close()
		image, readErr := io.ReadAll(io.LimitReader(body, maxSize+1))
		if readErr != nil {
			log.Printf("read R2 image %q: %v", key, readErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
			return
		}
		if int64(len(image)) > maxSize {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Stored image exceeds size limit"})
			return
		}
		detectedType, supported := supportedImageContentType(image)
		if !supported {
			log.Printf("R2 image %q has unsupported content", key)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Stored image has unsupported content"})
			return
		}
		if contentType != detectedType {
			log.Printf("R2 image %q content type metadata %q differs from detected %q", key, contentType, detectedType)
		}
		serveImageBytes(c, image, detectedType, cacheControl)
		return
	}
	if !storage.IsNotFound(err) {
		log.Printf("load R2 image %q: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
		return
	}

	file, err := os.Open(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
		} else {
			log.Printf("open legacy image %q: %v", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
		}
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		log.Printf("stat legacy image %q: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
		return
	}
	if info.Size() > maxSize {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Stored image exceeds size limit"})
		return
	}
	image, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		log.Printf("read legacy image %q: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
		return
	}

	contentType, supported := supportedImageContentType(image)
	if !supported {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Stored image has unsupported content"})
		return
	}
	if migrationErr := r2.Put(c.Request.Context(), key, bytes.NewReader(image), contentType); migrationErr != nil {
		log.Printf("migrate legacy image %q to R2: %v", key, migrationErr)
	} else if removeErr := os.Remove(legacyPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		log.Printf("remove migrated legacy image %q: %v", key, removeErr)
	}
	serveImageBytes(c, image, contentType, cacheControl)
}

func serveLegacyImage(c *gin.Context, legacyPath string, maxSize int64, cacheControl string) {
	file, err := os.Open(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
		} else {
			log.Printf("open legacy image %q: %v", legacyPath, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
		}
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		log.Printf("stat legacy image %q: %v", legacyPath, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
		return
	}
	if info.Size() > maxSize {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Stored image exceeds size limit"})
		return
	}
	image, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		log.Printf("read legacy image %q: %v", legacyPath, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load image"})
		return
	}
	contentType, supported := supportedImageContentType(image)
	if !supported {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Stored image has unsupported content"})
		return
	}
	serveImageBytes(c, image, contentType, cacheControl)
}

func serveImageBytes(c *gin.Context, image []byte, contentType, cacheControl string) {
	if contentType == "" || !isImageContentType(contentType) {
		contentType = http.DetectContentType(image)
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", cacheControl)
	c.Data(http.StatusOK, contentType, image)
}

func isImageContentType(contentType string) bool {
	return contentType == "image/jpeg" || contentType == "image/png" || contentType == "image/webp"
}

func supportedImageContentType(image []byte) (string, bool) {
	contentType := http.DetectContentType(image)
	if isImageContentType(contentType) {
		return contentType, true
	}
	if len(image) >= 12 &&
		string(image[:4]) == "RIFF" &&
		string(image[8:12]) == "WEBP" {
		return "image/webp", true
	}
	return "", false
}

func removeLegacyImage(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("remove legacy image %q: %v", path, err)
	}
}

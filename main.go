package main

import (
	"errors"
	"log"
	"os"

	"github.com/joho/godotenv"
	"assistant/db"
	"assistant/routes"
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("load .env configuration: %v", err)
	}

	if err := db.ConnectMaster(); err != nil {
		log.Fatal(err)
	}

	r := routes.Setup()

	if err := r.Run("0.0.0.0:8080"); err != nil {
		log.Fatal(err)
	}
}

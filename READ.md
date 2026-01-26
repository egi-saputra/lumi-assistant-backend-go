Started Setup Backend API with Golang untuk React Native (Expo)

1️⃣ Persiapan Lingkungan (Installasi)
a. Install Golang
Download Golang: https://go.dev/dl/

Cek versi:
go version

Minimal Golang 1.20+ untuk fitur modern.

b. Install MySQL
Bisa pakai MySQL lokal (XAMPP / MySQL Community)

Buat database baru misal cbt_db

CREATE DATABASE cbt_db;

Buat tabel users:

```sh
CREATE TABLE users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    phone VARCHAR(20) UNIQUE NULL,
    password VARCHAR(255) NOT NULL,
    role ENUM('admin','siswa') NOT NULL DEFAULT 'siswa'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

Optional: insert user contoh:

```sh
INSERT INTO users (name, email, phone, password, role) VALUES
('admin', 'admin@example.com', '0896099912121', '$2a$10$CwTycUXWue0Thq9StjUM0uJ8u3m3aWq1/dhblOa/p8jC.5dO/hzFe', 'admin');

INSERT INTO users (name, email, phone, password, role) VALUES
('siswa', 'siswa@example.com', '089609916121', '2a$10$FtFhtto7k4yaFDupIxBmKeuy9x2TbPG8K4njjVIQGJ5kM7M3PGthu', 'siswa');
```

password untuk admin: 123456 sudah di-hash bcrypt,
password untuk siswa: password sudah di-hash bcrypt.

2️⃣ Buat Folder Project Golang
mkdir cbt-api
cd cbt-api
go mod init cbt-api

Ini membuat file go.mod untuk dependency Golang.

3️⃣ Install Dependencies Golang

# MySQL driver

go get github.com/go-sql-driver/mysql

# Bcrypt untuk hashing password

go get golang.org/x/crypto/bcrypt

# Cors Handlers

go get github.com/gin-contrib/cors

# JWT Token Auth

go get github.com/golang-jwt/jwt/v5

# Optional: framework ringan untuk API seperti Gin (lebih rapi daripada net/http)

go get github.com/gin-gonic/gin

Kita pakai Gin untuk lebih mudah handle routing dan JSON response.

4️⃣ Struktur Project
cbt-api/
├─ main.go
├─ go.mod
├─ db/
│ └─ db.go
├─ models/
│ └─ user.go
├─ handlers/
│ └─ auth.go
└─ utils/
└─ hash.go

5️⃣ Buat Koneksi Database: db/db.go

```sh
package db

import (
    "database/sql"
    "log"

    _ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func Connect() {
    var err error
    DB, err = sql.Open("mysql", "root:password_db_kamu@tcp(127.0.0.1:3306)/cbt_db")
    if err != nil {
        log.Fatal(err)
    }

    if err = DB.Ping(); err != nil {
        log.Fatal(err)
    }

    log.Println("Database connected!")
}
```

6️⃣ Buat Model User: models/user.go

```sh
package models

type User struct {
    ID       int    `json:"id"`
    Username string `json:"username"`
    Email string `json:"email"`
    Phone string `json:"phone"`
    Password string `json:"-"`
    Role     string `json:"role"`
}
```

7️⃣ Buat Utils untuk Hash Password: utils/hash.go

```sh
package utils

import "golang.org/x/crypto/bcrypt"

func HashPassword(password string) (string, error) {
    bytes, err := bcrypt.GenerateFromPassword([]byte(password), 10)
    return string(bytes), err
}

func CheckPassword(password, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
    return err == nil
}
```

8️⃣ Buat Handler Login: handlers/auth.go

```sh
package handlers

import (
    "cbt-api/db"
    "cbt-api/models"
    "cbt-api/utils"
    "net/http"

    "github.com/gin-gonic/gin"
)

func Login(c *gin.Context) {
    var req struct {
        Email string `json:"email"`
        Password string `json:"password"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
        return
    }

    var user models.User
    err := db.DB.QueryRow("SELECT id, username, password, role FROM users WHERE username=?", req.Username).
        Scan(&user.ID, &user.Username, &user.Password, &user.Role)
    if err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
        return
    }

    if !utils.CheckPassword(req.Password, user.Password) {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "Wrong password"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "message": "Login successful",
        "role":    user.Role,
    })
}
```

9️⃣ Buat main.go

```sh
package main

import (
    "cbt-api/db"
    "cbt-api/handlers"

    "github.com/gin-gonic/gin"
)

func main() {
    db.Connect()

    router := gin.Default()

    router.POST("/login", handlers.Login)

    router.Run(":8080") // server running di port 8080
}
```

🔟 Run API
go run main.go

Test Endpoint login melalui curl:

```sh
curl -X POST http://localhost:8080/login \
-H "Content-Type: application/json" \
-d '{"email":"siswa@example.com","password":"password"}'
```

# Jika sukses akan muncul seperti ini :

```sh
{"message":"Login successful","role":"siswa"}
```

=================================================================================

## Cara generate hash bcrypt di Go

Kamu bisa buat file sementara generate_hash.go pada root project:

```sh
package main

import (
    "fmt"
    "golang.org/x/crypto/bcrypt"
)

func main() {
    password := "password"
    hash, _ := bcrypt.GenerateFromPassword([]byte(password), 10)
    fmt.Println(string(hash))
}
```

Kemudian Jalankan:

```sh
go run generate_hash.go
```

Copy output → masukkan ke kolom password di MySQL.

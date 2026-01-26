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

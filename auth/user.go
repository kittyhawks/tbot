package auth

import (
	"golang.org/x/crypto/bcrypt"

	"crypto/rand"
	"encoding/base64"
	"time"
)

const (
	StandardUser = "standard"
	StaffUser    = "staff"
	AdminUser    = "admin"
)

// User represents information for a registered user.
type User struct {
	PasswordHash   []byte    `json:"password_hash"`
	ChangePassword bool      `json:"change_password"`
	Type           string    `json:"type"`
	Created        time.Time `json:"created"`
}

// Authenticate will check the specified password against its stored hash.
func (u *User) authenticate(password string) bool {
	if bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(password)) == nil {
		return true
	}
	return false
}

// resetPassword generates a password for the user and forces it to be changed
// immediately after login.
func (u *User) resetPassword() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	if err := u.setPassword(base64.StdEncoding.EncodeToString(b)); err != nil {
		return "", err
	}
	u.ChangePassword = true
	return string(b), nil
}

// setPassword changes the password set on the account.
func (u *User) setPassword(password string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), 0)
	if err != nil {
		return err
	}
	u.PasswordHash = h
	return nil
}
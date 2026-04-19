package domain

import (
	"net/mail"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)


type User struct {
	ID           uuid.UUID
	FirstName    string
	LastName     string
	Email        string
	PasswordHash string
	IsDeleted    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func NewUser(firstName, lastName, email, plainTextPassword string) (User, error) {
	if _, err := mail.ParseAddress(email); err != nil {
		return User{}, ErrInvalidEmail
	}

	if plainTextPassword == "" {
		return User{}, ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plainTextPassword), 12)
	if err != nil {
		return User{}, err
	}

	now := time.Now().UTC()
	return User{
		ID:           uuid.New(),
		FirstName:    firstName,
		LastName:     lastName,
		Email:        email,
		PasswordHash: string(hash),
		IsDeleted:    false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (user User) CheckPassword(plainText string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(plainText))
	return err == nil
}

package domain_test

import (
	"testing"
	"ms-users/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

var (
	email 			= "test@example.com"
	firstName 		= "First"
	lastName 		= "Last"
	password 		= "securepassword123"
)

func TestNewUser(test *testing.T) {
	test.Run("valid input creates user and hashes password", func(test *testing.T) {
		user, err := domain.NewUser(firstName, lastName, email, password)

		if err != nil {
			test.Fatalf("expected no error, got %v", err)
		}

		if user.FirstName != firstName {
			test.Errorf("expected first name %q, got %q", firstName, user.FirstName)
		}

		if user.LastName != lastName {
			test.Errorf("expected last name %q, got %q", lastName, user.LastName)
		}

		if user.IsDeleted {
			test.Error("expected IsDeleted to be false")
		}

		if user.Email != email {
			test.Errorf("expected email %q, got %q", email, user.Email)
		}

		if user.PasswordHash == password {
			test.Error("expected password to be hashed, but it is plaintext")
		}

		err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
		if err != nil {
			test.Errorf("password hash is invalid: %v", err)
		}

		cost, err := bcrypt.Cost([]byte(user.PasswordHash))
		if err != nil {
			test.Fatalf("could not get bcrypt cost: %v", err)
		}
		if cost != 12 {
			test.Errorf("expected bcrypt cost 12, got %d", cost)
		}

		if user.ID.String() == "00000000-0000-0000-0000-000000000000" {
			test.Error("expected valid UUID, got empty UUID")
		}
		
		if user.CreatedAt.IsZero() {
			test.Error("expected CreatedAt to be set")
		}
		
		if user.UpdatedAt.IsZero() {
			test.Error("expected UpdatedAt to be set")
		}
	})

	test.Run("invalid email returns error", func(test *testing.T) {
		invalidEmail := "invalid-email"
		password := "securepassword123"

		_, err := domain.NewUser(firstName, lastName, invalidEmail, password)
		if err == nil {
			test.Error("expected error for invalid email, got nil")
		}
	})

	test.Run("empty password returns error", func(test *testing.T) {
		emptyPassword := ""

		_, err := domain.NewUser(firstName, lastName, email, emptyPassword)
		if err == nil {
			test.Error("expected error for empty password, got nil")
		}
	})
}

func TestCheckPassword(test *testing.T) {
	test.Run("correct password returns true", func(test *testing.T) {
		password := "mypassword"
		user, err := domain.NewUser(firstName, lastName, email, password)
		if err != nil {
			test.Fatalf("could not create user: %v", err)
		}

		if !user.CheckPassword(password) {
			test.Error("expected CheckPassword to return true for correct password")
		}
	})

	test.Run("incorrect password returns false", func(test *testing.T) {
		user, err := domain.NewUser(firstName, lastName, email, password)
		if err != nil {
			test.Fatalf("could not create user: %v", err)
		}

		if user.CheckPassword("wrongpassword") {
			test.Error("expected CheckPassword to return false for incorrect password")
		}
	})
}

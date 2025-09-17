package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestJWTValidation(t *testing.T) {
	secret := "supersecret"
	userID := uuid.New()

	tests := []struct {
		name        string
		makeToken   func() (string, error)
		secretToUse string
		expectError bool
	}{
		{
			name: "valid token",
			makeToken: func() (string, error) {
				return MakeJWT(userID, secret, time.Minute)
			},
			secretToUse: secret,
			expectError: false,
		},
		{
			name: "expired token",
			makeToken: func() (string, error) {
				return MakeJWT(userID, secret, -time.Minute)
			},
			secretToUse: secret,
			expectError: true,
		},
		{
			name: "wrong secret",
			makeToken: func() (string, error) {
				return MakeJWT(userID, secret, time.Minute)
			},
			secretToUse: "wrongsecret",
			expectError: true,
		},
		{
			name: "wrong signing method",
			makeToken: func() (string, error) {
				claims := jwt.RegisteredClaims{
					Issuer:    "chirpy",
					IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
					ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Minute)),
					Subject:   userID.String(),
				}
				// Deliberately use an unexpected algorithm (none)
				token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
				return token.SignedString(jwt.UnsafeAllowNoneSignatureType)
			},
			secretToUse: secret,
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, err := tc.makeToken()
			if err != nil {
				t.Fatalf("failed to create token: %v", err)
			}

			parsedID, err := ValidateJWT(token, tc.secretToUse)
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got none (parsedID=%v)", parsedID)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if parsedID != userID {
					t.Errorf("expected userID %v, got %v", userID, parsedID)
				}
			}
		})
	}
}

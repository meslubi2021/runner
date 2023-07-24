package auth

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt"
)

type Verifier struct {
	VerifierKey *rsa.PublicKey
}

func NewVerifier(verifierKey *rsa.PublicKey) *Verifier {
	if verifierKey == nil {
		panic("failed to initialize verifier: public key is nil")
	}
	return &Verifier{
		VerifierKey: verifierKey,
	}
}

func (v *Verifier) Verify(tokenString string) (jwt.MapClaims, error) {

	// Parse the token string
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Check the signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Return the public key
		return v.VerifierKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %v", err)
	}

	// Verify the token
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Verify the expiration time
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to get claims")
	}

	exp := time.Unix(int64(claims["exp"].(float64)), 0)
	if time.Now().After(exp) {
		return nil, fmt.Errorf("token has expired")
	}

	// Token is valid
	return claims, nil
}

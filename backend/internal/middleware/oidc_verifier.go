package middleware

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

type JWTVerifier struct {
	IssuerURL string
}

func NewJWTVerifier(issuerURL string) *JWTVerifier {
	return &JWTVerifier{IssuerURL: issuerURL}
}

func (v *JWTVerifier) Verify(ctx context.Context, rawToken string) (string, error) {
	parser := jwt.NewParser()
	claims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(rawToken, claims)
	if err != nil {
		return "", fmt.Errorf("failed to parse jwt: %w", err)
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", errors.New("sub claim missing or empty")
	}

	return sub, nil
}

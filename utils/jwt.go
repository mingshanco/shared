package utils

import (
	"github.com/dgrijalva/jwt-go"
)

func JwtToken(secretKey string, slat string, uid int64) (string, error) {
	claims := make(jwt.MapClaims)
	claims["uid"] = uid
	claims["salt"] = slat
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secretKey))
}

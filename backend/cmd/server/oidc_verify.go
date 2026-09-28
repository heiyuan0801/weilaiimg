package main

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type oidcJWTHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}
type oidcJWTClaims struct {
	Iss   string `json:"iss"`
	Aud   any    `json:"aud"`
	Azp   string `json:"azp"`
	Exp   int64  `json:"exp"`
	Iat   int64  `json:"iat"`
	Nonce string `json:"nonce"`
}
type oidcJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (s *server) validateOIDCIDToken(ctx context.Context, discovery oidcDiscovery, p oidcProvider, raw, nonce string) error {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return errors.New("malformed ID token")
	}
	var header oidcJWTHeader
	var claims oidcJWTClaims
	if err := decodeB64JSON(parts[0], &header); err != nil || header.Kid == "" {
		return errors.New("invalid ID token header")
	}
	if err := decodeB64JSON(parts[1], &claims); err != nil {
		return errors.New("invalid ID token claims")
	}
	issuer := strings.TrimRight(discovery.Issuer, "/")
	if issuer == "" {
		issuer = strings.TrimRight(p.IssuerURL, "/")
	}
	if claims.Iss != issuer || claims.Exp <= time.Now().Unix() || claims.Iat > time.Now().Add(2*time.Minute).Unix() || claims.Nonce != nonce {
		return errors.New("ID token issuer, lifetime, or nonce is invalid")
	}
	if !audienceContains(claims.Aud, p.ClientID) || (claims.Azp != "" && claims.Azp != p.ClientID) {
		return errors.New("ID token audience is invalid")
	}
	if discovery.JWKSURI == "" {
		return errors.New("OIDC provider has no JWKS endpoint")
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, discovery.JWKSURI, nil)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("JWKS returned %s", response.Status)
	}
	var set struct {
		Keys []oidcJWK `json:"keys"`
	}
	if json.NewDecoder(response.Body).Decode(&set) != nil {
		return errors.New("invalid JWKS")
	}
	for _, key := range set.Keys {
		if key.Kid == header.Kid {
			return verifyOIDCSignature(header.Alg, key, parts[0]+"."+parts[1], parts[2])
		}
	}
	return errors.New("ID token signing key not found")
}

func decodeB64JSON(value string, out any) error {
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func audienceContains(value any, expected string) bool {
	switch v := value.(type) {
	case string:
		return v == expected
	case []any:
		for _, item := range v {
			if item == expected {
				return true
			}
		}
	}
	return false
}

func verifyOIDCSignature(alg string, key oidcJWK, signed, encoded string) error {
	sig, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return errors.New("invalid ID token signature")
	}
	digest := sha256.Sum256([]byte(signed))
	switch alg {
	case "RS256":
		n, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			return err
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 | int(b)
		}
		if e == 0 {
			return errors.New("invalid RSA exponent")
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
			return errors.New("ID token signature verification failed")
		}
		return nil
	case "ES256":
		if len(sig) != 64 {
			return errors.New("invalid ECDSA signature")
		}
		xb, e := base64.RawURLEncoding.DecodeString(key.X)
		if e != nil {
			return e
		}
		yb, e := base64.RawURLEncoding.DecodeString(key.Y)
		if e != nil {
			return e
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xb), Y: new(big.Int).SetBytes(yb)}
		if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			return errors.New("ID token signature verification failed")
		}
		return nil
	default:
		return fmt.Errorf("unsupported ID token algorithm %s", alg)
	}
}

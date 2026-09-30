package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OIDCClaims defines standard and Kestrel-specific OpenID Connect claims.
type OIDCClaims struct {
	Issuer     string `json:"iss"`
	Subject    string `json:"sub"`
	Audience   string `json:"aud"`
	ExpiresAt  int64  `json:"exp"`
	NotBefore  int64  `json:"nbf"`
	IssuedAt   int64  `json:"iat"`
	JTI        string `json:"jti"`
	Repository string `json:"repository,omitempty"`
	Ref        string `json:"ref,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	JobID      string `json:"job_id,omitempty"`
	Actor      string `json:"actor,omitempty"`
	Tenant     string `json:"tenant,omitempty"`
}

// OIDCTokenRequest represents incoming request parameters to issue an OIDC JWT.
type OIDCTokenRequest struct {
	RunID      string `json:"run_id"`
	JobID      string `json:"job_id"`
	Audience   string `json:"audience"`
	Repository string `json:"repository,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Tenant     string `json:"tenant,omitempty"`
	Actor      string `json:"actor,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// OIDCIssuer creates and verifies cryptographically signed OIDC tokens.
type OIDCIssuer struct {
	issuerURL  string
	signingKey []byte
	defaultTTL time.Duration
}

// NewOIDCIssuer creates a new OIDC issuer for zero-trust cloud token exchange.
func NewOIDCIssuer(issuerURL string, secretKey []byte) *OIDCIssuer {
	if issuerURL == "" {
		issuerURL = "https://kestrel.ci"
	}
	if len(secretKey) == 0 {
		secretKey = []byte("kestrel-default-oidc-signing-secret-key-32b")
	}
	return &OIDCIssuer{
		issuerURL:  strings.TrimRight(issuerURL, "/"),
		signingKey: secretKey,
		defaultTTL: 15 * time.Minute,
	}
}

// IssuerURL returns the configured issuer URL.
func (oi *OIDCIssuer) IssuerURL() string {
	return oi.issuerURL
}

// IssueToken mints a signed OIDC JWT token for a specific CI task or workload.
func (oi *OIDCIssuer) IssueToken(req OIDCTokenRequest) (string, error) {
	now := time.Now()
	ttl := oi.defaultTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}

	repo := req.Repository
	if repo == "" {
		repo = "kestrel-ci/default-repo"
	}

	ref := req.Ref
	if ref == "" {
		ref = "refs/heads/main"
	}

	sub := fmt.Sprintf("repo:%s:ref:%s:job:%s", repo, ref, req.JobID)

	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate JTI: %w", err)
	}
	jti := hex.EncodeToString(randomBytes)

	claims := OIDCClaims{
		Issuer:     oi.issuerURL,
		Subject:    sub,
		Audience:   req.Audience,
		ExpiresAt:  now.Add(ttl).Unix(),
		NotBefore:  now.Add(-10 * time.Second).Unix(), // 10s clock skew allowance
		IssuedAt:   now.Unix(),
		JTI:        jti,
		Repository: repo,
		Ref:        ref,
		RunID:      req.RunID,
		JobID:      req.JobID,
		Actor:      req.Actor,
		Tenant:     req.Tenant,
	}

	return oi.SignToken(claims)
}

// SignToken serializes and signs given claims into a compact JWT string.
func (oi *OIDCIssuer) SignToken(claims OIDCClaims) (string, error) {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := encodedHeader + "." + encodedPayload

	h := hmac.New(sha256.New, oi.signingKey)
	h.Write([]byte(signingInput))
	signature := h.Sum(nil)
	encodedSignature := base64.RawURLEncoding.EncodeToString(signature)

	return signingInput + "." + encodedSignature, nil
}

// VerifyToken decodes, verifies the cryptographic signature, and validates claims.
func (oi *OIDCIssuer) VerifyToken(tokenString, expectedAudience string) (*OIDCClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid jwt format: token must have 3 dot-separated segments")
	}

	signingInput := parts[0] + "." + parts[1]
	providedSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}

	h := hmac.New(sha256.New, oi.signingKey)
	h.Write([]byte(signingInput))
	expectedSig := h.Sum(nil)

	if !hmac.Equal(providedSig, expectedSig) {
		return nil, errors.New("signature verification failed: cryptographic signature mismatch")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}

	var claims OIDCClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claims: %w", err)
	}

	now := time.Now().Unix()
	if claims.ExpiresAt > 0 && now >= claims.ExpiresAt {
		return nil, fmt.Errorf("token expired at %d, current time is %d", claims.ExpiresAt, now)
	}
	if claims.NotBefore > 0 && now < claims.NotBefore {
		return nil, fmt.Errorf("token not valid before %d, current time is %d", claims.NotBefore, now)
	}
	if claims.Issuer != oi.issuerURL {
		return nil, fmt.Errorf("issuer mismatch: expected '%s', got '%s'", oi.issuerURL, claims.Issuer)
	}
	if expectedAudience != "" && claims.Audience != expectedAudience {
		return nil, fmt.Errorf("audience mismatch: expected '%s', got '%s'", expectedAudience, claims.Audience)
	}

	return &claims, nil
}

// DiscoveryConfiguration returns the standard OpenID Connect discovery metadata.
func (oi *OIDCIssuer) DiscoveryConfiguration() map[string]interface{} {
	return map[string]interface{}{
		"issuer":                                oi.issuerURL,
		"token_endpoint":                        oi.issuerURL + "/api/v1/oidc/token",
		"jwks_uri":                              oi.issuerURL + "/api/v1/oidc/jwks",
		"response_types_supported":              []string{"id_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"HS256"},
		"claims_supported": []string{
			"iss", "sub", "aud", "exp", "nbf", "iat", "jti",
			"repository", "ref", "run_id", "job_id", "actor", "tenant",
		},
	}
}

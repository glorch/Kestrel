package security

import (
	"strings"
	"testing"
	"time"
)

func TestOIDCIssuer_IssueAndVerify(t *testing.T) {
	secret := []byte("test-oidc-super-secret-key-123456")
	issuer := NewOIDCIssuer("https://kestrel.example.com", secret)

	req := OIDCTokenRequest{
		RunID:      "run-9988",
		JobID:      "deploy-prod",
		Audience:   "sts.amazonaws.com",
		Repository: "myorg/backend-service",
		Ref:        "refs/heads/release/v1.0",
		Tenant:     "payment-gateway",
		Actor:      "alice",
		TTLSeconds: 300,
	}

	rawToken, err := issuer.IssueToken(req)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts in compact JWT, got %d", len(parts))
	}

	// Verify valid token with matching audience
	claims, err := issuer.VerifyToken(rawToken, "sts.amazonaws.com")
	if err != nil {
		t.Fatalf("failed to verify valid token: %v", err)
	}

	if claims.Issuer != "https://kestrel.example.com" {
		t.Errorf("expected issuer https://kestrel.example.com, got %s", claims.Issuer)
	}
	expectedSub := "repo:myorg/backend-service:ref:refs/heads/release/v1.0:job:deploy-prod"
	if claims.Subject != expectedSub {
		t.Errorf("expected sub %s, got %s", expectedSub, claims.Subject)
	}
	if claims.Tenant != "payment-gateway" {
		t.Errorf("expected tenant payment-gateway, got %s", claims.Tenant)
	}
	if claims.Actor != "alice" {
		t.Errorf("expected actor alice, got %s", claims.Actor)
	}
	if claims.RunID != "run-9988" {
		t.Errorf("expected runID run-9988, got %s", claims.RunID)
	}

	// Verify with wrong audience should fail
	_, err = issuer.VerifyToken(rawToken, "https://vault.hashicorp.com")
	if err == nil || !strings.Contains(err.Error(), "audience mismatch") {
		t.Fatalf("expected audience mismatch error, got: %v", err)
	}

	// Tampered token payload should fail signature check
	tamperedToken := parts[0] + "." + parts[1] + "tampered" + "." + parts[2]
	_, err = issuer.VerifyToken(tamperedToken, "sts.amazonaws.com")
	if err == nil {
		t.Fatalf("expected signature verification failure for tampered token")
	}

	// Different key should fail
	otherIssuer := NewOIDCIssuer("https://kestrel.example.com", []byte("completely-different-signing-key"))
	_, err = otherIssuer.VerifyToken(rawToken, "sts.amazonaws.com")
	if err == nil {
		t.Fatalf("expected verification failure with different signing key")
	}
}

func TestOIDCIssuer_ExpiredToken(t *testing.T) {
	secret := []byte("test-oidc-super-secret-key-123456")
	issuer := NewOIDCIssuer("https://kestrel.example.com", secret)

	claims := OIDCClaims{
		Issuer:    "https://kestrel.example.com",
		Subject:   "repo:demo:job:test",
		Audience:  "aud",
		ExpiresAt: time.Now().Add(-10 * time.Minute).Unix(), // already expired
		IssuedAt:  time.Now().Add(-20 * time.Minute).Unix(),
	}

	token, err := issuer.SignToken(claims)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = issuer.VerifyToken(token, "aud")
	if err == nil || !strings.Contains(err.Error(), "token expired") {
		t.Fatalf("expected token expired error, got: %v", err)
	}
}

func TestOIDCIssuer_DiscoveryMetadata(t *testing.T) {
	issuer := NewOIDCIssuer("https://kestrel.ci/", []byte("key"))
	cfg := issuer.DiscoveryConfiguration()

	if cfg["issuer"] != "https://kestrel.ci" {
		t.Errorf("expected issuer https://kestrel.ci, got %v", cfg["issuer"])
	}
	if cfg["token_endpoint"] != "https://kestrel.ci/api/v1/oidc/token" {
		t.Errorf("expected token endpoint, got %v", cfg["token_endpoint"])
	}
}

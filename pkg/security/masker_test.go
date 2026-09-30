package security

import (
	"bytes"
	"testing"
)

func TestMaskerBasic(t *testing.T) {
	masker := NewMasker()
	masker.Register("my-secret-token", "db_password_12345")

	input := "Connecting with password=db_password_12345 and token my-secret-token to server"
	expected := "Connecting with password=*** and token *** to server"

	actual := masker.Mask(input)
	if actual != expected {
		t.Errorf("expected: %q, got: %q", expected, actual)
	}
}

func TestMaskerOverlap(t *testing.T) {
	masker := NewMasker()
	// Longer secret should be replaced first without messing up shorter substring
	masker.Register("secret", "super-secret-key")

	input := "value is super-secret-key and secret"
	expected := "value is *** and ***"

	actual := masker.Mask(input)
	if actual != expected {
		t.Errorf("expected: %q, got: %q", expected, actual)
	}
}

func TestMaskingWriter(t *testing.T) {
	masker := NewMasker()
	masker.Register("ghp_ABC123XYZ")

	var buf bytes.Buffer
	writer := NewMaskingWriter(&buf, masker)

	_, err := writer.Write([]byte("Authorization: Bearer ghp_ABC123XYZ\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Authorization: Bearer ***\n"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestSanitizeMap(t *testing.T) {
	env := map[string]string{
		"NORMAL_VAR":     "hello",
		"API_TOKEN":      "sensitive_value_1",
		"DB_PASSWORD":    "sensitive_value_2",
		"MY_PRIVATE_KEY": "sensitive_value_3",
	}

	sanitized := SanitizeMap(env)
	if sanitized["NORMAL_VAR"] != "hello" {
		t.Errorf("expected normal var preserved, got %s", sanitized["NORMAL_VAR"])
	}
	if sanitized["API_TOKEN"] != MaskReplacement {
		t.Errorf("expected API_TOKEN to be masked, got %s", sanitized["API_TOKEN"])
	}
	if sanitized["DB_PASSWORD"] != MaskReplacement {
		t.Errorf("expected DB_PASSWORD to be masked, got %s", sanitized["DB_PASSWORD"])
	}
	if sanitized["MY_PRIVATE_KEY"] != MaskReplacement {
		t.Errorf("expected MY_PRIVATE_KEY to be masked, got %s", sanitized["MY_PRIVATE_KEY"])
	}
}

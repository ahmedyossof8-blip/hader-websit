package utils

import (
	"testing"
	"time"
)

func TestTOTPGenerationAndValidation(t *testing.T) {
	secret := "hader-super-secret-lecture-qr-key-2026"
	step := int64(5) // 5 seconds step

	// 1. Generate token
	token, remaining, err := GenerateDynamicTOTP(secret, step)
	if err != nil {
		t.Fatalf("Failed to generate TOTP: %v", err)
	}
	if len(token) != 6 {
		t.Errorf("Expected 6-digit TOTP, got %s (len %d)", token, len(token))
	}
	if remaining <= 0 || remaining > step {
		t.Errorf("Invalid remaining seconds: %d", remaining)
	}

	// 2. Validate current step token
	if !ValidateDynamicTOTP(secret, token, step) {
		t.Errorf("Expected current token to be valid")
	}

	// 3. Test previous step (t - 1) jitter tolerance
	now := time.Now().Unix()
	prevWindow := (now / step) - 1
	prevCode, err := computeTOTPCode(secret, prevWindow)
	if err != nil {
		t.Fatalf("Failed to compute prev code: %v", err)
	}
	if !ValidateDynamicTOTP(secret, prevCode, step) {
		t.Errorf("Expected previous step (t-1) token to be accepted for network jitter tolerance")
	}

	// 4. Test older step (t - 2) must be REJECTED
	oldWindow := (now / step) - 2
	oldCode, _ := computeTOTPCode(secret, oldWindow)
	if ValidateDynamicTOTP(secret, oldCode, step) {
		t.Errorf("Expected token from 2 windows ago (t-2) to be strictly REJECTED")
	}

	// 5. Test invalid/bogus code
	if ValidateDynamicTOTP(secret, "000000", step) && token != "000000" && prevCode != "000000" {
		t.Errorf("Expected arbitrary token to be rejected")
	}
	if ValidateDynamicTOTP("different-secret", token, step) {
		t.Errorf("Expected token with mismatched secret to be rejected")
	}
}

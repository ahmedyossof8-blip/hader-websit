package utils

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOfflineProofSignature(t *testing.T) {
	deviceID := "client-hardware-sha256-fingerprint-999"
	sessionID := uuid.New().String()
	totpCode := "481029"
	timestamp := time.Now().Unix()

	// 1. Generate valid proof signature
	sig := GenerateOfflineProofSignature(deviceID, sessionID, totpCode, timestamp)
	if len(sig) != 64 { // SHA-256 produces 32 bytes = 64 hex characters
		t.Errorf("Expected 64-char hex signature, got %d chars: %s", len(sig), sig)
	}

	// 2. Verify with identical parameters -> MUST be true
	if !VerifyOfflineProofSignature(deviceID, sessionID, totpCode, timestamp, sig) {
		t.Errorf("Valid signature failed verification")
	}

	// 3. Verify with tampered TOTP code -> MUST be false
	if VerifyOfflineProofSignature(deviceID, sessionID, "000000", timestamp, sig) {
		t.Errorf("Verification succeeded on tampered TOTP code")
	}

	// 4. Verify with tampered timestamp -> MUST be false
	if VerifyOfflineProofSignature(deviceID, sessionID, totpCode, timestamp+10, sig) {
		t.Errorf("Verification succeeded on tampered timestamp")
	}

	// 5. Verify with mismatched device ID -> MUST be false
	if VerifyOfflineProofSignature("different-device-id", sessionID, totpCode, timestamp, sig) {
		t.Errorf("Verification succeeded on mismatched device ID")
	}

	// 6. Verify with corrupted signature string -> MUST be false
	if VerifyOfflineProofSignature(deviceID, sessionID, totpCode, timestamp, "corrupted-sig") {
		t.Errorf("Verification succeeded on malformed signature string")
	}
}

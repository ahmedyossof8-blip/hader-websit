package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateOfflineProofSignature generates an HMAC-SHA256 cryptographic signature proving that
// a specific hardware device scanned a dynamic TOTP code at a specific timestamp.
// Payload format: "session_id:totp_code:timestamp".
func GenerateOfflineProofSignature(deviceID string, sessionID string, totpCode string, timestamp int64) string {
	payload := fmt.Sprintf("%s:%s:%d", sessionID, totpCode, timestamp)
	mac := hmac.New(sha256.New, []byte(deviceID))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyOfflineProofSignature validates an incoming offline proof signature against the student's hardware device ID.
// Uses hmac.Equal on the raw digest bytes to protect against timing side-channel attacks.
func VerifyOfflineProofSignature(deviceID, sessionID, totpCode string, timestamp int64, incomingSignature string) bool {
	if deviceID == "" || sessionID == "" || totpCode == "" || timestamp <= 0 || incomingSignature == "" {
		return false
	}

	incomingBytes, err := hex.DecodeString(strings.TrimSpace(incomingSignature))
	if err != nil {
		return false
	}

	payload := fmt.Sprintf("%s:%s:%d", sessionID, totpCode, timestamp)
	mac := hmac.New(sha256.New, []byte(deviceID))
	mac.Write([]byte(payload))
	expectedBytes := mac.Sum(nil)

	return hmac.Equal(expectedBytes, incomingBytes)
}

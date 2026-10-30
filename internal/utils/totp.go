package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GenerateDynamicTOTP produces a high-entropy 6-digit numeric token based on HMAC-SHA256
// with a configurable time-step (default 5 seconds for Hader-V2 dynamic anti-proxy QR codes).
// Returns the OTP string, remaining seconds in the current rotation window, and any error.
func GenerateDynamicTOTP(secret string, timeStepSeconds int64) (string, int64, error) {
	if secret == "" {
		return "", 0, errors.New("totp secret cannot be empty")
	}
	if timeStepSeconds <= 0 {
		timeStepSeconds = 5
	}

	now := time.Now().Unix()
	window := now / timeStepSeconds
	remainingSeconds := timeStepSeconds - (now % timeStepSeconds)

	code, err := computeTOTPCode(secret, window)
	if err != nil {
		return "", 0, err
	}

	return code, remainingSeconds, nil
}

// ValidateDynamicTOTP verifies whether an incoming OTP token matches either the current
// step t or the immediate previous step (t - 1) to accommodate reasonable network transit jitter.
func ValidateDynamicTOTP(secret, incomingCode string, timeStepSeconds int64) bool {
	if secret == "" || strings.TrimSpace(incomingCode) == "" {
		return false
	}
	if timeStepSeconds <= 0 {
		timeStepSeconds = 5
	}

	trimmedCode := strings.TrimSpace(incomingCode)
	now := time.Now().Unix()
	currentWindow := now / timeStepSeconds

	// 1. Verify current time window step t
	currentCode, err := computeTOTPCode(secret, currentWindow)
	if err == nil && subtle.ConstantTimeCompare([]byte(trimmedCode), []byte(currentCode)) == 1 {
		return true
	}

	// 2. Verify immediately preceding window step t - 1 (network jitter grace period)
	prevCode, err := computeTOTPCode(secret, currentWindow-1)
	if err == nil && subtle.ConstantTimeCompare([]byte(trimmedCode), []byte(prevCode)) == 1 {
		return true
	}

	return false
}

// ValidateHistoricalTOTP validates an offline scan token against an exact historical timestamp.
// Evaluates the discrete window corresponding to scanTimestamp and scanTimestamp - 1.
func ValidateHistoricalTOTP(secret, incomingCode string, scanTimestamp, timeStepSeconds int64) bool {
	if secret == "" || strings.TrimSpace(incomingCode) == "" || scanTimestamp <= 0 {
		return false
	}
	if timeStepSeconds <= 0 {
		timeStepSeconds = 5
	}

	trimmedCode := strings.TrimSpace(incomingCode)
	targetWindow := scanTimestamp / timeStepSeconds

	// 1. Verify exact window at historical scan timestamp
	targetCode, err := computeTOTPCode(secret, targetWindow)
	if err == nil && subtle.ConstantTimeCompare([]byte(trimmedCode), []byte(targetCode)) == 1 {
		return true
	}

	// 2. Verify adjacent window (step - 1) for transit jitter at moment of capture
	prevCode, err := computeTOTPCode(secret, targetWindow-1)
	if err == nil && subtle.ConstantTimeCompare([]byte(trimmedCode), []byte(prevCode)) == 1 {
		return true
	}

	return false
}

// ComputeTOTPForWindow generates the 6-digit token for a specific discrete time window using HMAC-SHA256.
func ComputeTOTPForWindow(secret string, window int64) (string, error) {
	return computeTOTPCode(secret, window)
}

// computeTOTPCode generates the 6-digit token for a specific discrete time window using HMAC-SHA256.
func computeTOTPCode(secret string, window int64) (string, error) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(window))

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(buf); err != nil {
		return "", err
	}
	hashResult := mac.Sum(nil)

	// RFC 4226 / RFC 6238 Dynamic Truncation
	offset := hashResult[len(hashResult)-1] & 0x0f
	binaryValue := (int(hashResult[offset]&0x7f) << 24) |
		(int(hashResult[offset+1]&0xff) << 16) |
		(int(hashResult[offset+2]&0xff) << 8) |
		(int(hashResult[offset+3] & 0xff))

	otp := binaryValue % 1000000
	return fmt.Sprintf("%06d", otp), nil
}

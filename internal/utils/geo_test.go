package utils

import (
	"math"
	"testing"
	"time"
)

func TestCalculateHaversineDistance(t *testing.T) {
	// Coordinate 1: Cairo Tahrir Square (30.0444, 31.2357)
	// Coordinate 2: Cairo University, Giza (30.0276, 31.2101)
	// Expected distance is approximately 3.1 - 3.2 km
	dist := CalculateHaversineDistance(30.0444, 31.2357, 30.0276, 31.2101)

	if dist < 3000.0 || dist > 3300.0 {
		t.Errorf("Expected distance between Tahrir and Cairo Univ ~3150m, got %.2f meters", dist)
	}

	// Identical coordinates must be 0 meters
	distZero := CalculateHaversineDistance(30.0444, 31.2357, 30.0444, 31.2357)
	if math.Abs(distZero) > 0.001 {
		t.Errorf("Expected identical coordinate distance to be 0, got %.4f", distZero)
	}
}

func TestCheckImpossibleTravel(t *testing.T) {
	now := time.Now()

	// Scenario 1: User moves 5 km in 2 minutes (150 km/h) -> Impossible travel
	time1 := now
	time2 := now.Add(2 * time.Minute)
	// Coordinate 1: 30.0444, 31.2357
	// Coordinate 2: ~5km away
	isImpossible, speed := CheckImpossibleTravel(30.0444, 31.2357, time1, 30.0900, 31.2357, time2)
	if !isImpossible {
		t.Errorf("Expected impossible travel flag true for 150 km/h displacement, got false (speed: %.2f)", speed)
	}

	// Scenario 2: Normal walking displacement: 100 meters in 2 minutes -> Allowed
	isImpossibleWalking, speedWalking := CheckImpossibleTravel(30.0444, 31.2357, time1, 30.0450, 31.2357, time2)
	if isImpossibleWalking {
		t.Errorf("Expected normal travel for walking, got impossible: true (speed: %.2f)", speedWalking)
	}

	// Scenario 3: High speed over long duration (e.g., flight / train trip of 45 minutes) -> Outside 5 min window
	time3 := now.Add(45 * time.Minute)
	isImpossibleLong, _ := CheckImpossibleTravel(30.0444, 31.2357, time1, 31.2001, 29.9187, time3)
	if isImpossibleLong {
		t.Errorf("Expected false because interval > 5 minutes, got true")
	}
}

func TestValidateGPSAccuracy(t *testing.T) {
	// Genuine GPS accuracy range
	if !ValidateGPSAccuracy(12.5) {
		t.Errorf("Expected 12.5m accuracy to be valid")
	}

	// Mock GPS zero accuracy must be rejected
	if ValidateGPSAccuracy(0.0) {
		t.Errorf("Expected 0.0m accuracy (Mock GPS) to be rejected")
	}

	// Degraded accuracy > 35m must be rejected
	if ValidateGPSAccuracy(42.0) {
		t.Errorf("Expected 42.0m accuracy to be rejected")
	}
}

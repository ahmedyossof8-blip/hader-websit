package utils

import (
	"math"
	"time"
)

const (
	// EarthRadiusMeters is the standard spherical mean radius of Earth in meters.
	EarthRadiusMeters = 6371000.0

	// MaxAcceptableGPSAccuracyMeters defines the maximum allowable horizontal GPS accuracy.
	// Readings higher than 35m indicate degraded precision or approximate cellular triangulation.
	MaxAcceptableGPSAccuracyMeters = 35.0

	// MinAcceptableGPSAccuracyMeters defines the minimum allowable accuracy.
	// Exactly 0.0 meters is universally characteristic of Mock GPS / fake location provider injection.
	MinAcceptableGPSAccuracyMeters = 0.01

	// ImpossibleTravelSpeedThresholdKmH defines the velocity threshold (60 km/h) over short intervals.
	ImpossibleTravelSpeedThresholdKmH = 60.0

	// ImpossibleTravelWindow defines the time window (5 minutes) evaluated for impossible travel.
	ImpossibleTravelWindow = 5 * time.Minute
)

// degToRad converts degrees to radians.
func degToRad(deg float64) float64 {
	return deg * (math.Pi / 180.0)
}

// CalculateHaversineDistance computes the great-circle distance between two geographic coordinates
// on the Earth's surface using the spherical Haversine formula, returning the distance in meters.
func CalculateHaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := degToRad(lat2 - lat1)
	dLon := degToRad(lon2 - lon1)

	phi1 := degToRad(lat1)
	phi2 := degToRad(lat2)

	sinDLat2 := math.Sin(dLat / 2.0)
	sinDLon2 := math.Sin(dLon / 2.0)

	a := (sinDLat2 * sinDLat2) + math.Cos(phi1)*math.Cos(phi2)*(sinDLon2*sinDLon2)
	// Clamp a to [0.0, 1.0] to prevent NaN due to floating-point rounding
	if a > 1.0 {
		a = 1.0
	}

	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return EarthRadiusMeters * c
}

// CheckImpossibleTravel determines whether a user's geographical displacement between two points
// in time exceeds physical human transit thresholds within a small window.
// Returns isImpossible (true if speed > 60 km/h within 5 mins) and the calculated speed in km/h.
func CheckImpossibleTravel(lat1, lon1 float64, time1 time.Time, lat2, lon2 float64, time2 time.Time) (bool, float64) {
	elapsed := time2.Sub(time1)
	if elapsed < 0 {
		elapsed = -elapsed
	}

	distanceMeters := CalculateHaversineDistance(lat1, lon1, lat2, lon2)
	seconds := elapsed.Seconds()

	// Zero or sub-second interval with significant distance implies teleportation / spoofing
	if seconds < 1.0 {
		if distanceMeters > 50.0 {
			return true, 9999.0
		}
		return false, 0.0
	}

	// Speed in km/h: (meters / seconds) * 3.6
	speedKmH := (distanceMeters / seconds) * 3.6

	if elapsed < ImpossibleTravelWindow && speedKmH > ImpossibleTravelSpeedThresholdKmH {
		return true, speedKmH
	}

	return false, speedKmH
}

// IsWithinGeofence validates whether a student's coordinate falls strictly within the permitted radius.
func IsWithinGeofence(userLat, userLng, centerLat, centerLng float64, radiusMeters float64) bool {
	dist := CalculateHaversineDistance(userLat, userLng, centerLat, centerLng)
	return dist <= radiusMeters
}

// ValidateGPSAccuracy checks if the device's reported horizontal accuracy is genuine and sufficient.
// Rejects zero/sub-zero values (Mock GPS) and values exceeding 35 meters.
func ValidateGPSAccuracy(accuracyMeters float64) bool {
	return accuracyMeters >= MinAcceptableGPSAccuracyMeters && accuracyMeters <= MaxAcceptableGPSAccuracyMeters
}

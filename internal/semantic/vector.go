package semantic

import "math"

// ValidateVector accepts only finite unit float32 vectors of the exact shape.
func ValidateVector(vector []float32, dimensions int) error {
	if dimensions < 1 || dimensions > MaxEmbeddingDimensions || len(vector) != dimensions {
		return newError(ReasonDimensionMismatch)
	}

	squaredNorm := 0.0

	for _, component := range vector {
		value := float64(component)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return newError(ReasonInvalidResponse)
		}

		squaredNorm += value * value
	}

	if math.Abs(squaredNorm-1) > VectorUnitTolerance {
		return newError(ReasonInvalidResponse)
	}

	return nil
}

// Cosine compares validated unit vectors with float64 accumulation.
func Cosine(left, right []float32) (float64, error) {
	if err := ValidateVector(left, len(left)); err != nil {
		return 0, err
	}

	if err := ValidateVector(right, len(left)); err != nil {
		return 0, err
	}

	dot := 0.0
	for index, component := range left {
		dot += float64(component) * float64(right[index])
	}

	return max(-1, min(1, dot)), nil
}

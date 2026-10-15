package embedding

import "math"

func normalize(input []*float64) ([]float32, error) {
	scale, err := vectorScale(input)
	if err != nil {
		return nil, err
	}

	norm := scaledNorm(input, scale)
	output := make([]float32, len(input))
	outputNorm := 0.0

	for index, component := range input {
		value := float32((*component / scale) / norm)
		if !finite(float64(value)) {
			return nil, newError(CodeInvalidResponse)
		}

		output[index] = value
		outputNorm += float64(value) * float64(value)
	}

	if outputNorm == 0 {
		return nil, newError(CodeInvalidResponse)
	}

	return output, nil
}

func vectorScale(input []*float64) (float64, error) {
	scale := 0.0

	for _, component := range input {
		if component == nil || !finite(*component) {
			return 0, newError(CodeInvalidResponse)
		}

		scale = math.Max(scale, math.Abs(*component))
	}

	if scale == 0 {
		return 0, newError(CodeInvalidResponse)
	}

	return scale, nil
}

func scaledNorm(input []*float64, scale float64) float64 {
	sum := 0.0

	for _, component := range input {
		scaled := *component / scale
		sum += scaled * scaled
	}

	return math.Sqrt(sum)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

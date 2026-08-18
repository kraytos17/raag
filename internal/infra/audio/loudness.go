package audio

import (
	"io"
	"math"
)

// EstimateLoudnessDB decodes the given audio and returns its loudness as a
// dBFS value (RMS). It is best-effort: callers should treat errors as "no
// loudness known" and fall back to 0.
func EstimateLoudnessDB(rc io.ReadCloser, mimeType string) (float32, error) {
	streamer, _, err := decode(rc, mimeType)
	if err != nil {
		return 0, err
	}
	defer func() { _ = streamer.Close() }()

	samples := make([][2]float64, 1024)
	var sumSquares float64
	var count float64
	for {
		n, ok := streamer.Stream(samples)
		if !ok || n <= 0 {
			break
		}
		for i := range n {
			l := samples[i][0]
			r := samples[i][1]
			sumSquares += l*l + r*r
			count += 2
		}
	}
	if count == 0 {
		return 0, io.EOF
	}

	rms := math.Sqrt(sumSquares / count)
	if rms <= 0 {
		return math.MinInt32, nil // effectively silence
	}
	db := 20 * math.Log10(rms)
	return float32(db), nil
}

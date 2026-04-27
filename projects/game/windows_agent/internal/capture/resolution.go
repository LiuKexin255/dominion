package capture

import "fmt"

// CalculateScale computes the output resolution while maintaining aspect ratio.
// Scales down only if either dimension exceeds the maximum; otherwise keeps original.
func CalculateScale(srcWidth, srcHeight, maxWidth, maxHeight int) (outWidth, outHeight int) {
	if srcWidth <= maxWidth && srcHeight <= maxHeight {
		return srcWidth, srcHeight
	}

	// Scale based on whichever dimension exceeds more aggressively.
	widthRatio := float64(maxWidth) / float64(srcWidth)
	heightRatio := float64(maxHeight) / float64(srcHeight)

	ratio := widthRatio
	if heightRatio < widthRatio {
		ratio = heightRatio
	}

	outWidth = int(float64(srcWidth) * ratio)
	outHeight = int(float64(srcHeight) * ratio)

	// Ensure dimensions are even (required by many codecs).
	outWidth = outWidth &^ 1
	outHeight = outHeight &^ 1

	return outWidth, outHeight
}

// CalculateBitrate estimates an appropriate bitrate string for a given resolution.
// Uses a simple pixel-count heuristic: bitrate ≈ pixels × 0.1 kbps.
func CalculateBitrate(width, height int) string {
	pixels := width * height
	// ~0.1 bits per pixel at 30fps baseline.
	kbps := pixels * 3 / 100
	if kbps < 500 {
		kbps = 500
	}
	return fmt.Sprintf("%dk", kbps)
}

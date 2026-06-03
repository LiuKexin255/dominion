package operation

import "fmt"

// ScreenshotToScreenCoords converts screenshot-relative pixel coordinates
// to absolute screen coordinates using the window position.
//
// screenshotX, screenshotY: pixel coordinates within the screenshot image.
// windowLeft, windowTop: screen position of the window's top-left corner.
func ScreenshotToScreenCoords(screenshotX, screenshotY int32, windowLeft, windowTop int32) (screenX, screenY int32, err error) {
	screenX = screenshotX + windowLeft
	screenY = screenshotY + windowTop
	return screenX, screenY, nil
}

// ValidateBounds checks that coordinates are within screenshot dimensions.
func ValidateBounds(x, y, width, height int32) error {
	if x < 0 || x >= width || y < 0 || y >= height {
		return fmt.Errorf("coordinates (%d,%d) out of bounds [%dx%d]", x, y, width, height)
	}
	return nil
}

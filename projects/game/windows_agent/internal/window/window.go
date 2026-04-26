package window

// Rect describes a Win32 window rectangle in screen coordinates.
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// WindowInfo contains the metadata needed to display and bind a top-level window.
type WindowInfo struct {
	HWND      uintptr
	Title     string
	ClassName string
	ProcessID uint32
	Rect      Rect
}

// Binding records the single window currently selected by the agent.
type Binding struct {
	Window  WindowInfo
	BoundAt int64
}

// OffsetClientPoint converts a point relative to the rectangle origin into screen coordinates.
func OffsetClientPoint(rect Rect, x int32, y int32) (int32, int32) {
	return rect.Left + x, rect.Top + y
}

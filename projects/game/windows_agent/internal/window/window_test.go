package window

import "testing"

func TestWindowInfo(t *testing.T) {
	tests := []struct {
		name string
		info WindowInfo
	}{
		{
			name: "stores window metadata",
			info: WindowInfo{
				HWND:      100,
				Title:     "Game Window",
				ClassName: "GameClass",
				ProcessID: 200,
				Rect: Rect{
					Left:   10,
					Top:    20,
					Right:  810,
					Bottom: 620,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			got := tt.info

			// then
			if got.HWND != 100 {
				t.Fatalf("WindowInfo.HWND = %d, want 100", got.HWND)
			}
			if got.Title != "Game Window" {
				t.Fatalf("WindowInfo.Title = %q, want Game Window", got.Title)
			}
			if got.ClassName != "GameClass" {
				t.Fatalf("WindowInfo.ClassName = %q, want GameClass", got.ClassName)
			}
			if got.ProcessID != 200 {
				t.Fatalf("WindowInfo.ProcessID = %d, want 200", got.ProcessID)
			}
			if got.Rect.Left != 10 || got.Rect.Top != 20 || got.Rect.Right != 810 || got.Rect.Bottom != 620 {
				t.Fatalf("WindowInfo.Rect = %+v, want left=10 top=20 right=810 bottom=620", got.Rect)
			}
		})
	}
}

func TestBinding(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
	}{
		{
			name: "stores selected window and timestamp",
			binding: Binding{
				Window: WindowInfo{
					HWND:      300,
					Title:     "Bound Window",
					ClassName: "BoundClass",
					ProcessID: 400,
				},
				BoundAt: 1710000000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			got := tt.binding

			// then
			if got.Window.HWND != 300 {
				t.Fatalf("Binding.Window.HWND = %d, want 300", got.Window.HWND)
			}
			if got.Window.Title != "Bound Window" {
				t.Fatalf("Binding.Window.Title = %q, want Bound Window", got.Window.Title)
			}
			if got.Window.ClassName != "BoundClass" {
				t.Fatalf("Binding.Window.ClassName = %q, want BoundClass", got.Window.ClassName)
			}
			if got.Window.ProcessID != 400 {
				t.Fatalf("Binding.Window.ProcessID = %d, want 400", got.Window.ProcessID)
			}
			if got.BoundAt != 1710000000 {
				t.Fatalf("Binding.BoundAt = %d, want 1710000000", got.BoundAt)
			}
		})
	}
}

func TestNonWindowsStubs(t *testing.T) {
	tests := []struct {
		name string
		hwnd uintptr
	}{
		{name: "zero handle", hwnd: 0},
		{name: "non-zero handle", hwnd: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			windows, err := EnumerateWindows()

			// then
			if err != nil {
				t.Fatalf("EnumerateWindows() unexpected error: %v", err)
			}
			if windows != nil {
				t.Fatalf("EnumerateWindows() = %v, want nil", windows)
			}

			if IsWindowValid(tt.hwnd) {
				t.Fatalf("IsWindowValid(%d) = true, want false", tt.hwnd)
			}

			rect, err := GetWindowRect(tt.hwnd)
			if err == nil {
				t.Fatalf("GetWindowRect(%d) expected error", tt.hwnd)
			}
			if rect != (Rect{}) {
				t.Fatalf("GetWindowRect(%d) rect = %+v, want zero value", tt.hwnd, rect)
			}

			screenX, screenY, err := ClientToScreen(tt.hwnd, 5, 7)
			if err == nil {
				t.Fatalf("ClientToScreen(%d, 5, 7) expected error", tt.hwnd)
			}
			if screenX != 0 || screenY != 0 {
				t.Fatalf("ClientToScreen(%d, 5, 7) = (%d, %d), want (0, 0)", tt.hwnd, screenX, screenY)
			}
		})
	}
}

func TestOffsetClientPoint(t *testing.T) {
	tests := []struct {
		name  string
		rect  Rect
		x     int32
		y     int32
		wantX int32
		wantY int32
	}{
		{
			name:  "positive window origin",
			rect:  Rect{Left: 100, Top: 200, Right: 500, Bottom: 600},
			x:     12,
			y:     34,
			wantX: 112,
			wantY: 234,
		},
		{
			name:  "negative window origin",
			rect:  Rect{Left: -50, Top: -25, Right: 450, Bottom: 375},
			x:     60,
			y:     30,
			wantX: 10,
			wantY: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			gotX, gotY := OffsetClientPoint(tt.rect, tt.x, tt.y)

			// then
			if gotX != tt.wantX || gotY != tt.wantY {
				t.Fatalf("OffsetClientPoint(%+v, %d, %d) = (%d, %d), want (%d, %d)", tt.rect, tt.x, tt.y, gotX, gotY, tt.wantX, tt.wantY)
			}
		})
	}
}

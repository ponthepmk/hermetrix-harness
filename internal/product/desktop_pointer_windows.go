//go:build windows

package product

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var desktopUser32 = syscall.NewLazyDLL("user32.dll")
var errDesktopNotDispatched = errors.New("mouse input was not dispatched")

type desktopRect struct{ Left, Top, Right, Bottom int32 }
type desktopMouseInput struct {
	DX, DY                 int32
	MouseData, Flags, Time uint32
	ExtraInfo              uintptr
}
type desktopInput struct {
	Kind  uint32
	Mouse desktopMouseInput
}

// Dispatch one left click in a reviewed window, never a global unbound click.
// Activation, geometry, PID and hit-testing are checked before input injection.
func clickDesktopPoint(ctx context.Context, item DesktopAction) (result error) {
	dispatched := false
	defer func() {
		if result != nil && !dispatched {
			result = fmt.Errorf("%w: %v", errDesktopNotDispatched, result)
		}
	}()
	if item.Target.Point == nil {
		return fmt.Errorf("missing reviewed point")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dpi := desktopUser32.NewProc("SetThreadDpiAwarenessContext")
	old, _, _ := dpi.Call(^uintptr(3))
	if old != 0 {
		defer dpi.Call(old)
	}
	hwnd := uintptr(item.Handle)
	var pid uint32
	desktopUser32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if int(pid) != item.ProcessID {
		return fmt.Errorf("window process changed")
	}
	minimized, _, _ := desktopUser32.NewProc("IsIconic").Call(hwnd)
	if minimized != 0 {
		return fmt.Errorf("restore the minimized window and inspect again")
	}
	desktopUser32.NewProc("SetForegroundWindow").Call(hwnd)
	// Windows may deny focus to a background process. Raise only the selected
	// window without stealing focus; the hit-test below still requires that the
	// actual clicked pixel belongs to it. Never attach another app's input queue.
	desktopUser32.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, 0, 0, 0x0001|0x0002|0x0010|0x4000)
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	var rect desktopRect
	ok, _, _ := desktopUser32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	b := item.Target.Bounds
	if ok == 0 || int(rect.Left) != b.X || int(rect.Top) != b.Y || int(rect.Right-rect.Left) != b.Width || int(rect.Bottom-rect.Top) != b.Height {
		return fmt.Errorf("window geometry changed; inspect again")
	}
	p := item.Target.Point
	if p.X < 0 || p.Y < 0 || p.X >= b.Width || p.Y >= b.Height {
		return fmt.Errorf("point is outside the reviewed window")
	}
	x, y := int32(b.X+p.X), int32(b.Y+p.Y)
	// WindowFromPoint takes POINT by value. This runtime targets Windows amd64.
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return fmt.Errorf("pointer control requires Windows amd64")
	}
	packed := uintptr(uint64(uint32(x)) | uint64(uint32(y))<<32)
	hit, _, _ := desktopUser32.NewProc("WindowFromPoint").Call(packed)
	root, _, _ := desktopUser32.NewProc("GetAncestor").Call(hit, 2)
	if root != hwnd {
		return fmt.Errorf("point is covered by another window; no click sent")
	}
	metric := func(n uintptr) int32 { v, _, _ := desktopUser32.NewProc("GetSystemMetrics").Call(n); return int32(v) }
	left, top, width, height := metric(76), metric(77), metric(78), metric(79)
	if width < 2 || height < 2 || x < left || y < top || x >= left+width || y >= top+height {
		return fmt.Errorf("point is outside the interactive desktop")
	}
	dx, dy := int32(int64(x-left)*65535/int64(width-1)), int32(int64(y-top)*65535/int64(height-1))
	inputs := [2]desktopInput{{Mouse: desktopMouseInput{DX: dx, DY: dy, Flags: 0x8000 | 0x4000 | 0x0001 | 0x0002}}, {Mouse: desktopMouseInput{DX: dx, DY: dy, Flags: 0x8000 | 0x4000 | 0x0001 | 0x0004}}}
	if err := ctx.Err(); err != nil {
		return err
	}
	dispatched = true
	count, _, err := desktopUser32.NewProc("SendInput").Call(2, uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if count != 2 {
		return fmt.Errorf("mouse input outcome uncertain (%d/2): %v", count, err)
	}
	return nil
}

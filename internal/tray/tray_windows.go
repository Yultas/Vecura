//go:build windows

package tray

import (
	"context"
	"os"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// Win32 constants for the system tray.
const (
	nimAdd         = 0x00000000
	nimDelete      = 0x00000002
	nifMessage     = 0x00000001
	nifIcon        = 0x00000002
	nifTip         = 0x00000004
	wmUser         = 0x0400
	wmTrayIcon     = wmUser + 1
	lrLoadfromfile = 0x00000010
	imageIcon      = 1
	mfString       = 0x00000000
	tpmBottomalign = 0x00000020
	tpmLeftalign   = 0x00000000
	tpmRightbutton = 0x00000002
	wsExToolwindow = 0x00000080
	// HWND_MESSAGE is a special value for message-only windows.
	HWND_MESSAGE  = ^uintptr(2) // -3
	wmRButtonDown = 0x0204
	wmLButtonDown = 0x0201
	wmMouseMove   = 0x0200
)

// notifyIconData matches NOTIFYICONDATAW (unicode).
type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

// wndClassEx is a minimal WNDCLASSEXW.
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	clsExtra      int32
	wndExtra      int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// menuItem is a tray menu entry with a callback.
type menuItem struct {
	label    string
	onSelect func()
}

// Tray manages a Windows system tray icon with a popup menu.
type Tray struct {
	hWnd     uintptr
	hIcon    uintptr
	iconData []byte
	items    []menuItem
	ctx      context.Context
	quit     chan struct{}
}

// New creates a tray with the given icon data (.ico bytes). The context is
// used to show/quit the Wails window. Call AddItem before Run.
func New(iconData []byte, ctx context.Context) *Tray {
	return &Tray{
		iconData: iconData,
		ctx:      ctx,
		quit:     make(chan struct{}),
	}
}

// AddItem appends a menu entry. Call before Run.
func (t *Tray) AddItem(label string, onSelect func()) {
	t.items = append(t.items, menuItem{label: label, onSelect: onSelect})
}

// SetActive wires the global reference so menu callbacks reach this instance.
func (t *Tray) SetActive() { currentTray = t }

// Run creates the icon and enters the message loop. It blocks until Quit.
func (t *Tray) Run() error {
	user32 := windows.NewLazySystemDLL("user32.dll")
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")

	// Write icon data to a temp file (LoadImageW needs a file path).
	tmp, err := os.CreateTemp("", "vecura-*.ico")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(t.iconData); err != nil {
		return err
	}
	tmp.Close()

	hInst, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	t.hIcon, _, _ = user32.NewProc("LoadImageW").Call(
		0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(tmp.Name()))),
		imageIcon, 0, 0, lrLoadfromfile,
	)

	className, _ := windows.UTF16PtrFromString("VecuraTrayClass")
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   windows.NewCallback(wndProc),
		hInstance:     uintptr(hInst),
		lpszClassName: className,
	}
	reg, _, _ := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))
	if reg == 0 {
		return windows.GetLastError()
	}

	hwnd, _, _ := user32.NewProc("CreateWindowExW").Call(
		wsExToolwindow,
		uintptr(unsafe.Pointer(className)),
		0, 0, 0, 0, 0, 0,
		uintptr(HWND_MESSAGE),
		0, uintptr(hInst), 0,
	)
	t.hWnd = hwnd

	nid := notifyIconData{
		cbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		hWnd:             hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayIcon,
		hIcon:            t.hIcon,
	}
	copyUTF16(nid.szTip[:], "Vecura")
	shell32.NewProc("Shell_NotifyIconW").Call(nimAdd, uintptr(unsafe.Pointer(&nid)))

	msg := struct {
		hwnd    uintptr
		message uint32
		wparam  uintptr
		lparam  uintptr
		time    uint32
		pt      struct{ x, y int32 }
	}{}
	for {
		select {
		case <-t.quit:
			shell32.NewProc("Shell_NotifyIconW").Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
			return nil
		default:
		}
		ret, _, _ := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			return nil // WM_QUIT
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// Quit removes the icon and stops the loop.
func (t *Tray) Quit() {
	close(t.quit)
}

// wndProc handles tray callbacks and shows the popup menu.
func wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	user32 := windows.NewLazySystemDLL("user32.dll")
	if msg == wmTrayIcon {
		switch lparam {
		case wmMouseMove, wmLButtonDown, wmRButtonDown:
			showMenu(hwnd)
		}
	}
	ret, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

// showMenu builds and displays the tray popup menu, dispatching the chosen
// item's callback.
func showMenu(hwnd uintptr) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	hMenu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	for i, it := range currentTray.items {
		label, _ := windows.UTF16PtrFromString(it.label)
		user32.NewProc("AppendMenuW").Call(hMenu, mfString, uintptr(i+1), uintptr(unsafe.Pointer(label)))
	}
	user32.NewProc("SetForegroundWindow").Call(hwnd)
	var pt struct{ x, y int32 }
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))
	res, _, _ := user32.NewProc("TrackPopupMenu").Call(
		hMenu,
		tpmBottomalign|tpmLeftalign|tpmRightbutton,
		uintptr(pt.x), uintptr(pt.y),
		0, hwnd, 0,
	)
	if idx := int(res) - 1; idx >= 0 && idx < len(currentTray.items) {
		currentTray.items[idx].onSelect()
	}
	user32.NewProc("DestroyMenu").Call(hMenu)
}

// copyUTF16 copies a Go string into a UTF-16 array.
func copyUTF16(dst []uint16, s string) {
	for i, r := range s {
		if i >= len(dst)-1 {
			break
		}
		dst[i] = uint16(r)
	}
}

// currentTray is the active tray, used by showMenu to dispatch clicks.
var currentTray *Tray

// Show shows the main Wails window.
func Show(ctx context.Context) { runtime.WindowShow(ctx) }

// QuitApp quits the Wails application.
func QuitApp(ctx context.Context) { runtime.Quit(ctx) }

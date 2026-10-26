package main

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	getConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	showWindow       = user32.NewProc("ShowWindow")
	messageBoxW      = user32.NewProc("MessageBoxW")
	shellExecuteExW  = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteExW")
)

const (
	swMinimize   = 6
	swShowNormal = 1
	mbIconErr    = 0x10

	seeMaskNoAsync  = 0x00000100 // return only after the handler has taken over (the caller may exit right after)
	seeMaskFlagNoUI = 0x00000400 // no Windows error dialog: the error comes back to us
)

// shellExecuteInfo is SHELLEXECUTEINFOW; Go's field alignment matches the C layout on 386 and amd64.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

// shellOpen opens a URL or file with its registered handler, like a double click in Explorer.
// Unlike starting "rundll32 url.dll,FileProtocolHandler" it reports whether that actually worked.
func shellOpen(target string) error {
	// the shell may use COM for the handler: an apartment-threaded COM thread, as the ShellExecuteEx docs ask for
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil {
		defer windows.CoUninitialize()
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	file, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{fMask: seeMaskNoAsync | seeMaskFlagNoUI, lpVerb: verb, lpFile: file, nShow: swShowNormal}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if ok, _, err := shellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		return err
	}
	return nil
}

// defaultBrowserProgID returns the ProgID Windows uses for http links ("" = none set).
func defaultBrowserProgID() string {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\Shell\Associations\UrlAssociations\http\UserChoice`, registry.READ)
	if err == nil {
		defer k.Close()
		if id, _, err := k.GetStringValue("ProgId"); err == nil && id != "" {
			return id
		}
	}
	// no user choice: the system-wide http handler (older Windows versions)
	k2, err := registry.OpenKey(registry.CLASSES_ROOT, `http\shell\open\command`, registry.READ)
	if err == nil {
		defer k2.Close()
		if cmd, _, err := k2.GetStringValue(""); err == nil && strings.TrimSpace(cmd) != "" {
			return "http"
		}
	}
	return ""
}

// checkDefaultBrowser fails if no default browser is configured or its handler is missing.
func checkDefaultBrowser() error {
	id := defaultBrowserProgID()
	if id == "" {
		return errors.New("no default browser is set")
	}
	k, err := registry.OpenKey(registry.CLASSES_ROOT, id+`\shell\open\command`, registry.READ)
	if err != nil {
		return errors.New("the default browser (" + id + ") is not installed correctly")
	}
	defer k.Close()
	if cmd, _, err := k.GetStringValue(""); err != nil || strings.TrimSpace(cmd) == "" {
		return errors.New("the default browser (" + id + ") has no start command")
	}
	return nil
}

// openBrowser opens the url in the default browser.
func openBrowser(url string) error {
	if err := checkDefaultBrowser(); err != nil {
		return err
	}
	return shellOpen(url)
}

// minimizeConsole minimizes the console window of this process (if it has one).
func minimizeConsole() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		_, _, _ = showWindow.Call(hwnd, swMinimize)
	}
}

// showError shows a message box (the console may be minimized or closed right after).
func showError(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	_, _, _ = messageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconErr)
}

// minimizeConsoleSoon gives the browser a moment to take focus first.
func minimizeConsoleSoon() {
	go func() {
		time.Sleep(800 * time.Millisecond)
		minimizeConsole()
	}()
}

package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const csExe = "cs2.exe"

// lastChange is the time of the last apply/reset made by this process (unix nanoseconds).
// Together with the conf file's mtime it tells whether the running game predates our changes.
var lastChange atomic.Int64

func markChanged() { lastChange.Store(time.Now().UnixNano()) }

// csProcess looks for a running cs2.exe and returns its pid and start time.
func csProcess() (pid uint32, start time.Time, ok bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, time.Time{}, false
	}
	defer windows.CloseHandle(snap)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), csExe) {
			return e.ProcessID, processStart(e.ProcessID), true
		}
	}
	return 0, time.Time{}, false
}

// processStart returns the creation time of a process (zero time if it cannot be read).
func processStart(pid uint32) time.Time {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return time.Time{}
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &creation, &exit, &kernel, &user) != nil {
		return time.Time{}
	}
	return time.Unix(0, creation.Nanoseconds())
}

// needsRestart: the game is running and was started before our last change to the font files.
// An unknown start time counts as "restart needed", the safe side for the user.
func needsRestart(running bool, started, changed time.Time) bool {
	if !running || changed.IsZero() {
		return false
	}
	return started.IsZero() || changed.After(started)
}

// changedAt is the newest known change: this session's apply/reset or the mtime of our conf file.
func changedAt(root string) time.Time {
	t := time.Time{}
	if ns := lastChange.Load(); ns != 0 {
		t = time.Unix(0, ns)
	}
	if st, err := os.Stat(confFile(root)); err == nil && st.ModTime().After(t) {
		t = st.ModTime()
	}
	return t
}

// restartCS asks a running CS2 to close (like the window's close button, no force kill),
// waits for it to exit and starts the game again through Steam.
func restartCS() error {
	if pid, _, ok := csProcess(); ok {
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(int(pid)))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
		deadline := time.Now().Add(20 * time.Second)
		for {
			if _, _, still := csProcess(); !still {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("CS2 did not close within 20 seconds. Close it yourself and start it again")
			}
			time.Sleep(300 * time.Millisecond)
		}
		time.Sleep(time.Second) // let Steam notice that the game has ended
	}
	return shellOpen("steam://rungameid/730")
}

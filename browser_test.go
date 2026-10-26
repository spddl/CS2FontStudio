package main

import (
	"path/filepath"
	"testing"
)

func TestDefaultBrowserDetected(t *testing.T) {
	id := defaultBrowserProgID()
	t.Logf("ProgID: %q", id)
	if err := checkDefaultBrowser(); err != nil {
		t.Skipf("no default browser on this machine: %v", err)
	}
}

func TestShellOpenReportsErrors(t *testing.T) {
	// a missing file fails without opening anything (and, with SEE_MASK_FLAG_NO_UI, without a Windows dialog)
	if err := shellOpen(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Error("opening a missing file reported success")
	} else {
		t.Logf("error as expected: %v", err)
	}
}

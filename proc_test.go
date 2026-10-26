package main

import (
	"os"
	"testing"
	"time"
)

func TestNeedsRestart(t *testing.T) {
	start := time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name             string
		running          bool
		started, changed time.Time
		want             bool
	}{
		{"game not running", false, start, start.Add(time.Hour), false},
		{"never changed", true, start, time.Time{}, false},
		{"changed after launch", true, start, start.Add(time.Minute), true},
		{"changed before launch", true, start, start.Add(-time.Minute), false},
		{"start time unknown", true, time.Time{}, start, true},
	}
	for _, c := range cases {
		if got := needsRestart(c.running, c.started, c.changed); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestChangedAtUsesConfMtime(t *testing.T) {
	root := fakeRoot(t)
	lastChange.Store(0)
	if !changedAt(root).IsZero() {
		t.Fatal("no conf and no session change: want zero time")
	}
	if err := os.WriteFile(confFile(root), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changedAt(root).IsZero() {
		t.Error("conf file exists: want its mtime")
	}
}

package io

import "testing"

func TestClosePolicy(t *testing.T) {
	if got := Close(3, func(int) bool { return true }, nil); got != 0 {
		t.Fatal(got)
	}
	if got := Close(9, func(int) bool { return false }, func(int) bool { return false }); got != -9 {
		t.Fatal(got)
	}
}

func TestDupPolicy(t *testing.T) {
	valid := func(fd int) bool { return fd == 3 }
	install := func(_, newFD int, _ uint32, _ bool) int32 { return int32(newFD) }
	if got := Dup(3, 4, 0, false, valid, install); got != 4 {
		t.Fatal(got)
	}
	if got := Dup(3, 3, 0, true, valid, install); got != -22 {
		t.Fatal(got)
	}
}

package io

func Close(fd int, descriptor func(int) bool, tty func(int) bool) int32 {
	if descriptor(fd) || tty(fd) {
		return 0
	}
	return -9
}

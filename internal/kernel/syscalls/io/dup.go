package io

func Dup(oldFD, newFD int, flags uint32, dup3 bool, valid func(int) bool, install func(int, int, uint32, bool) int32) int32 {
	if newFD < 0 || oldFD < 0 || (flags != 0 && flags != 0x80000) {
		return -22
	}
	if oldFD == newFD {
		if !dup3 && valid(oldFD) {
			return int32(newFD)
		}
		return -22
	}
	if !valid(oldFD) {
		return -9
	}
	return install(oldFD, newFD, flags, dup3)
}

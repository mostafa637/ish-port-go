package kernel

func (k *Kernel) dupInstall(oldFD, newFD int, flags uint32, dup3 bool) int32 {
	handle, _ := k.handleForFD(oldFD)
	if old, exists := k.fds[newFD]; exists && old != handle {
		_ = old.Close()
	}
	k.fds[newFD] = cloneHandle(handle)
	delete(k.closedFDs, newFD)
	delete(k.fdCloexec, newFD)
	if dup3 && flags&0x80000 != 0 {
		k.fdCloexec[newFD] = true
	}
	if newFD >= k.nextFD {
		k.nextFD = newFD + 1
	}
	return int32(newFD)
}

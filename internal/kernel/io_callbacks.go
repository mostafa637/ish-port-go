package kernel

func (k *Kernel) closeDescriptor(fd int) bool {
	f, ok := k.fds[fd]
	if !ok { return false }
	_ = f.Close(); delete(k.fds, fd); delete(k.closedFDs, fd); delete(k.fdCloexec, fd)
	return true
}

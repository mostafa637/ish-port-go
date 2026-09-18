package kernel

func (k *Kernel) closeTTY(fd int) bool {
	if fd < 0 || fd > 2 || k.TTY == nil || k.closedFDs[fd] { return false }
	k.closedFDs[fd] = true
	return true
}

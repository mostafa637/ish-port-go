package kernel

func (k *Kernel) validFD(fd int) bool {
	_, ok := k.handleForFD(fd)
	return ok
}

package memory

func PageLength(length uint32) (uint32, bool) {
	if length == 0 || length > ^uint32(0)-4094 {
		return 0, false
	}
	return (length + 4095) &^ 4095, true
}

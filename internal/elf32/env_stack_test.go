package elf32

import "testing"

func TestInitialStackContainsEnvironment(t *testing.T) {
	image, err := LoadWithEnv(tinyELF([]byte{0xF4}), []string{"/bin/test"}, []string{"PATH=/bin", "TERM=xterm"})
	if err != nil {
		t.Fatal(err)
	}
	sp := image.CPU.Regs[4]
	argc, err := image.Memory.Read32(sp)
	if err != nil || argc != 1 {
		t.Fatalf("argc=(%d,%v)", argc, err)
	}
	argv0, err := image.Memory.Read32(sp + 4)
	if err != nil {
		t.Fatal(err)
	}
	value, err := image.Memory.ReadCString(argv0, 128)
	if err != nil || value != "/bin/test" {
		t.Fatalf("argv0=(%q,%v)", value, err)
	}
	env0, err := image.Memory.Read32(sp + 12)
	if err != nil {
		t.Fatal(err)
	}
	value, err = image.Memory.ReadCString(env0, 128)
	if err != nil || value != "PATH=/bin" {
		t.Fatalf("env0=(%q,%v)", value, err)
	}
	auxNull, err := image.Memory.Read32(sp + 20)
	if err != nil || auxNull != 0 {
		t.Fatalf("auxv terminator=(%d,%v)", auxNull, err)
	}
}

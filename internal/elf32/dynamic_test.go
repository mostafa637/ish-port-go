package elf32

import (
	"os"
	"path/filepath"
	"testing"

	"example.com/ish-go/internal/i386"
)

func TestLoadAlpineDynamicBusybox(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "alpine-x86")
	program := filepath.Join(root, "bin", "busybox")
	if _, err := os.Stat(program); err != nil {
		t.Skip("Alpine testdata is not present")
	}
	image, err := LoadFileWithEnvResolver(program, []string{"/bin/busybox", "true"}, nil, func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	})
	if err != nil {
		t.Fatal(err)
	}
	if image.Interpreter != "/lib/ld-musl-i386.so.1" {
		t.Fatalf("interpreter=%q", image.Interpreter)
	}
	if image.Start == image.Entry || image.InterpreterEntry == 0 {
		t.Fatalf("start=0x%x entry=0x%x interp=0x%x", image.Start, image.Entry, image.InterpreterEntry)
	}
	if mapping, ok := image.Space.MappingAt(image.InterpreterEntry); !ok || mapping.Prot&i386.ProtExec == 0 {
		t.Fatalf("interpreter mapping=(%+v,%t)", mapping, ok)
	}
	if image.CPU.EIP != image.InterpreterEntry {
		t.Fatalf("cpu eip=0x%x", image.CPU.EIP)
	}
}

func TestAlpineDynamicAuxv(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "alpine-x86")
	program := filepath.Join(root, "bin", "busybox")
	if _, err := os.Stat(program); err != nil {
		t.Skip("Alpine testdata is not present")
	}
	image, err := LoadFileWithEnvResolver(program, []string{"/bin/busybox", "true"}, nil, func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	})
	if err != nil {
		t.Fatal(err)
	}
	argc, err := image.Memory.Read32(image.Stack)
	if err != nil || argc != 2 {
		t.Fatalf("argc=%d err=%v", argc, err)
	}
	cursor := image.Stack + 4 + (argc+1)*4
	for {
		value, readErr := image.Memory.Read32(cursor)
		if readErr != nil {
			t.Fatal(readErr)
		}
		cursor += 4
		if value == 0 {
			break
		}
	}
	aux := make(map[uint32]uint32)
	for {
		key, readErr := image.Memory.Read32(cursor)
		if readErr != nil {
			t.Fatal(readErr)
		}
		value, readErr := image.Memory.Read32(cursor + 4)
		if readErr != nil {
			t.Fatal(readErr)
		}
		cursor += 8
		if key == 0 {
			break
		}
		aux[key] = value
	}
	if aux[3] != 0x34 || aux[4] != 32 || aux[5] != 12 || aux[6] != pageSize {
		t.Fatalf("program auxv=%#v", aux)
	}
	if aux[7] != interpreterBase || aux[9] != image.Entry || aux[25] == 0 || aux[31] == 0 {
		t.Fatalf("dynamic auxv=%#v entry=0x%x", aux, image.Entry)
	}
}

func TestAlpineInterpreterRelocationsRemainGuestOwned(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "alpine-x86")
	program := filepath.Join(root, "bin", "busybox")
	if _, err := os.Stat(program); err != nil {
		t.Skip("Alpine testdata is not present")
	}
	image, err := LoadFileWithEnvResolver(program, []string{"/bin/busybox", "true"}, nil, func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := image.Memory.Read32(interpreterBase + 0xa52b4)
	if err != nil {
		t.Fatal(err)
	}
	if value != 0x0006fb0f {
		t.Fatalf("interpreter relocation target=0x%x want raw 0x6fb0f", value)
	}
}

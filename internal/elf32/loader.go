package elf32

import (
	"crypto/rand"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"example.com/ish-go/internal/i386"
)

const (
	defaultMemory          = 128 << 20
	stackReserve           = 8 << 20
	interpreterBase        = 0x10000000
	pageSize        uint32 = 4096
)

type DynamicLinkerError struct {
	Interpreter string
}

func (e *DynamicLinkerError) Error() string {
	return fmt.Sprintf("ELF requires dynamic linker %q; no resolver was supplied", e.Interpreter)
}

type FileResolver func(string) ([]byte, error)

type Image struct {
	Memory           *i386.Memory
	Space            *i386.AddressSpace
	CPU              *i386.CPU
	Entry            uint32
	Start            uint32
	Interpreter      string
	InterpreterEntry uint32
	Stack            uint32
	Brk              uint32
	Env              []string
}

type auxEntry struct {
	key uint32
	val uint32
}

func LoadFile(path string, argv []string) (*Image, error) {
	return LoadFileWithEnv(path, argv, nil)
}

func LoadFileWithEnv(path string, argv, envp []string) (*Image, error) {
	return LoadFileWithEnvResolver(path, argv, envp, nil)
}

func LoadFileWithEnvResolver(path string, argv, envp []string, resolve FileResolver) (*Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadWithEnvResolver(data, argv, envp, resolve)
}

func Load(data []byte, argv []string) (*Image, error) {
	return LoadWithEnv(data, argv, nil)
}

func LoadWithEnv(data []byte, argv, envp []string) (*Image, error) {
	return LoadWithEnvResolver(data, argv, envp, nil)
}

func LoadWithEnvResolver(data []byte, argv, envp []string, resolve FileResolver) (*Image, error) {
	mainFile, err := elf.NewFile(bytesReader(data))
	if err != nil {
		return nil, fmt.Errorf("ELF: %w", err)
	}
	defer mainFile.Close()
	if err := validateELF(mainFile, data); err != nil {
		return nil, err
	}
	interpName, err := interpreterPath(mainFile)
	if err != nil {
		return nil, err
	}
	var interpData []byte
	var interpFile *elf.File
	if interpName != "" {
		if resolve == nil {
			return nil, &DynamicLinkerError{Interpreter: interpName}
		}
		interpData, err = resolve(interpName)
		if err != nil {
			return nil, fmt.Errorf("load interpreter %q: %w", interpName, err)
		}
		interpFile, err = elf.NewFile(bytesReader(interpData))
		if err != nil {
			return nil, fmt.Errorf("interpreter ELF: %w", err)
		}
		defer interpFile.Close()
		if err := validateELF(interpFile, interpData); err != nil {
			return nil, fmt.Errorf("interpreter: %w", err)
		}
	}

	// The current flat guest map keeps the main ET_DYN image at bias zero;
	// a randomized/nonzero PIE bias requires complete in-guest auxv/linker
	// validation and remains explicitly unsupported for now.
	mainBase := uint32(0)
	mainMax, mainBrk, err := imageBounds(mainFile, data, mainBase)
	if err != nil {
		return nil, err
	}
	max := mainMax
	interpMax := uint64(0)
	if interpFile != nil {
		interpMax, _, err = imageBounds(interpFile, interpData, interpreterBase)
		if err != nil {
			return nil, err
		}
		if interpMax > max {
			max = interpMax
		}
	}
	if max == 0 {
		return nil, fmt.Errorf("ELF contains no loadable segments")
	}
	size := uint64(defaultMemory)
	if need := max + uint64(stackReserve) + uint64(pageSize); need > size {
		size = need
	}
	if size > uint64(^uint32(0)) {
		return nil, fmt.Errorf("guest address space too large")
	}
	space := i386.NewAddressSpace(uint32(size))
	if err := loadSegments(space, mainFile, data, mainBase, "ELF PT_LOAD"); err != nil {
		return nil, err
	}
	mainEntry := mainBase + uint32(mainFile.Entry)
	start := mainEntry
	interpEntry := uint32(0)
	if interpFile != nil {
		if err := loadSegments(space, interpFile, interpData, interpreterBase, "ELF interpreter"); err != nil {
			return nil, err
		}
		// `_dlstart_c` in the guest applies the interpreter's early
		// relative relocations before symbolic lookup.

		interpEntry = interpreterBase + uint32(interpFile.Entry)
		start = interpEntry
	}
	stackBase := space.Mem.Size() - stackReserve
	if err := space.Add(stackBase, stackReserve-pageSize, i386.ProtRead|i386.ProtWrite, "guest stack"); err != nil {
		return nil, err
	}
	stackTop := (space.Mem.Size() - pageSize) &^ 15
	phdr := programHeaderAddress(mainFile, data, mainBase)
	randomAddr := stackTop - 16
	randomData := make([]byte, 16)
	if _, err := rand.Read(randomData); err != nil {
		return nil, err
	}
	if err := space.Mem.WriteBytes(randomAddr, randomData); err != nil {
		return nil, err
	}
	auxv := []auxEntry{
		{3, phdr},
		{4, 32},
		{5, uint32(len(mainFile.Progs))},
		{6, pageSize},
		{9, mainEntry},
		{11, 0}, {12, 0}, {13, 0}, {14, 0},
		{25, randomAddr},
		{31, 0},
	}
	if interpFile != nil {
		auxv = append(auxv, auxEntry{7, interpreterBase})
	}
	stack, _, err := pushInitialStack(space.Mem, stackTop, argv, envp, auxv)
	if err != nil {
		return nil, err
	}
	space.EnableProtection()
	cpu := i386.NewCPU(space.Mem)
	cpu.EIP = start
	cpu.Regs[i386.ESP] = stack
	return &Image{
		Memory: space.Mem, Space: space, CPU: cpu, Entry: mainEntry, Start: start,
		Interpreter: interpName, InterpreterEntry: interpEntry, Stack: stack,
		Brk: uint32(mainBrk), Env: append([]string(nil), envp...),
	}, nil
}

func validateELF(f *elf.File, data []byte) error {
	if f.Class != elf.ELFCLASS32 || f.Data != elf.ELFDATA2LSB || f.Machine != elf.EM_386 {
		return fmt.Errorf("unsupported ELF class/data/machine: %v/%v/%v", f.Class, f.Data, f.Machine)
	}
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD && p.Type != elf.PT_INTERP {
			continue
		}
		if p.Type == elf.PT_LOAD && (p.Memsz < p.Filesz || p.Vaddr+p.Memsz > 1<<32 || p.Off+p.Filesz > uint64(len(data))) {
			return fmt.Errorf("invalid load segment at 0x%x", p.Vaddr)
		}
		if p.Type == elf.PT_INTERP && p.Off+p.Filesz > uint64(len(data)) {
			return fmt.Errorf("invalid PT_INTERP")
		}
	}
	return nil
}

func imageBounds(f *elf.File, data []byte, base uint32) (uint64, uint64, error) {
	var max, brk uint64
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		end := uint64(base) + p.Vaddr + p.Memsz
		if end > max {
			max = end
		}
		if p.Flags&elf.PF_W != 0 && end > brk {
			brk = end
		}
	}
	if max == 0 {
		return 0, 0, fmt.Errorf("ELF contains no loadable segments")
	}
	if brk == 0 {
		brk = max
	}
	return uint64(pageAlignUp(uint32(max))), uint64(pageAlignUp(uint32(brk))), nil
}

func loadSegments(space *i386.AddressSpace, f *elf.File, data []byte, base uint32, label string) error {
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		start64 := uint64(base) + p.Vaddr
		end64 := start64 + p.Memsz
		if end64 > 1<<32 {
			return fmt.Errorf("segment exceeds guest address space")
		}
		start := uint32(start64) &^ (pageSize - 1)
		end := pageAlignUp(uint32(end64))
		prot := uint8(0)
		if p.Flags&elf.PF_R != 0 {
			prot |= i386.ProtRead
		}
		if p.Flags&elf.PF_W != 0 {
			prot |= i386.ProtWrite
		}
		if p.Flags&elf.PF_X != 0 {
			prot |= i386.ProtExec
		}
		if _, ok := space.MappingAt(start); !ok {
			if err := space.Add(start, end-start, prot, label); err != nil {
				return err
			}
		}
		if p.Filesz != 0 {
			addr := uint32(start64)
			if err := space.Mem.WriteBytes(addr, data[p.Off:p.Off+p.Filesz]); err != nil {
				return err
			}
		}
	}
	return nil
}

func pageAlignUp(v uint32) uint32 {
	if v > ^uint32(0)-(pageSize-1) {
		return ^uint32(0)
	}
	return (v + pageSize - 1) &^ (pageSize - 1)
}

func programHeaderAddress(f *elf.File, data []byte, base uint32) uint32 {
	if len(data) < 32 {
		return base
	}
	phoff := uint64(binary.LittleEndian.Uint32(data[28:32]))
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD && phoff >= p.Off && phoff < p.Off+p.Filesz {
			return base + uint32(p.Vaddr+(phoff-p.Off))
		}
	}
	return base + uint32(phoff)
}

func interpreterPath(f *elf.File) (string, error) {
	for _, p := range f.Progs {
		if p.Type != elf.PT_INTERP {
			continue
		}
		data, err := io.ReadAll(p.Open())
		if err != nil {
			return "", fmt.Errorf("read PT_INTERP: %w", err)
		}
		return strings.TrimRight(string(data), string([]byte{0, '\n'})), nil
	}
	return "", nil
}

func pushArgv(mem *i386.Memory, top uint32, argv []string) (uint32, error) {
	stack, _, err := pushInitialStack(mem, top, argv, nil, nil)
	return stack, err
}

func pushInitialStack(mem *i386.Memory, top uint32, argv, envp []string, auxv []auxEntry) (uint32, uint32, error) {
	if len(argv) == 0 {
		argv = []string{"ish-go"}
	}
	argvPtrs, err := pushStrings(mem, top, argv)
	if err != nil {
		return 0, 0, err
	}
	sp := top
	for _, ptr := range argvPtrs {
		if ptr < sp {
			sp = ptr
		}
	}
	envPtrs, err := pushStrings(mem, sp, envp)
	if err != nil {
		return 0, 0, err
	}
	for _, ptr := range envPtrs {
		if ptr < sp {
			sp = ptr
		}
	}
	if len(argvPtrs) > 0 {
		for i := range auxv {
			if auxv[i].key == 31 {
				auxv[i].val = argvPtrs[0]
			}
		}
	}
	sp &^= 15
	words := 1 + len(argvPtrs) + 1 + len(envPtrs) + 1 + 2*(len(auxv)+1)
	sp -= uint32(words * 4)
	cursor := sp
	if err := mem.Write32(cursor, uint32(len(argvPtrs))); err != nil {
		return 0, 0, err
	}
	cursor += 4
	for _, ptr := range argvPtrs {
		if err := mem.Write32(cursor, ptr); err != nil {
			return 0, 0, err
		}
		cursor += 4
	}
	if err := mem.Write32(cursor, 0); err != nil {
		return 0, 0, err
	}
	cursor += 4
	for _, ptr := range envPtrs {
		if err := mem.Write32(cursor, ptr); err != nil {
			return 0, 0, err
		}
		cursor += 4
	}
	if err := mem.Write32(cursor, 0); err != nil {
		return 0, 0, err
	}
	cursor += 4
	for _, entry := range auxv {
		if err := mem.Write32(cursor, entry.key); err != nil {
			return 0, 0, err
		}
		if err := mem.Write32(cursor+4, entry.val); err != nil {
			return 0, 0, err
		}
		cursor += 8
	}
	if err := mem.Write32(cursor, 0); err != nil {
		return 0, 0, err
	}
	if err := mem.Write32(cursor+4, 0); err != nil {
		return 0, 0, err
	}
	return sp, 0, nil
}

func pushStrings(mem *i386.Memory, top uint32, values []string) ([]uint32, error) {
	ptrs := make([]uint32, len(values))
	sp := top
	for i := len(values) - 1; i >= 0; i-- {
		b := append([]byte(values[i]), 0)
		if uint32(len(b)) > sp {
			return nil, fmt.Errorf("initial stack exceeds guest memory")
		}
		sp -= uint32(len(b))
		if err := mem.WriteBytes(sp, b); err != nil {
			return nil, err
		}
		ptrs[i] = sp
	}
	return ptrs, nil
}

type byteReader struct {
	data []byte
	pos  int64
}

func bytesReader(data []byte) *byteReader { return &byteReader{data: data} }
func (r *byteReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(r.data)) {
		return 0, os.ErrInvalid
	}
	n := copy(p, r.data[off:])
	if n != len(p) {
		return n, os.ErrInvalid
	}
	return n, nil
}
func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += int64(n)
	return n, nil
}
func (r *byteReader) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case 0:
		next = offset
	case 1:
		next = r.pos + offset
	case 2:
		next = int64(len(r.data)) + offset
	default:
		return 0, os.ErrInvalid
	}
	if next < 0 {
		return 0, os.ErrInvalid
	}
	r.pos = next
	return next, nil
}

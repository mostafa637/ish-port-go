# i386 instruction units

Each subdirectory groups one instruction family.

- `data`: MOV and register/data transfer forms.
- `arithmetic`: ADD, SUB, MUL, DIV, INC, DEC, CMP.
- `logic`: boolean, shifts, and bit operations.
- `control`: calls, jumps, returns, and HLT.
- `stack`: PUSH and POP forms.
- `strings`: MOVS and STOS forms.
- `flags`: flag and direction operations.
- `x87`: floating-point subset.

The decoder remains in `internal/i386/cpu.go`; these units are the stable per-operation catalog.

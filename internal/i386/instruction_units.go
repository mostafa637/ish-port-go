package i386

import (
	"example.com/ish-go/internal/instructions/arithmetic"
	"example.com/ish-go/internal/instructions/control"
	"example.com/ish-go/internal/instructions/data"
	"example.com/ish-go/internal/instructions/logic"
	"example.com/ish-go/internal/instructions/stack"
)

const (
	_ = arithmetic.OpcodeAdd
	_ = control.OpcodeCall
	_ = data.OpcodeMovImm8
	_ = logic.OpcodeAnd
	_ = stack.OpcodePushImm
)

// Assembly dot-product kernel. Processes 16 floats per iteration using four
// 128-bit FMA accumulators, then reduces and handles a scalar tail.
//
// func dotFMA(a, b []float32) float32
#include "textflag.h"

TEXT ·dotFMA(SB), NOSPLIT, $0-52
    MOVQ a_base+0(FP), DI
    MOVQ b_base+24(FP), SI
    MOVQ a_len+8(FP), CX
    VXORPS X0, X0, X0
    VXORPS X1, X1, X1
    VXORPS X2, X2, X2
    VXORPS X3, X3, X3
    MOVQ CX, R10
    ANDQ $15, R10
    CMPQ CX, $16
    JL   tail_scan
    MOVQ CX, R8
    SHRQ $4, R8
    SHLQ $4, R8
vec_loop:
    VMOVUPS (DI), X4
    VMOVUPS (SI), X8
    VFMADD231PS X4, X8, X0
    VMOVUPS 16(DI), X5
    VMOVUPS 16(SI), X9
    VFMADD231PS X5, X9, X1
    VMOVUPS 32(DI), X6
    VMOVUPS 32(SI), X10
    VFMADD231PS X6, X10, X2
    VMOVUPS 48(DI), X7
    VMOVUPS 48(SI), X11
    VFMADD231PS X7, X11, X3
    ADDQ $64, DI
    ADDQ $64, SI
    SUBQ $16, R8
    JG   vec_loop
    VADDPS X0, X1, X0
    VADDPS X2, X3, X2
    VADDPS X0, X2, X0
    VPSHUFD $0x4E, X0, X1
    VADDPS X0, X1, X0
    VPSHUFD $0x11, X0, X1
    VADDPS X0, X1, X0
tail_scan:
    CMPQ R10, $0
    JLE  tail_done
    MOVSS (DI), X1
    MOVSS (SI), X2
    VMULSS X1, X2, X2
    VADDSS X0, X2, X0
    ADDQ $4, DI
    ADDQ $4, SI
    DECQ R10
    JMP  tail_scan
tail_done:
    MOVSS X0, ret+48(FP)
    RET

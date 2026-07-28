package vector

import (
	"math"

	"golang.org/x/sys/cpu"
)

// useFMA reports whether the CPU can run the AVX2+FMA dot-product kernel.
// golang.org/x/sys/cpu is zero-valued on non-amd64, so this is false there
// and the scalar fallback is used instead.
var useFMA = cpu.X86.HasAVX2 && cpu.X86.HasFMA

// dotProd returns the dot product of two equal-length float32 slices. On
// AVX2+FMA capable CPUs it dispatches to an assembly kernel (dotAVX2);
// otherwise it falls back to the scalar loop, which gc already auto-
// vectorizes to SSE/AVX.
func dotProd(a, b []float32) float32 {
	if useFMA && len(a) >= 32 {
		return dotFMA(a, b)
	}
	return dotScalar(a, b)
}

// dotScalar is the portable fallback. gc recognizes the s += a[i]*b[i]
// idiom and emits packed SIMD multiplies, so it is already fast.
func dotScalar(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// invNorm returns 1/|v| (precomputed denominator for cosine similarity).
func invNorm(v []float32) float32 {
	var sum float32
	for _, x := range v {
		sum += x * x
	}
	if sum <= 0 {
		return 0
	}
	return 1 / float32(math.Sqrt(float64(sum)))
}

// dotFMA is implemented in dot_amd64.s. It processes 32 floats per
// iteration using four 256-bit FMA accumulators and a scalar tail.
func dotFMA(a, b []float32) float32

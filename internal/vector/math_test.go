package vector

import (
	"math/rand"
	"testing"
)

// naiveDot is the reference implementation used to verify dotProd's result.
func naiveDot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func TestDotProdMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	dims := []int{1, 2, 3, 7, 8, 9, 15, 16, 17, 31, 32, 33, 100, 1536, 3072}
	for _, n := range dims {
		a := make([]float32, n)
		b := make([]float32, n)
		for i := range a {
			a[i] = rng.Float32()*2 - 1
			b[i] = rng.Float32()*2 - 1
		}
		got := dotProd(a, b)
		want := naiveDot(a, b)
		// float32 accumulation order differs, so allow a small epsilon.
		if diff := got - want; diff > 1e-2 || diff < -1e-2 {
			t.Errorf("dim=%d: dotProd=%v naive=%v diff=%v", n, got, want, diff)
		}
	}
}

func BenchmarkDotProd1536(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	a := make([]float32, 1536)
	v := make([]float32, 1536)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
		v[i] = rng.Float32()*2 - 1
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dotProd(a, v)
	}
}

func BenchmarkDotProdNaive1536(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	a := make([]float32, 1536)
	v := make([]float32, 1536)
	for i := range a {
		a[i] = rng.Float32()*2 - 1
		v[i] = rng.Float32()*2 - 1
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = naiveDot(a, v)
	}
}

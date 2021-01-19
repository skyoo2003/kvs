package bitset

import (
	"math"
	"math/rand"
	"testing"
)

func TestTest(t *testing.T) {
	bs := New()
	bs.v.SetInt64(2) // bin: 10
	if !bs.Test(1) {
		t.Error("Test(1): Invalid operation", bs.Test(1))
	}
}

func TestSet(t *testing.T) {
	bs := New()
	bs.Set(1)
	if !bs.Test(1) {
		t.Error("Test(1): Invalid operation", bs.Test(1))
	}
}

func TestReset(t *testing.T) {
	bs := New()
	bs.v.SetInt64(2) // bin: 10
	bs.Reset(1)      // bin: 00
	if bs.Test(1) {
		t.Error("Test(1): Invalid operation", bs.Test(1))
	}
}

func TestFlip(t *testing.T) {
	bs := New()
	bs.v.SetInt64(3) // bin: 11
	bs.Flip(0)
	if !bs.Test(1) {
		t.Error("Test(1): Invalid operation", bs.Test(1))
	}
	if bs.Test(0) {
		t.Error("Test(0): Invalid operation", bs.Test(0))
	}
}

func TestAll(t *testing.T) {
	bs := New()
	bs.v.SetInt64(3) // bin: 11
	if !bs.All() {
		t.Error("All(): Invalid operation", bs.All())
	}

	bs.v.SetInt64(2) // bin: 10
	if bs.All() {
		t.Error("All(): Invalid operation", bs.All())
	}
}

func TestAny(t *testing.T) {
	bs := New()
	bs.v.SetInt64(2) // bin: 10
	if !bs.Any() {
		t.Error("Any(): Invalid operation", bs.Any())
	}

	bs.v.SetInt64(0) // bin: 00
	if bs.Any() {
		t.Error("Any(): Invalid operation", bs.Any())
	}
}

func TestCount(t *testing.T) {
	bs := New()
	bs.v.SetInt64(3) // bin: 11
	if bs.Count() != 2 {
		t.Error("Count(): Invalid operation", bs.Count())
	}
}

func TestSize(t *testing.T) {
	bs := New()
	bs.v.SetInt64(4) // bin: 100
	if bs.Size() != 3 {
		t.Error("Size(): Invalid operation", bs.Size())
	}
}

func TestAnd(t *testing.T) {
	bs1, bs2 := New(), New()
	bs1.v.SetInt64(3) // bin: 11
	bs2.v.SetInt64(1) // bin: 01
	bs1.And(bs2)
	if bs1.Test(1) {
		t.Error("Test(1): Invalid operation", bs1.Test(1))
	}
	if !bs1.Test(0) {
		t.Error("Test(0): Invalid operation", bs1.Test(0))
	}
}

func TestOr(t *testing.T) {
	bs1, bs2 := New(), New()
	bs1.v.SetInt64(2) // bin: 10
	bs2.v.SetInt64(1) // bin: 01
	bs1.Or(bs2)
	if !bs1.Test(1) {
		t.Error("Test(1): Invalid operation", bs1.Test(1))
	}
	if !bs1.Test(0) {
		t.Error("Test(0): Invalid operation", bs1.Test(0))
	}
}

func TestXor(t *testing.T) {
	bs1, bs2 := New(), New()
	bs1.v.SetInt64(2) // bin: 10
	bs2.v.SetInt64(1) // bin: 01
	bs1.Xor(bs2)
	if !bs1.Test(1) {
		t.Error("Test(1): Invalid operation", bs1.Test(1))
	}
	if !bs1.Test(0) {
		t.Error("Test(0): Invalid operation", bs1.Test(0))
	}
}

func TestNot(t *testing.T) {
	bs := New()
	bs.v.SetInt64(2) // bin: 10
	bs.Not()
	if bs.Test(1) {
		t.Error("Test(1): Invalid operation", bs.Test(1))
	}
	if !bs.Test(0) {
		t.Error("Test(0): Invalid operation", bs.Test(0))
	}
}

func TestLshift(t *testing.T) {
	bs := New()
	bs.v.SetInt64(2) // bin: 10
	bs.Lshift(1)
	if !bs.Test(2) {
		t.Error("Test(2): Invalid operation", bs.Test(2))
	}
	if bs.Test(1) {
		t.Error("Test(1): Invalid operation", bs.Test(1))
	}
	if bs.Test(0) {
		t.Error("Test(0): Invalid operation", bs.Test(0))
	}
}

func TestRshift(t *testing.T) {
	bs := New()
	bs.v.SetInt64(4) // bin: 100
	bs.Rshift(1)
	if bs.Test(2) {
		t.Error("Test(2): Invalid operation", bs.Test(2))
	}
	if !bs.Test(1) {
		t.Error("Test(1): Invalid operation", bs.Test(1))
	}
	if bs.Test(0) {
		t.Error("Test(0): Invalid operation", bs.Test(0))
	}
}

func BenchmarkSet(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) //nolint:gosec // this is not used in a secure application
	bs := New()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
}

func BenchmarkTest(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) //nolint:gosec // this is not used in a secure application
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Test(b)
	}
}

func BenchmarkFlip(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Flip(b)
	}
}

func BenchmarkAll(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs.All()
	}
}

func BenchmarkAny(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs.Any()
	}
}

func BenchmarkCount(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs.Count()
	}
}

func BenchmarkSize(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs.Size()
	}
}

func BenchmarkAnd(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs1, bs2 := New(), New()
	for i := 0; i < b.N; i++ {
		b1 := uint(r.Intn(math.MaxInt32))
		bs1.Set(b1)
		b2 := uint(r.Intn(math.MaxInt32))
		bs2.Set(b2)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs1.And(bs2)
	}
}

func BenchmarkOr(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs1, bs2 := New(), New()
	for i := 0; i < b.N; i++ {
		b1 := uint(r.Intn(math.MaxInt32))
		bs1.Set(b1)
		b2 := uint(r.Intn(math.MaxInt32))
		bs2.Set(b2)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs1.Or(bs2)
	}
}

func BenchmarkXor(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs1, bs2 := New(), New()
	for i := 0; i < b.N; i++ {
		b1 := uint(r.Intn(math.MaxInt32))
		bs1.Set(b1)
		b2 := uint(r.Intn(math.MaxInt32))
		bs2.Set(b2)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs1.Xor(bs2)
	}
}

func BenchmarkNot(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs.Not()
	}
}

func BenchmarkLshift(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs.Lshift(4)
	}
}

func BenchmarkRshift(b *testing.B) {
	b.StopTimer()
	r := rand.New(rand.NewSource(7777)) // nolint
	bs := New()
	for i := 0; i < b.N; i++ {
		b := uint(r.Intn(math.MaxInt32))
		bs.Set(b)
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs.Rshift(4)
	}
}

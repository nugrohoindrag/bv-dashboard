package cache

import (
	"testing"
	"time"
)

func TestTTLMapExpiryAndSweep(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	m := New[string, int](time.Minute)
	m.now = func() time.Time { return now }
	m.Set("a", 1)
	if v, ok := m.Get("a"); !ok || v != 1 {
		t.Fatalf("get a: %v %v", v, ok)
	}
	now = now.Add(61 * time.Second)
	if _, ok := m.Get("a"); ok {
		t.Fatal("entri kedaluwarsa harus hilang")
	}
	// sweep oportunistik: isi > sweepEvery entri lalu majukan waktu → Set berikutnya menyapu
	for i := 0; i < sweepEvery-2; i++ {
		m.Set(string(rune('A'+i%26))+string(rune(i)), i)
	}
	before := m.Len()
	now = now.Add(2 * time.Minute)
	m.Set("z", 9)
	if m.Len() >= before {
		t.Fatalf("sweep harus mengurangi entri: before=%d after=%d", before, m.Len())
	}
	if _, ok := m.Get("z"); !ok {
		t.Fatal("entri baru tetap ada")
	}
}

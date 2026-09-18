// Package cache: peta in-memory ber-TTL untuk cache kecil per proses (principal/permission, trial guard, slug org).
//
// Berbeda dengan sync.Map polos, entri kedaluwarsa disapu secara oportunistik (setiap [sweepEvery] operasi Store)
// sehingga memori tidak tumbuh tanpa batas mengikuti user/org yang sudah dihapus (mis. reset demo berulang).
// Bukan pengganti Redis: cache ini per instance API dan hilang saat restart — dipakai hanya untuk data yang aman
// basi ≤ TTL dan punya invalidasi eksplisit (Delete) saat berubah.
package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

const sweepEvery = 256

type entry[V any] struct {
	v   V
	exp time.Time
}

// TTLMap: peta aman-goroutine dengan kedaluwarsa per entri.
type TTLMap[K comparable, V any] struct {
	ttl time.Duration
	m   sync.Map
	ops atomic.Uint32
	now func() time.Time
}

// New membuat TTLMap dengan masa hidup entri [ttl].
func New[K comparable, V any](ttl time.Duration) *TTLMap[K, V] {
	return &TTLMap[K, V]{ttl: ttl, now: time.Now}
}

// Get mengembalikan nilai bila ada dan belum kedaluwarsa.
func (t *TTLMap[K, V]) Get(k K) (V, bool) {
	var zero V
	v, ok := t.m.Load(k)
	if !ok {
		return zero, false
	}
	e := v.(entry[V])
	if t.now().After(e.exp) {
		t.m.Delete(k)
		return zero, false
	}
	return e.v, true
}

// Set menyimpan nilai dengan TTL default; setiap [sweepEvery] Set, entri kedaluwarsa disapu.
func (t *TTLMap[K, V]) Set(k K, v V) {
	t.m.Store(k, entry[V]{v: v, exp: t.now().Add(t.ttl)})
	if t.ops.Add(1)%sweepEvery == 0 {
		t.Sweep()
	}
}

// Delete menghapus satu entri (invalidasi eksplisit).
func (t *TTLMap[K, V]) Delete(k K) { t.m.Delete(k) }

// Sweep membuang semua entri kedaluwarsa; mengembalikan jumlah yang dihapus.
func (t *TTLMap[K, V]) Sweep() int {
	n := 0
	now := t.now()
	t.m.Range(func(k, v any) bool {
		if now.After(v.(entry[V]).exp) {
			t.m.Delete(k)
			n++
		}
		return true
	})
	return n
}

// Len menghitung entri (termasuk yang kedaluwarsa tapi belum disapu) — untuk metrik/test.
func (t *TTLMap[K, V]) Len() int {
	n := 0
	t.m.Range(func(_, _ any) bool { n++; return true })
	return n
}

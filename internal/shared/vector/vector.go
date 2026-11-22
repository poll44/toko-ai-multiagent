// Package vector: operasi vektor untuk pencarian semantik (pure Go).
// Penyimpanan default = BLOB float32 LE di SQLite + cosine brute-force (lihat Master PRD §9).
package vector

import (
	"context"
	"encoding/binary"
	"math"
	"sort"
)

// Normalize menormalkan L2 in-place (setelah ini cosine == dot product).
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// Dot: dot product (== cosine bila kedua vektor sudah dinormalisasi). Panjang berbeda => 0.
func Dot(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// Encode: []float32 -> BLOB little-endian.
func Encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}

// Decode: BLOB little-endian -> []float32 (nil bila panjang tidak kelipatan 4).
func Decode(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// Item: satu entri vektor terindeks.
type Item struct {
	ID  string
	Vec []float32
}

// Scored: hasil pencarian.
type Scored struct {
	ID    string
	Score float32
}

// TopK mengembalikan k entri dengan skor tertinggi (>= minScore), terurut menurun.
// keep (opsional) menyaring ID sebelum diskor (mis. hanya produk aktif).
func TopK(query []float32, items []Item, k int, minScore float32, keep func(id string) bool) []Scored {
	res := make([]Scored, 0, len(items))
	for _, it := range items {
		if keep != nil && !keep(it.ID) {
			continue
		}
		if s := Dot(query, it.Vec); s >= minScore {
			res = append(res, Scored{ID: it.ID, Score: s})
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Score > res[j].Score })
	if k > 0 && len(res) > k {
		res = res[:k]
	}
	return res
}

// Store: abstraksi penyimpanan vektor agar bisa diganti (sqlite-vec via ncruces/WASM, Qdrant) tanpa
// mengubah handler/kontrak API. Implementasi v1: brute-force di atas tabel *_embeddings.
type Store interface {
	Upsert(ctx context.Context, id string, vec []float32) error
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, query []float32, k int, minScore float32, keep func(id string) bool) ([]Scored, error)
}

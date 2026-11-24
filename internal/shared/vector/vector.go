// Package vector: operasi vektor untuk pencarian semantik (pure Go).
// Penyimpanan default = BLOB float32 LE di SQLite + cosine brute-force (lihat Master TRD §9).
package vector // nama package: vector

import ( // package yang dipakai
	"context"         // context untuk interface Store
	"encoding/binary" // encode/decode byte little-endian
	"math"            // fungsi matematika
	"sort"            // pengurutan hasil
)

// Normalize menormalkan L2 in-place (setelah ini cosine == dot product).
func Normalize(v []float32) []float32 {
	var sum float64       // jumlah kuadrat
	for _, x := range v { // hitung jumlah kuadrat tiap elemen
		sum += float64(x) * float64(x)
	}
	if sum == 0 { // vektor nol: tidak bisa dinormalkan
		return v
	}
	inv := float32(1 / math.Sqrt(sum)) // 1 / panjang vektor
	for i := range v {                 // kalikan tiap elemen
		v[i] *= inv
	}
	return v
}

// Dot: dot product (== cosine bila kedua vektor sudah dinormalisasi). Panjang berbeda => 0.
func Dot(a, b []float32) float32 {
	if len(a) != len(b) { // dimensi berbeda
		return 0
	}
	var s float32      // akumulator hasil
	for i := range a { // jumlahkan hasil kali elemen berpasangan
		s += a[i] * b[i]
	}
	return s
}

// Encode: []float32 -> BLOB little-endian.
func Encode(v []float32) []byte {
	b := make([]byte, 4*len(v)) // 4 byte per float32
	for i, x := range v {       // tulis tiap elemen
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x)) // float -> bit -> byte
	}
	return b
}

// Decode: BLOB little-endian -> []float32 (nil bila panjang tidak kelipatan 4).
func Decode(b []byte) []float32 {
	if len(b)%4 != 0 { // data rusak
		return nil
	}
	v := make([]float32, len(b)/4) // slice hasil
	for i := range v {             // baca tiap 4 byte
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:])) // byte -> bit -> float
	}
	return v
}

// Item: satu entri vektor terindeks.
type Item struct {
	ID  string    // ID entri
	Vec []float32 // vektor
}

// Scored: hasil pencarian.
type Scored struct {
	ID    string  // ID entri
	Score float32 // skor kemiripan
}

// TopK mengembalikan k entri dengan skor tertinggi (>= minScore), terurut menurun.
// keep (opsional) menyaring ID sebelum diskor (mis. hanya produk aktif).
func TopK(query []float32, items []Item, k int, minScore float32, keep func(id string) bool) []Scored {
	res := make([]Scored, 0, len(items)) // penampung hasil
	for _, it := range items {           // evaluasi tiap item
		if keep != nil && !keep(it.ID) { // disaring keluar
			continue
		}
		if s := Dot(query, it.Vec); s >= minScore { // skor memenuhi ambang
			res = append(res, Scored{ID: it.ID, Score: s})
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Score > res[j].Score }) // urut skor menurun
	if k > 0 && len(res) > k {                                                  // batasi jumlah hasil
		res = res[:k]
	}
	return res
}

// Store: abstraksi penyimpanan vektor agar bisa diganti (sqlite-vec via ncruces/WASM, Qdrant) tanpa
// mengubah handler/kontrak API. Implementasi v1: brute-force di atas tabel *_embeddings.
type Store interface {
	Upsert(ctx context.Context, id string, vec []float32) error                                                        // simpan/perbarui vektor
	Delete(ctx context.Context, id string) error                                                                       // hapus vektor
	Search(ctx context.Context, query []float32, k int, minScore float32, keep func(id string) bool) ([]Scored, error) // cari k terdekat
}

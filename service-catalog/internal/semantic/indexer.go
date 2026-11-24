// Package semantic: indexing embedding produk + pencarian vektor (TRD 02 §5, §6.2).
// Penyimpanan v1 = BLOB di SQLite + cosine brute-force di Go (lihat Master TRD §9).
package semantic // nama package: semantic

import ( // package yang dipakai
	"context"       // context
	"crypto/sha256" // hash isi teks
	"encoding/hex"  // hash ke string heksadesimal
	"errors"        // error sentinel
	"fmt"           // format string
	"log/slog"      // logging terstruktur
	"sync"          // mutex
	"sync/atomic"   // flag atomik
	"time"          // ticker

	"toko-ai-microservices/internal/shared/embedding"      // klien embedding
	"toko-ai-microservices/internal/shared/vector"         // operasi vektor
	"toko-ai-microservices/service-catalog/internal/model" // tipe data
	"toko-ai-microservices/service-catalog/internal/store" // repository
)

// Embedder diimplementasi *embedding.Client (dan fake pada test).
type Embedder interface {
	Enabled() bool                                                                            // apakah embedding aktif
	Model() string                                                                            // nama model
	Dim() int                                                                                 // dimensi vektor
	Embed(ctx context.Context, text string, task embedding.Task) ([]float32, error)           // embed satu teks
	EmbedBatch(ctx context.Context, texts []string, task embedding.Task) ([][]float32, error) // embed banyak teks
}

var (
	// ErrFallback: embedding tak tersedia / belum ada vektor => pemanggil memakai keyword_fallback.
	ErrFallback = errors.New("semantic search unavailable, use keyword fallback")
	// ErrBusy: reindex sedang berjalan.
	ErrBusy = errors.New("reindex sedang berjalan")
)

// BuildText: teks yang di-embed (tanpa harga/stok yang volatil).
func BuildText(p model.Product) string {
	return fmt.Sprintf("Nama: %s\nKategori: %s\nDeskripsi: %s\nSpesifikasi: %s", p.Name, p.Category, p.Description, p.Specifications)
}

// hashText menghasilkan hash SHA-256 (hex) dari teks, untuk mendeteksi perubahan isi.
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s)) // hitung hash
	return hex.EncodeToString(sum[:])
}

// Indexer mengelola embedding produk dan pencarian vektor.
type Indexer struct {
	repo *store.Repo // akses database
	emb  Embedder    // penyedia embedding

	baseCtx    context.Context // context hidup service (untuk goroutine background)
	reindexing atomic.Bool     // penanda reindex sedang berjalan

	mu     sync.RWMutex  // melindungi cache vektor
	items  []vector.Item // cache vektor di memori
	loaded bool          // apakah cache sudah dimuat
}

// New membuat Indexer baru.
func New(repo *store.Repo, emb Embedder) *Indexer {
	return &Indexer{repo: repo, emb: emb, baseCtx: context.Background()}
}

// Enabled: apakah embedding aktif.
func (ix *Indexer) Enabled() bool { return ix.emb.Enabled() }

// invalidate menandai cache vektor kedaluwarsa agar dimuat ulang dari database.
func (ix *Indexer) invalidate() {
	ix.mu.Lock()
	ix.loaded = false
	ix.mu.Unlock()
}

// IndexProduct meng-embed satu produk (sinkron). Dilewati bila hash/model/dim sama.
func (ix *Indexer) IndexProduct(ctx context.Context, p model.Product) error {
	if !ix.emb.Enabled() { // embedding mati
		return embedding.ErrUnavailable
	}
	text := BuildText(p)                    // teks yang akan di-embed
	h := hashText(text)                     // hash teks
	meta, err := ix.repo.EmbeddingMeta(ctx) // metadata embedding yang ada
	if err != nil {
		return err
	}
	if m, ok := meta[p.ID]; ok && m.ContentHash == h && m.Model == ix.emb.Model() && m.Dim == ix.emb.Dim() { // sudah mutakhir
		return nil
	}
	vec, err := ix.emb.Embed(ctx, text, embedding.TaskDocument) // panggil API embedding
	if err != nil {
		return err
	}
	if err := ix.repo.UpsertEmbedding(ctx, p.ID, ix.emb.Model(), ix.emb.Dim(), h, vec); err != nil { // simpan vektor
		return err
	}
	ix.invalidate() // cache harus dimuat ulang
	return nil
}

// IndexPending meng-embed produk yang belum/tidak cocok indeksnya (batch). force=true => semua produk.
// max<=0 => tanpa batas. Mengembalikan jumlah yang di-embed.
func (ix *Indexer) IndexPending(ctx context.Context, force bool, max int) (int, error) {
	if !ix.emb.Enabled() { // embedding mati
		return 0, embedding.ErrUnavailable
	}
	products, err := ix.repo.AllProducts(ctx) // semua produk
	if err != nil {
		return 0, err
	}
	meta, err := ix.repo.EmbeddingMeta(ctx) // metadata embedding saat ini
	if err != nil {
		return 0, err
	}
	var todo []model.Product   // produk yang perlu di-embed
	var texts, hashes []string // teks & hash-nya
	for _, p := range products {
		text := BuildText(p)
		h := hashText(text)
		m, ok := meta[p.ID]
		if force || !ok || m.ContentHash != h || m.Model != ix.emb.Model() || m.Dim != ix.emb.Dim() { // perlu di-embed
			todo = append(todo, p)
			texts = append(texts, text)
			hashes = append(hashes, h)
			if max > 0 && len(todo) >= max { // batas jumlah per putaran
				break
			}
		}
	}
	if len(todo) == 0 { // tidak ada yang perlu dikerjakan
		return 0, nil
	}
	vecs, err := ix.emb.EmbedBatch(ctx, texts, embedding.TaskDocument) // embed sekaligus
	if err != nil {
		return 0, err
	}
	for i, p := range todo { // simpan tiap vektor
		if err := ix.repo.UpsertEmbedding(ctx, p.ID, ix.emb.Model(), ix.emb.Dim(), hashes[i], vecs[i]); err != nil {
			return i, err
		}
	}
	ix.invalidate() // cache harus dimuat ulang
	return len(todo), nil
}

// Start menjalankan indexing awal + worker retry periodik (goroutine; berhenti saat ctx batal).
func (ix *Indexer) Start(ctx context.Context, interval time.Duration) {
	ix.baseCtx = ctx       // simpan context hidup service
	if !ix.emb.Enabled() { // tanpa embedding: tidak ada worker
		return
	}
	go func() { // worker background
		run := func() { // satu putaran indexing
			n, err := ix.IndexPending(ctx, false, 20) // maks 20 produk per putaran
			switch {
			case err != nil:
				slog.Warn("indexing embedding produk gagal (akan dicoba lagi)", "err", err)
			case n > 0:
				slog.Info("embedding produk diperbarui", "count", n)
			}
		}
		run()                         // putaran awal
		t := time.NewTicker(interval) // jadwal putaran berikutnya
		defer t.Stop()
		for {
			select {
			case <-ctx.Done(): // service berhenti
				return
			case <-t.C: // waktunya putaran baru
				run()
			}
		}
	}()
}

// Reindex memaksa re-embed semua produk di background. Mengembalikan jumlah produk yang diantrekan.
func (ix *Indexer) Reindex(ctx context.Context) (int, error) {
	if !ix.emb.Enabled() { // embedding mati
		return 0, embedding.ErrUnavailable
	}
	if !ix.reindexing.CompareAndSwap(false, true) { // sudah ada reindex berjalan
		return 0, ErrBusy
	}
	products, err := ix.repo.AllProducts(ctx) // hitung produk yang diantrekan
	if err != nil {
		ix.reindexing.Store(false) // lepas penanda
		return 0, err
	}
	go func() { // jalankan di background
		defer ix.reindexing.Store(false)               // lepas penanda saat selesai
		n, err := ix.IndexPending(ix.baseCtx, true, 0) // paksa semua produk
		if err != nil {
			slog.Error("reindex gagal", "err", err)
			return
		}
		slog.Info("reindex selesai", "count", n)
	}()
	return len(products), nil
}

// vectors mengembalikan cache vektor (memuat dari database bila belum dimuat).
func (ix *Indexer) vectors(ctx context.Context) ([]vector.Item, error) {
	ix.mu.RLock()  // baca cepat
	if ix.loaded { // cache siap
		items := ix.items
		ix.mu.RUnlock()
		return items, nil
	}
	ix.mu.RUnlock()
	items, err := ix.repo.LoadEmbeddings(ctx, ix.emb.Model(), ix.emb.Dim()) // muat dari database
	if err != nil {
		return nil, err
	}
	ix.mu.Lock() // tulis cache
	ix.items, ix.loaded = items, true
	ix.mu.Unlock()
	return items, nil
}

// Search meng-embed query lalu mengambil top-k (skor >= minScore) di antara ID yang lolos keep.
// ErrFallback bila embedding mati/gagal atau belum ada vektor terindeks.
func (ix *Indexer) Search(ctx context.Context, q string, k int, minScore float32, keep func(id string) bool) ([]vector.Scored, error) {
	if !ix.emb.Enabled() { // embedding mati
		return nil, ErrFallback
	}
	items, err := ix.vectors(ctx) // vektor produk
	if err != nil {
		return nil, err
	}
	if len(items) == 0 { // belum ada vektor
		return nil, ErrFallback
	}
	qv, err := ix.emb.Embed(ctx, q, embedding.TaskQuery) // embed query
	if err != nil {
		slog.Warn("embedding query gagal, pakai keyword_fallback", "err", err)
		return nil, ErrFallback
	}
	return vector.TopK(qv, items, k, minScore, keep), nil // ambil top-k terdekat
}

package knowledge // indexing embedding & pencarian knowledge base

import ( // package yang dipakai
	"context"       // context
	"crypto/sha256" // hash teks
	"encoding/hex"  // hash ke string heksadesimal
	"errors"        // error sentinel
	"log/slog"      // logging terstruktur
	"sync"          // mutex
	"sync/atomic"   // flag atomik
	"time"          // ticker

	"toko-ai-microservices/internal/shared/embedding"     // klien embedding
	"toko-ai-microservices/internal/shared/vector"        // operasi vektor
	"toko-ai-microservices/service-ticket/internal/model" // tipe data
	"toko-ai-microservices/service-ticket/internal/store" // repository
)

// Embedder diimplementasi *embedding.Client (dan fake pada test).
type Embedder interface {
	Enabled() bool                                                                            // apakah embedding aktif
	Model() string                                                                            // nama model
	Dim() int                                                                                 // dimensi vektor
	Embed(ctx context.Context, text string, task embedding.Task) ([]float32, error)           // embed satu teks
	EmbedBatch(ctx context.Context, texts []string, task embedding.Task) ([][]float32, error) // embed banyak teks
}

// ErrBusy: reindex sedang berjalan.
var ErrBusy = errors.New("reindex sedang berjalan")

// EmbedText: teks yang di-embed untuk satu chunk = "{judul}\n{isi chunk}" (judul menjaga konteks).
func EmbedText(title, content string) string { return title + "\n" + content }

// Hash: content_hash dari teks embedding (dipakai untuk melewati re-embed yang tidak perlu).
func Hash(title, content string) string {
	sum := sha256.Sum256([]byte(EmbedText(title, content))) // hitung hash teks embedding
	return hex.EncodeToString(sum[:])
}

// Indexer mengelola embedding chunk dan pencarian knowledge base.
type Indexer struct {
	repo *store.Repo // akses database
	emb  Embedder    // penyedia embedding

	baseCtx    context.Context // context hidup service (untuk goroutine background)
	reindexing atomic.Bool     // penanda reindex sedang berjalan

	mu     sync.RWMutex  // melindungi cache vektor
	items  []vector.Item // cache vektor di memori
	loaded bool          // apakah cache sudah dimuat
}

// NewIndexer membuat Indexer baru.
func NewIndexer(repo *store.Repo, emb Embedder) *Indexer {
	return &Indexer{repo: repo, emb: emb, baseCtx: context.Background()}
}

// Enabled: apakah embedding aktif.
func (ix *Indexer) Enabled() bool { return ix.emb.Enabled() }

// invalidate menandai cache vektor kedaluwarsa agar dimuat ulang dari database.
func (ix *Indexer) invalidate() { ix.mu.Lock(); ix.loaded = false; ix.mu.Unlock() }

// IndexPending meng-embed chunk yang belum/tidak cocok indeksnya (batch). force=true => semua chunk. max<=0 => tanpa batas.
func (ix *Indexer) IndexPending(ctx context.Context, force bool, max int) (int, error) {
	if !ix.emb.Enabled() { // embedding mati
		return 0, embedding.ErrUnavailable
	}
	chunks, _, err := ix.repo.AllChunks(ctx) // semua chunk (dokumen aktif)
	if err != nil {
		return 0, err
	}
	meta, err := ix.repo.EmbeddingMeta(ctx) // metadata embedding saat ini
	if err != nil {
		return 0, err
	}
	var todo []model.Chunk     // chunk yang perlu di-embed
	var texts, hashes []string // teks & hash-nya
	for _, c := range chunks {
		h := Hash(c.Title, c.Content)
		m, ok := meta[c.ID]
		if force || !ok || m.ContentHash != h || m.Model != ix.emb.Model() || m.Dim != ix.emb.Dim() { // perlu di-embed
			todo = append(todo, c)
			texts = append(texts, EmbedText(c.Title, c.Content))
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
	for i, c := range todo { // simpan tiap vektor
		if err := ix.repo.UpsertEmbedding(ctx, c.ID, ix.emb.Model(), ix.emb.Dim(), hashes[i], vecs[i]); err != nil {
			return i, err
		}
	}
	ix.invalidate() // cache harus dimuat ulang
	return len(todo), nil
}

// Start: indexing awal + worker retry periodik (goroutine; berhenti saat ctx batal).
func (ix *Indexer) Start(ctx context.Context, interval time.Duration) {
	ix.baseCtx = ctx       // simpan context hidup service
	if !ix.emb.Enabled() { // tanpa embedding: tidak ada worker
		return
	}
	go func() { // worker background
		run := func() { // satu putaran indexing
			switch n, err := ix.IndexPending(ctx, false, 50); { // maks 50 chunk per putaran
			case err != nil:
				slog.Warn("indexing knowledge gagal (akan dicoba lagi)", "err", err)
			case n > 0:
				slog.Info("embedding knowledge diperbarui", "chunks", n)
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

// Reindex memaksa re-embed semua chunk di background; mengembalikan jumlah chunk yang diantrekan.
func (ix *Indexer) Reindex(ctx context.Context) (int, error) {
	if !ix.emb.Enabled() { // embedding mati
		return 0, embedding.ErrUnavailable
	}
	if !ix.reindexing.CompareAndSwap(false, true) { // sudah ada reindex berjalan
		return 0, ErrBusy
	}
	chunks, _, err := ix.repo.AllChunks(ctx) // hitung chunk yang diantrekan
	if err != nil {
		ix.reindexing.Store(false) // lepas penanda
		return 0, err
	}
	go func() { // jalankan di background
		defer ix.reindexing.Store(false)               // lepas penanda saat selesai
		n, err := ix.IndexPending(ix.baseCtx, true, 0) // paksa semua chunk
		if err != nil {
			slog.Error("reindex knowledge gagal", "err", err)
			return
		}
		slog.Info("reindex knowledge selesai", "chunks", n)
	}()
	return len(chunks), nil
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

// maxChunksPerDoc: batas chunk hasil per dokumen agar hasil beragam.
const maxChunksPerDoc = 2

// Search mencari chunk kebijakan paling relevan. Mode "semantic" (vektor) atau "keyword_fallback" (LIKE) bila
// embedding mati/gagal/belum terindeks. Maks 2 chunk per dokumen agar hasil beragam. Kosong => data [] (bukan error).
func (ix *Indexer) Search(ctx context.Context, q string, limit int, minScore float32, category string) ([]model.ChunkResult, string, error) {
	active, err := ix.repo.ActiveChunks(ctx) // chunk dari dokumen aktif
	if err != nil {
		return nil, "", err
	}
	if ix.emb.Enabled() { // coba pencarian semantik dulu
		if items, err := ix.vectors(ctx); err == nil && len(items) > 0 {
			if qv, err := ix.emb.Embed(ctx, q, embedding.TaskQuery); err == nil { // embed query
				keep := func(id string) bool { // penyaring kandidat
					c, ok := active[id]
					return ok && (category == "" || c.Category == category)
				}
				scored := vector.TopK(qv, items, limit*4, minScore, keep) // ambil kandidat lebih banyak
				perDoc := map[string]int{}                                // hitungan chunk per dokumen
				res := []model.ChunkResult{}                              // hasil non-nil
				for _, s := range scored {
					c := active[s.ID]
					if perDoc[c.DocumentID] >= maxChunksPerDoc { // dokumen ini sudah cukup
						continue
					}
					perDoc[c.DocumentID]++
					sc := roundScore(float64(s.Score)) // bulatkan skor
					res = append(res, toResult(c, &sc))
					if len(res) >= limit { // sudah cukup hasil
						break
					}
				}
				return res, "semantic", nil
			} else {
				slog.Warn("embedding query gagal, pakai keyword_fallback", "err", err)
			}
		}
	}
	chunks, err := ix.repo.KeywordChunks(ctx, q, category, limit) // fallback pencarian kata kunci
	if err != nil {
		return nil, "", err
	}
	res := make([]model.ChunkResult, 0, len(chunks))
	for _, c := range chunks {
		res = append(res, toResult(c, nil)) // tanpa skor
	}
	return res, "keyword_fallback", nil
}

// toResult mengubah model.Chunk menjadi model.ChunkResult.
func toResult(c model.Chunk, score *float64) model.ChunkResult {
	return model.ChunkResult{ChunkID: c.ID, DocumentID: c.DocumentID, Title: c.Title, Category: c.Category, Content: c.Content, Score: score}
}

// roundScore membulatkan skor ke 4 desimal.
func roundScore(f float64) float64 {
	return float64(int64(f*10000+0.5)) / 10000
}

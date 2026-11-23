// Package semantic: indexing embedding produk + pencarian vektor (TRD 02 §5, §6.2).
// Penyimpanan v1 = BLOB di SQLite + cosine brute-force di Go (lihat Master TRD §9).
package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"toko-ai-microservices/internal/shared/embedding"
	"toko-ai-microservices/internal/shared/vector"
	"toko-ai-microservices/service-catalog/internal/model"
	"toko-ai-microservices/service-catalog/internal/store"
)

// Embedder diimplementasi *embedding.Client (dan fake pada test).
type Embedder interface {
	Enabled() bool
	Model() string
	Dim() int
	Embed(ctx context.Context, text string, task embedding.Task) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string, task embedding.Task) ([][]float32, error)
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

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type Indexer struct {
	repo *store.Repo
	emb  Embedder

	baseCtx    context.Context
	reindexing atomic.Bool

	mu     sync.RWMutex
	items  []vector.Item
	loaded bool
}

func New(repo *store.Repo, emb Embedder) *Indexer {
	return &Indexer{repo: repo, emb: emb, baseCtx: context.Background()}
}

// Enabled: apakah embedding aktif.
func (ix *Indexer) Enabled() bool { return ix.emb.Enabled() }

func (ix *Indexer) invalidate() {
	ix.mu.Lock()
	ix.loaded = false
	ix.mu.Unlock()
}

// IndexProduct meng-embed satu produk (sinkron). Dilewati bila hash/model/dim sama.
func (ix *Indexer) IndexProduct(ctx context.Context, p model.Product) error {
	if !ix.emb.Enabled() {
		return embedding.ErrUnavailable
	}
	text := BuildText(p)
	h := hashText(text)
	meta, err := ix.repo.EmbeddingMeta(ctx)
	if err != nil {
		return err
	}
	if m, ok := meta[p.ID]; ok && m.ContentHash == h && m.Model == ix.emb.Model() && m.Dim == ix.emb.Dim() {
		return nil
	}
	vec, err := ix.emb.Embed(ctx, text, embedding.TaskDocument)
	if err != nil {
		return err
	}
	if err := ix.repo.UpsertEmbedding(ctx, p.ID, ix.emb.Model(), ix.emb.Dim(), h, vec); err != nil {
		return err
	}
	ix.invalidate()
	return nil
}

// IndexPending meng-embed produk yang belum/tidak cocok indeksnya (batch). force=true => semua produk.
// max<=0 => tanpa batas. Mengembalikan jumlah yang di-embed.
func (ix *Indexer) IndexPending(ctx context.Context, force bool, max int) (int, error) {
	if !ix.emb.Enabled() {
		return 0, embedding.ErrUnavailable
	}
	products, err := ix.repo.AllProducts(ctx)
	if err != nil {
		return 0, err
	}
	meta, err := ix.repo.EmbeddingMeta(ctx)
	if err != nil {
		return 0, err
	}
	var todo []model.Product
	var texts, hashes []string
	for _, p := range products {
		text := BuildText(p)
		h := hashText(text)
		m, ok := meta[p.ID]
		if force || !ok || m.ContentHash != h || m.Model != ix.emb.Model() || m.Dim != ix.emb.Dim() {
			todo = append(todo, p)
			texts = append(texts, text)
			hashes = append(hashes, h)
			if max > 0 && len(todo) >= max {
				break
			}
		}
	}
	if len(todo) == 0 {
		return 0, nil
	}
	vecs, err := ix.emb.EmbedBatch(ctx, texts, embedding.TaskDocument)
	if err != nil {
		return 0, err
	}
	for i, p := range todo {
		if err := ix.repo.UpsertEmbedding(ctx, p.ID, ix.emb.Model(), ix.emb.Dim(), hashes[i], vecs[i]); err != nil {
			return i, err
		}
	}
	ix.invalidate()
	return len(todo), nil
}

// Start menjalankan indexing awal + worker retry periodik (goroutine; berhenti saat ctx batal).
func (ix *Indexer) Start(ctx context.Context, interval time.Duration) {
	ix.baseCtx = ctx
	if !ix.emb.Enabled() {
		return
	}
	go func() {
		run := func() {
			n, err := ix.IndexPending(ctx, false, 20)
			switch {
			case err != nil:
				slog.Warn("indexing embedding produk gagal (akan dicoba lagi)", "err", err)
			case n > 0:
				slog.Info("embedding produk diperbarui", "count", n)
			}
		}
		run()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}

// Reindex memaksa re-embed semua produk di background. Mengembalikan jumlah produk yang diantrekan.
func (ix *Indexer) Reindex(ctx context.Context) (int, error) {
	if !ix.emb.Enabled() {
		return 0, embedding.ErrUnavailable
	}
	if !ix.reindexing.CompareAndSwap(false, true) {
		return 0, ErrBusy
	}
	products, err := ix.repo.AllProducts(ctx)
	if err != nil {
		ix.reindexing.Store(false)
		return 0, err
	}
	go func() {
		defer ix.reindexing.Store(false)
		n, err := ix.IndexPending(ix.baseCtx, true, 0)
		if err != nil {
			slog.Error("reindex gagal", "err", err)
			return
		}
		slog.Info("reindex selesai", "count", n)
	}()
	return len(products), nil
}

func (ix *Indexer) vectors(ctx context.Context) ([]vector.Item, error) {
	ix.mu.RLock()
	if ix.loaded {
		items := ix.items
		ix.mu.RUnlock()
		return items, nil
	}
	ix.mu.RUnlock()
	items, err := ix.repo.LoadEmbeddings(ctx, ix.emb.Model(), ix.emb.Dim())
	if err != nil {
		return nil, err
	}
	ix.mu.Lock()
	ix.items, ix.loaded = items, true
	ix.mu.Unlock()
	return items, nil
}

// Search meng-embed query lalu mengambil top-k (skor >= minScore) di antara ID yang lolos keep.
// ErrFallback bila embedding mati/gagal atau belum ada vektor terindeks.
func (ix *Indexer) Search(ctx context.Context, q string, k int, minScore float32, keep func(id string) bool) ([]vector.Scored, error) {
	if !ix.emb.Enabled() {
		return nil, ErrFallback
	}
	items, err := ix.vectors(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrFallback
	}
	qv, err := ix.emb.Embed(ctx, q, embedding.TaskQuery)
	if err != nil {
		slog.Warn("embedding query gagal, pakai keyword_fallback", "err", err)
		return nil, ErrFallback
	}
	return vector.TopK(qv, items, k, minScore, keep), nil
}

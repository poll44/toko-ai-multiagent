package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"toko-ai-microservices/internal/shared/embedding"
	"toko-ai-microservices/internal/shared/vector"
	"toko-ai-microservices/service-ticket/internal/model"
	"toko-ai-microservices/service-ticket/internal/store"
)

// Embedder diimplementasi *embedding.Client (dan fake pada test).
type Embedder interface {
	Enabled() bool
	Model() string
	Dim() int
	Embed(ctx context.Context, text string, task embedding.Task) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string, task embedding.Task) ([][]float32, error)
}

var ErrBusy = errors.New("reindex sedang berjalan")

// EmbedText: teks yang di-embed untuk satu chunk = "{judul}\n{isi chunk}" (judul menjaga konteks).
func EmbedText(title, content string) string { return title + "\n" + content }

// Hash: content_hash dari teks embedding (dipakai untuk melewati re-embed yang tidak perlu).
func Hash(title, content string) string {
	sum := sha256.Sum256([]byte(EmbedText(title, content)))
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

func NewIndexer(repo *store.Repo, emb Embedder) *Indexer {
	return &Indexer{repo: repo, emb: emb, baseCtx: context.Background()}
}

func (ix *Indexer) Enabled() bool { return ix.emb.Enabled() }

func (ix *Indexer) invalidate() { ix.mu.Lock(); ix.loaded = false; ix.mu.Unlock() }

// IndexPending meng-embed chunk yang belum/tidak cocok indeksnya (batch). force=true => semua chunk. max<=0 => tanpa batas.
func (ix *Indexer) IndexPending(ctx context.Context, force bool, max int) (int, error) {
	if !ix.emb.Enabled() {
		return 0, embedding.ErrUnavailable
	}
	chunks, _, err := ix.repo.AllChunks(ctx)
	if err != nil {
		return 0, err
	}
	meta, err := ix.repo.EmbeddingMeta(ctx)
	if err != nil {
		return 0, err
	}
	var todo []model.Chunk
	var texts, hashes []string
	for _, c := range chunks {
		h := Hash(c.Title, c.Content)
		m, ok := meta[c.ID]
		if force || !ok || m.ContentHash != h || m.Model != ix.emb.Model() || m.Dim != ix.emb.Dim() {
			todo = append(todo, c)
			texts = append(texts, EmbedText(c.Title, c.Content))
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
	for i, c := range todo {
		if err := ix.repo.UpsertEmbedding(ctx, c.ID, ix.emb.Model(), ix.emb.Dim(), hashes[i], vecs[i]); err != nil {
			return i, err
		}
	}
	ix.invalidate()
	return len(todo), nil
}

// Start: indexing awal + worker retry periodik (goroutine; berhenti saat ctx batal).
func (ix *Indexer) Start(ctx context.Context, interval time.Duration) {
	ix.baseCtx = ctx
	if !ix.emb.Enabled() {
		return
	}
	go func() {
		run := func() {
			switch n, err := ix.IndexPending(ctx, false, 50); {
			case err != nil:
				slog.Warn("indexing knowledge gagal (akan dicoba lagi)", "err", err)
			case n > 0:
				slog.Info("embedding knowledge diperbarui", "chunks", n)
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

// Reindex memaksa re-embed semua chunk di background; mengembalikan jumlah chunk yang diantrekan.
func (ix *Indexer) Reindex(ctx context.Context) (int, error) {
	if !ix.emb.Enabled() {
		return 0, embedding.ErrUnavailable
	}
	if !ix.reindexing.CompareAndSwap(false, true) {
		return 0, ErrBusy
	}
	chunks, _, err := ix.repo.AllChunks(ctx)
	if err != nil {
		ix.reindexing.Store(false)
		return 0, err
	}
	go func() {
		defer ix.reindexing.Store(false)
		n, err := ix.IndexPending(ix.baseCtx, true, 0)
		if err != nil {
			slog.Error("reindex knowledge gagal", "err", err)
			return
		}
		slog.Info("reindex knowledge selesai", "chunks", n)
	}()
	return len(chunks), nil
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

const maxChunksPerDoc = 2

// Search mencari chunk kebijakan paling relevan. Mode "semantic" (vektor) atau "keyword_fallback" (LIKE) bila
// embedding mati/gagal/belum terindeks. Maks 2 chunk per dokumen agar hasil beragam. Kosong => data [] (bukan error).
func (ix *Indexer) Search(ctx context.Context, q string, limit int, minScore float32, category string) ([]model.ChunkResult, string, error) {
	active, err := ix.repo.ActiveChunks(ctx)
	if err != nil {
		return nil, "", err
	}
	if ix.emb.Enabled() {
		if items, err := ix.vectors(ctx); err == nil && len(items) > 0 {
			if qv, err := ix.emb.Embed(ctx, q, embedding.TaskQuery); err == nil {
				keep := func(id string) bool {
					c, ok := active[id]
					return ok && (category == "" || c.Category == category)
				}
				scored := vector.TopK(qv, items, limit*4, minScore, keep)
				perDoc := map[string]int{}
				res := []model.ChunkResult{}
				for _, s := range scored {
					c := active[s.ID]
					if perDoc[c.DocumentID] >= maxChunksPerDoc {
						continue
					}
					perDoc[c.DocumentID]++
					sc := roundScore(float64(s.Score))
					res = append(res, toResult(c, &sc))
					if len(res) >= limit {
						break
					}
				}
				return res, "semantic", nil
			} else {
				slog.Warn("embedding query gagal, pakai keyword_fallback", "err", err)
			}
		}
	}
	chunks, err := ix.repo.KeywordChunks(ctx, q, category, limit)
	if err != nil {
		return nil, "", err
	}
	res := make([]model.ChunkResult, 0, len(chunks))
	for _, c := range chunks {
		res = append(res, toResult(c, nil))
	}
	return res, "keyword_fallback", nil
}

func toResult(c model.Chunk, score *float64) model.ChunkResult {
	return model.ChunkResult{ChunkID: c.ID, DocumentID: c.DocumentID, Title: c.Title, Category: c.Category, Content: c.Content, Score: score}
}

func roundScore(f float64) float64 {
	return float64(int64(f*10000+0.5)) / 10000
}

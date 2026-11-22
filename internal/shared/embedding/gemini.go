// Package embedding: klien Gemini Embedding API (REST) + cache LRU untuk vektor query.
package embedding

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"toko-ai-microservices/internal/shared/vector"
)

type Task string

const (
	TaskDocument Task = "RETRIEVAL_DOCUMENT" // saat indexing
	TaskQuery    Task = "RETRIEVAL_QUERY"    // saat query pengguna
)

// ErrUnavailable: embedding tidak tersedia (tanpa API key / jaringan / kuota). Pemanggil
// harus jatuh ke pencarian keyword (meta.mode = "keyword_fallback").
var ErrUnavailable = errors.New("embedding unavailable")

const (
	defaultBase = "https://generativelanguage.googleapis.com/v1beta"
	batchSize   = 100
	cacheSize   = 256
)

type Client struct {
	apiKey string
	model  string
	dim    int
	base   string
	hc     *http.Client
	cache  *lru
}

// New membuat klien. apiKey kosong => Enabled()==false (semua panggilan mengembalikan ErrUnavailable).
func New(apiKey, model string, dim int, timeout time.Duration) *Client {
	return &Client{
		apiKey: apiKey, model: model, dim: dim, base: defaultBase,
		hc: &http.Client{Timeout: timeout}, cache: newLRU(cacheSize),
	}
}

func (c *Client) Enabled() bool { return c != nil && c.apiKey != "" }
func (c *Client) Model() string { return c.model }
func (c *Client) Dim() int      { return c.dim }

type part struct {
	Text string `json:"text"`
}
type content struct {
	Parts []part `json:"parts"`
}
type embedReq struct {
	Model                string  `json:"model"`
	Content              content `json:"content"`
	TaskType             Task    `json:"taskType"`
	OutputDimensionality int     `json:"outputDimensionality,omitempty"`
}
type embedVals struct {
	Values []float32 `json:"values"`
}

func (c *Client) req(text string, task Task) embedReq {
	r := embedReq{Model: "models/" + c.model, Content: content{Parts: []part{{Text: text}}}, TaskType: task}
	if c.dim > 0 && strings.HasPrefix(c.model, "gemini-embedding") {
		r.OutputDimensionality = c.dim
	}
	return r
}

// Embed menghasilkan vektor ter-normalisasi L2. Vektor query (TaskQuery) di-cache.
func (c *Client) Embed(ctx context.Context, text string, task Task) ([]float32, error) {
	if !c.Enabled() {
		return nil, ErrUnavailable
	}
	key := string(task) + "|" + strings.ToLower(strings.TrimSpace(text))
	if task == TaskQuery {
		if v, ok := c.cache.get(key); ok {
			return v, nil
		}
	}
	var out struct {
		Embedding embedVals `json:"embedding"`
	}
	if err := c.post(ctx, "/models/"+c.model+":embedContent", c.req(text, task), &out); err != nil {
		return nil, err
	}
	v, err := c.finish(out.Embedding.Values)
	if err != nil {
		return nil, err
	}
	if task == TaskQuery {
		c.cache.put(key, v)
	}
	return v, nil
}

// EmbedBatch meng-embed banyak teks (dipecah per 100) dengan urutan hasil = urutan input.
func (c *Client) EmbedBatch(ctx context.Context, texts []string, task Task) ([][]float32, error) {
	if !c.Enabled() {
		return nil, ErrUnavailable
	}
	res := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += batchSize {
		end := min(i+batchSize, len(texts))
		reqs := make([]embedReq, 0, end-i)
		for _, t := range texts[i:end] {
			reqs = append(reqs, c.req(t, task))
		}
		var out struct {
			Embeddings []embedVals `json:"embeddings"`
		}
		if err := c.post(ctx, "/models/"+c.model+":batchEmbedContents", map[string]any{"requests": reqs}, &out); err != nil {
			return nil, err
		}
		if len(out.Embeddings) != len(reqs) {
			return nil, fmt.Errorf("embedding: jumlah hasil batch tidak cocok (%d != %d)", len(out.Embeddings), len(reqs))
		}
		for _, e := range out.Embeddings {
			v, err := c.finish(e.Values)
			if err != nil {
				return nil, err
			}
			res = append(res, v)
		}
	}
	return res, nil
}

func (c *Client) finish(v []float32) ([]float32, error) {
	if len(v) == 0 {
		return nil, fmt.Errorf("embedding: respons tanpa vektor")
	}
	if c.dim > 0 && len(v) != c.dim {
		return nil, fmt.Errorf("embedding: dimensi respons %d != EMBEDDING_DIM %d", len(v), c.dim)
	}
	return vector.Normalize(v), nil
}

// post: retry maks 2x (backoff 300ms, 900ms) untuk 429/5xx/jaringan; selain itu gagal langsung.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var last error
	delays := []time.Duration{0, 300 * time.Millisecond, 900 * time.Millisecond}
	for _, d := range delays {
		if d > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: %v", ErrUnavailable, ctx.Err())
			case <-time.After(d):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", c.apiKey)
		resp, err := c.hc.Do(req)
		if err != nil {
			last = fmt.Errorf("%w: %v", ErrUnavailable, err)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			last = fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
			continue
		case resp.StatusCode >= 400:
			// 4xx lain (key salah, model tidak ada) tidak akan membaik dengan retry.
			return fmt.Errorf("%w: status %d: %s", ErrUnavailable, resp.StatusCode, truncate(string(raw), 200))
		}
		return json.Unmarshal(raw, out)
	}
	return last
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ---- LRU sederhana ----

type lru struct {
	mu  sync.Mutex
	cap int
	ll  *list.List
	m   map[string]*list.Element
}
type entry struct {
	key string
	val []float32
}

func newLRU(cap int) *lru { return &lru{cap: cap, ll: list.New(), m: map[string]*list.Element{}} }

func (l *lru) get(k string) ([]float32, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.m[k]; ok {
		l.ll.MoveToFront(e)
		return e.Value.(*entry).val, true
	}
	return nil, false
}

func (l *lru) put(k string, v []float32) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.m[k]; ok {
		e.Value.(*entry).val = v
		l.ll.MoveToFront(e)
		return
	}
	l.m[k] = l.ll.PushFront(&entry{k, v})
	if l.ll.Len() > l.cap {
		last := l.ll.Back()
		l.ll.Remove(last)
		delete(l.m, last.Value.(*entry).key)
	}
}

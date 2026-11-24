// Package embedding: klien Gemini Embedding API (REST) + cache LRU untuk vektor query.
package embedding // nama package: embedding

import ( // package yang dipakai
	"bytes"          // membuat body request dari byte
	"container/list" // linked list untuk LRU
	"context"        // context & pembatalan
	"encoding/json"  // encode/decode JSON
	"errors"         // membuat error sentinel
	"fmt"            // format error
	"io"             // membaca body respons
	"net/http"       // klien HTTP
	"strings"        // utilitas string
	"sync"           // mutex untuk cache
	"time"           // durasi timeout & backoff

	"toko-ai-microservices/internal/shared/vector" // normalisasi vektor
)

// Task adalah jenis tugas embedding yang diminta ke Gemini.
type Task string

const (
	TaskDocument Task = "RETRIEVAL_DOCUMENT" // saat indexing
	TaskQuery    Task = "RETRIEVAL_QUERY"    // saat query pengguna
)

// ErrUnavailable: embedding tidak tersedia (tanpa API key / jaringan / kuota). Pemanggil
// harus jatuh ke pencarian keyword (meta.mode = "keyword_fallback").
var ErrUnavailable = errors.New("embedding unavailable")

const (
	defaultBase = "https://generativelanguage.googleapis.com/v1beta" // URL dasar API Gemini
	batchSize   = 100                                                // maks teks per batch request
	cacheSize   = 256                                                // jumlah vektor query yang di-cache
)

// Client adalah klien Gemini Embedding.
type Client struct {
	apiKey string       // API key Gemini
	model  string       // nama model embedding
	dim    int          // dimensi vektor yang diharapkan
	base   string       // URL dasar API
	hc     *http.Client // klien HTTP dengan timeout
	cache  *lru         // cache vektor query
}

// New membuat klien. apiKey kosong => Enabled()==false (semua panggilan mengembalikan ErrUnavailable).
func New(apiKey, model string, dim int, timeout time.Duration) *Client {
	return &Client{ // rakit klien
		apiKey: apiKey, model: model, dim: dim, base: defaultBase, // konfigurasi dasar
		hc: &http.Client{Timeout: timeout}, cache: newLRU(cacheSize), // HTTP client & cache
	}
}

// Enabled: true bila klien ada dan API key terisi.
func (c *Client) Enabled() bool { return c != nil && c.apiKey != "" }

// Model mengembalikan nama model embedding.
func (c *Client) Model() string { return c.model }

// Dim mengembalikan dimensi vektor.
func (c *Client) Dim() int { return c.dim }

// part adalah satu potongan teks pada request.
type part struct {
	Text string `json:"text"` // isi teks
}

// content membungkus daftar part.
type content struct {
	Parts []part `json:"parts"` // daftar potongan teks
}

// embedReq adalah payload satu permintaan embedding.
type embedReq struct {
	Model                string  `json:"model"`                          // nama model lengkap
	Content              content `json:"content"`                        // teks yang di-embed
	TaskType             Task    `json:"taskType"`                       // jenis tugas
	OutputDimensionality int     `json:"outputDimensionality,omitempty"` // dimensi keluaran (opsional)
}

// embedVals adalah vektor hasil embedding.
type embedVals struct {
	Values []float32 `json:"values"` // nilai vektor
}

// req membangun payload embedding untuk satu teks.
func (c *Client) req(text string, task Task) embedReq {
	r := embedReq{Model: "models/" + c.model, Content: content{Parts: []part{{Text: text}}}, TaskType: task} // payload dasar
	if c.dim > 0 && strings.HasPrefix(c.model, "gemini-embedding") {                                         // model mendukung pengaturan dimensi
		r.OutputDimensionality = c.dim // minta dimensi tertentu
	}
	return r
}

// Embed menghasilkan vektor ter-normalisasi L2. Vektor query (TaskQuery) di-cache.
func (c *Client) Embed(ctx context.Context, text string, task Task) ([]float32, error) {
	if !c.Enabled() { // tanpa API key
		return nil, ErrUnavailable
	}
	key := string(task) + "|" + strings.ToLower(strings.TrimSpace(text)) // kunci cache
	if task == TaskQuery {                                               // hanya query yang di-cache
		if v, ok := c.cache.get(key); ok { // cache hit
			return v, nil
		}
	}
	var out struct { // penampung respons
		Embedding embedVals `json:"embedding"`
	}
	if err := c.post(ctx, "/models/"+c.model+":embedContent", c.req(text, task), &out); err != nil { // panggil API
		return nil, err
	}
	v, err := c.finish(out.Embedding.Values) // validasi & normalisasi
	if err != nil {
		return nil, err
	}
	if task == TaskQuery { // simpan ke cache
		c.cache.put(key, v)
	}
	return v, nil
}

// EmbedBatch meng-embed banyak teks (dipecah per 100) dengan urutan hasil = urutan input.
func (c *Client) EmbedBatch(ctx context.Context, texts []string, task Task) ([][]float32, error) {
	if !c.Enabled() { // tanpa API key
		return nil, ErrUnavailable
	}
	res := make([][]float32, 0, len(texts))      // hasil akhir
	for i := 0; i < len(texts); i += batchSize { // proses per batch
		end := min(i+batchSize, len(texts)) // batas akhir batch
		reqs := make([]embedReq, 0, end-i)  // payload batch
		for _, t := range texts[i:end] {    // buat request tiap teks
			reqs = append(reqs, c.req(t, task))
		}
		var out struct { // penampung respons batch
			Embeddings []embedVals `json:"embeddings"`
		}
		if err := c.post(ctx, "/models/"+c.model+":batchEmbedContents", map[string]any{"requests": reqs}, &out); err != nil { // panggil API batch
			return nil, err
		}
		if len(out.Embeddings) != len(reqs) { // jumlah hasil harus sama dengan input
			return nil, fmt.Errorf("embedding: jumlah hasil batch tidak cocok (%d != %d)", len(out.Embeddings), len(reqs))
		}
		for _, e := range out.Embeddings { // validasi tiap vektor
			v, err := c.finish(e.Values)
			if err != nil {
				return nil, err
			}
			res = append(res, v) // tambahkan sesuai urutan
		}
	}
	return res, nil
}

// finish memvalidasi dimensi vektor lalu menormalkannya (L2).
func (c *Client) finish(v []float32) ([]float32, error) {
	if len(v) == 0 { // vektor kosong
		return nil, fmt.Errorf("embedding: respons tanpa vektor")
	}
	if c.dim > 0 && len(v) != c.dim { // dimensi tidak sesuai konfigurasi
		return nil, fmt.Errorf("embedding: dimensi respons %d != EMBEDDING_DIM %d", len(v), c.dim)
	}
	return vector.Normalize(v), nil // normalisasi L2
}

// post: retry maks 2x (backoff 300ms, 900ms) untuk 429/5xx/jaringan; selain itu gagal langsung.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body) // encode body ke JSON
	if err != nil {
		return err
	}
	var last error                                                               // error percobaan terakhir
	delays := []time.Duration{0, 300 * time.Millisecond, 900 * time.Millisecond} // jeda sebelum tiap percobaan
	for _, d := range delays {                                                   // coba maksimal 3 kali
		if d > 0 { // tunggu backoff
			select {
			case <-ctx.Done(): // dibatalkan saat menunggu
				return fmt.Errorf("%w: %v", ErrUnavailable, ctx.Err())
			case <-time.After(d): // jeda selesai
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b)) // buat request
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json") // tipe konten JSON
		req.Header.Set("x-goog-api-key", c.apiKey)         // autentikasi API key
		resp, err := c.hc.Do(req)                          // kirim request
		if err != nil {                                    // kegagalan jaringan
			last = fmt.Errorf("%w: %v", ErrUnavailable, err)
			continue // coba lagi
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // baca body (maks 8 MB)
		resp.Body.Close()                                      // tutup body
		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500: // dapat dicoba ulang
			last = fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
			continue
		case resp.StatusCode >= 400:
			// 4xx lain (key salah, model tidak ada) tidak akan membaik dengan retry.
			return fmt.Errorf("%w: status %d: %s", ErrUnavailable, resp.StatusCode, truncate(string(raw), 200))
		}
		return json.Unmarshal(raw, out) // sukses: decode respons
	}
	return last // semua percobaan gagal
}

// truncate memotong string s agar maksimal n byte.
func truncate(s string, n int) string {
	if len(s) <= n { // sudah cukup pendek
		return s
	}
	return s[:n] // potong
}

// ---- LRU sederhana ----

// lru adalah cache Least-Recently-Used yang aman untuk konkurensi.
type lru struct {
	mu  sync.Mutex               // melindungi akses konkuren
	cap int                      // kapasitas maksimum
	ll  *list.List               // urutan pemakaian (depan = terbaru)
	m   map[string]*list.Element // indeks cepat kunci -> elemen
}

// entry adalah satu pasangan kunci-vektor di cache.
type entry struct {
	key string    // kunci cache
	val []float32 // vektor tersimpan
}

// newLRU membuat cache kosong berkapasitas cap.
func newLRU(cap int) *lru { return &lru{cap: cap, ll: list.New(), m: map[string]*list.Element{}} }

// get mengambil vektor dari cache dan menandainya sebagai terbaru.
func (l *lru) get(k string) ([]float32, bool) {
	l.mu.Lock()              // kunci akses
	defer l.mu.Unlock()      // lepas kunci saat selesai
	if e, ok := l.m[k]; ok { // kunci ada
		l.ll.MoveToFront(e) // jadikan terbaru
		return e.Value.(*entry).val, true
	}
	return nil, false // tidak ditemukan
}

// put menyimpan vektor; membuang entri terlama bila melebihi kapasitas.
func (l *lru) put(k string, v []float32) {
	l.mu.Lock()              // kunci akses
	defer l.mu.Unlock()      // lepas kunci saat selesai
	if e, ok := l.m[k]; ok { // kunci sudah ada
		e.Value.(*entry).val = v // perbarui nilai
		l.ll.MoveToFront(e)      // jadikan terbaru
		return
	}
	l.m[k] = l.ll.PushFront(&entry{k, v}) // tambah entri baru di depan
	if l.ll.Len() > l.cap {               // melebihi kapasitas
		last := l.ll.Back()                  // entri paling lama
		l.ll.Remove(last)                    // hapus dari list
		delete(l.m, last.Value.(*entry).key) // hapus dari indeks
	}
}

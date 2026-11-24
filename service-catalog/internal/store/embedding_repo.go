package store // akses data embedding produk

import ( // package yang dipakai
	"context" // context

	"toko-ai-microservices/internal/shared/sqlitex" // waktu UTC
	"toko-ai-microservices/internal/shared/vector"  // encode/decode vektor
)

// EmbMeta: metadata baris product_embeddings (tanpa BLOB) untuk keputusan re-embed.
type EmbMeta struct {
	Model       string // model yang dipakai
	Dim         int    // dimensi vektor
	ContentHash string // hash teks sumber
}

// EmbeddingMeta mengembalikan metadata embedding semua produk (kunci = product_id).
func (r *Repo) EmbeddingMeta(ctx context.Context) (map[string]EmbMeta, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT product_id, model, dim, content_hash FROM product_embeddings`) // ambil metadata tanpa BLOB
	if err != nil {
		return nil, err
	}
	defer rows.Close()        // tutup cursor saat selesai
	m := map[string]EmbMeta{} // hasil
	for rows.Next() {         // baca tiap baris
		var id string
		var e EmbMeta
		if err := rows.Scan(&id, &e.Model, &e.Dim, &e.ContentHash); err != nil {
			return nil, err
		}
		m[id] = e // simpan
	}
	return m, rows.Err()
}

// UpsertEmbedding menyimpan/memperbarui vektor embedding satu produk.
func (r *Repo) UpsertEmbedding(ctx context.Context, productID, model string, dim int, hash string, vec []float32) error {
	_, err := r.db.ExecContext(ctx,
		// sisipkan baris baru, atau perbarui bila product_id sudah ada
		`INSERT INTO product_embeddings (product_id,model,dim,content_hash,embedding,updated_at) VALUES (?,?,?,?,?,?)
		 ON CONFLICT(product_id) DO UPDATE SET model=excluded.model, dim=excluded.dim,
		   content_hash=excluded.content_hash, embedding=excluded.embedding, updated_at=excluded.updated_at`,
		productID, model, dim, hash, vector.Encode(vec), sqlitex.NowUTC()) // vektor di-encode ke BLOB
	return err
}

// LoadEmbeddings memuat vektor dengan model & dimensi yang cocok (ruang vektor berbeda tidak dicampur).
func (r *Repo) LoadEmbeddings(ctx context.Context, model string, dim int) ([]vector.Item, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT product_id, embedding FROM product_embeddings WHERE model=? AND dim=?`, model, dim) // filter model & dimensi
	if err != nil {
		return nil, err
	}
	defer rows.Close()      // tutup cursor saat selesai
	var items []vector.Item // hasil
	for rows.Next() {       // baca tiap baris
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		if v := vector.Decode(blob); len(v) == dim { // decode & pastikan dimensi sesuai
			items = append(items, vector.Item{ID: id, Vec: v})
		}
	}
	return items, rows.Err()
}

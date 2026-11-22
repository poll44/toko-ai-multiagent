package store

import (
	"context"

	"toko-ai-microservices/internal/shared/sqlitex"
	"toko-ai-microservices/internal/shared/vector"
)

// EmbMeta: metadata baris product_embeddings (tanpa BLOB) untuk keputusan re-embed.
type EmbMeta struct {
	Model       string
	Dim         int
	ContentHash string
}

func (r *Repo) EmbeddingMeta(ctx context.Context) (map[string]EmbMeta, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT product_id, model, dim, content_hash FROM product_embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]EmbMeta{}
	for rows.Next() {
		var id string
		var e EmbMeta
		if err := rows.Scan(&id, &e.Model, &e.Dim, &e.ContentHash); err != nil {
			return nil, err
		}
		m[id] = e
	}
	return m, rows.Err()
}

func (r *Repo) UpsertEmbedding(ctx context.Context, productID, model string, dim int, hash string, vec []float32) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO product_embeddings (product_id,model,dim,content_hash,embedding,updated_at) VALUES (?,?,?,?,?,?)
		 ON CONFLICT(product_id) DO UPDATE SET model=excluded.model, dim=excluded.dim,
		   content_hash=excluded.content_hash, embedding=excluded.embedding, updated_at=excluded.updated_at`,
		productID, model, dim, hash, vector.Encode(vec), sqlitex.NowUTC())
	return err
}

// LoadEmbeddings memuat vektor dengan model & dimensi yang cocok (ruang vektor berbeda tidak dicampur).
func (r *Repo) LoadEmbeddings(ctx context.Context, model string, dim int) ([]vector.Item, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT product_id, embedding FROM product_embeddings WHERE model=? AND dim=?`, model, dim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []vector.Item
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		if v := vector.Decode(blob); len(v) == dim {
			items = append(items, vector.Item{ID: id, Vec: v})
		}
	}
	return items, rows.Err()
}

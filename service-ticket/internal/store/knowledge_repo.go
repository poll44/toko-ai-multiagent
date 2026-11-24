package store // repository dokumen, chunk, dan embedding knowledge base

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"errors"       // errors.Is
	"fmt"          // format string
	"strings"      // utilitas string

	"toko-ai-microservices/internal/shared/httputil"      // error API standar
	"toko-ai-microservices/internal/shared/sqlitex"       // transaksi & waktu
	"toko-ai-microservices/internal/shared/vector"        // encode/decode vektor
	"toko-ai-microservices/service-ticket/internal/model" // tipe data
)

// docNotFound membuat error 404 untuk dokumen id.
func docNotFound(id string) error { return httputil.ErrNotFound("Dokumen " + id + " tidak ditemukan.") }

// docCols: kolom dokumen + jumlah chunk (subquery); urutan harus sama dengan scanDoc.
const docCols = `d.id,d.title,d.category,d.is_active,d.created_at,d.updated_at,(SELECT COUNT(*) FROM knowledge_chunks c WHERE c.document_id=d.id)`

// scanDoc membaca satu baris dokumen ke model.Document (tanpa content).
func scanDoc(sc interface{ Scan(...any) error }, withContent bool) (*model.Document, error) {
	var d model.Document // hasil
	var active int       // is_active tersimpan sebagai 0/1
	if err := sc.Scan(&d.ID, &d.Title, &d.Category, &active, &d.CreatedAt, &d.UpdatedAt, &d.ChunkCount); err != nil {
		return nil, err
	}
	d.IsActive = active == 1 // ubah ke bool
	return &d, nil
}

// boolInt mengubah bool menjadi 1/0 untuk disimpan di SQLite.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ListDocs: daftar dokumen (tanpa content). includeInactive hanya untuk admin.
func (r *Repo) ListDocs(ctx context.Context, includeInactive bool) ([]model.Document, error) {
	q := "SELECT " + docCols + " FROM knowledge_documents d" // query dasar
	if !includeInactive {                                    // sembunyikan dokumen nonaktif
		q += " WHERE d.is_active=1"
	}
	rows, err := r.db.QueryContext(ctx, q+" ORDER BY d.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()        // tutup cursor saat selesai
	out := []model.Document{} // slice non-nil
	for rows.Next() {         // baca tiap baris
		d, err := scanDoc(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDoc: dokumen lengkap (dengan content).
func (r *Repo) GetDoc(ctx context.Context, id string) (*model.Document, error) {
	d, err := scanDoc(r.db.QueryRowContext(ctx, "SELECT "+docCols+" FROM knowledge_documents d WHERE d.id=?", id), false)
	if errors.Is(err, sql.ErrNoRows) { // tidak ditemukan
		return nil, docNotFound(id)
	}
	if err != nil {
		return nil, err
	}
	var content string // isi lengkap dokumen
	if err := r.db.QueryRowContext(ctx, `SELECT content FROM knowledge_documents WHERE id=?`, id).Scan(&content); err != nil {
		return nil, err
	}
	d.Content = &content
	return d, nil
}

// replaceChunks menghapus chunk lama dokumen lalu menyimpan chunk baru (ID: {docID}#{urutan}).
func replaceChunks(ctx context.Context, tx *sql.Tx, docID string, chunks []string, hash func(title, content string) string, title string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunks WHERE document_id=?`, docID); err != nil { // hapus chunk lama (embedding ikut terhapus)
		return err
	}
	for i, c := range chunks { // simpan tiap chunk baru
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_chunks (id,document_id,chunk_index,content,content_hash) VALUES (?,?,?,?,?)`,
			fmt.Sprintf("%s#%d", docID, i+1), docID, i+1, c, hash(title, c)); err != nil {
			return err
		}
	}
	return nil
}

// HashFunc menghitung content_hash dari teks yang di-embed (judul + isi chunk).
type HashFunc = func(title, content string) string

// CreateDoc membuat dokumen (ID KB-xxx) + chunk-nya secara atomik.
func (r *Repo) CreateDoc(ctx context.Context, in model.DocumentInput, chunks []string, hash HashFunc) (string, error) {
	var id string                                             // ID dokumen baru
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // satu transaksi
		var n int // nomor urut terbesar saat ini
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(SUBSTR(id,4) AS INTEGER)),0) FROM knowledge_documents`).Scan(&n); err != nil {
			return err
		}
		id = fmt.Sprintf("KB-%03d", n+1) // ID berikutnya
		now := sqlitex.NowUTC()          // waktu pembuatan
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_documents (id,title,category,content,is_active,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
			id, in.Title, in.Category, in.Content, boolInt(in.IsActive), now, now); err != nil {
			return err
		}
		return replaceChunks(ctx, tx, id, chunks, hash, in.Title) // simpan chunk
	})
	return id, err
}

// UpdateDoc mengubah dokumen dan membuat ulang chunk-nya (embedding lama ikut terhapus lewat ON DELETE CASCADE).
func (r *Repo) UpdateDoc(ctx context.Context, id string, in model.DocumentInput, chunks []string, hash HashFunc) error {
	return sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // satu transaksi
		res, err := tx.ExecContext(ctx, `UPDATE knowledge_documents SET title=?,category=?,content=?,is_active=?,updated_at=? WHERE id=?`,
			in.Title, in.Category, in.Content, boolInt(in.IsActive), sqlitex.NowUTC(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 { // dokumen tidak ada
			return docNotFound(id)
		}
		return replaceChunks(ctx, tx, id, chunks, hash, in.Title) // ganti chunk
	})
}

// SoftDeleteDoc menonaktifkan dokumen (is_active=0); dokumen tidak ada => 404.
func (r *Repo) SoftDeleteDoc(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE knowledge_documents SET is_active=0, updated_at=? WHERE id=?`, sqlitex.NowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 { // dokumen tidak ada
		return docNotFound(id)
	}
	return nil
}

// AllChunks: semua chunk (termasuk dokumen nonaktif) untuk indexing.
func (r *Repo) AllChunks(ctx context.Context) ([]model.Chunk, map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT c.id,c.document_id,d.title,d.category,c.content,c.content_hash
		FROM knowledge_chunks c JOIN knowledge_documents d ON d.id=c.document_id ORDER BY c.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()            // tutup cursor saat selesai
	var out []model.Chunk         // daftar chunk
	hashes := map[string]string{} // chunk id -> content_hash
	for rows.Next() {             // baca tiap baris
		var c model.Chunk
		var h string
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.Title, &c.Category, &c.Content, &h); err != nil {
			return nil, nil, err
		}
		out = append(out, c)
		hashes[c.ID] = h
	}
	return out, hashes, rows.Err()
}

// ActiveChunks: chunk dari dokumen aktif, by chunk id (untuk join hasil pencarian & filter).
func (r *Repo) ActiveChunks(ctx context.Context) (map[string]model.Chunk, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT c.id,c.document_id,d.title,d.category,c.content
		FROM knowledge_chunks c JOIN knowledge_documents d ON d.id=c.document_id WHERE d.is_active=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()            // tutup cursor saat selesai
	m := map[string]model.Chunk{} // hasil: chunk id -> chunk
	for rows.Next() {             // baca tiap baris
		var c model.Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.Title, &c.Category, &c.Content); err != nil {
			return nil, err
		}
		m[c.ID] = c
	}
	return m, rows.Err()
}

// KeywordChunks: fallback LIKE per kata kunci pada judul/isi chunk dokumen aktif.
func (r *Repo) KeywordChunks(ctx context.Context, q, category string, limit int) ([]model.Chunk, error) {
	var ors []string                                         // kondisi LIKE per kata
	var args []any                                           // argumen kondisi
	for _, tok := range strings.Fields(strings.ToLower(q)) { // pecah query menjadi kata
		tok = strings.Trim(tok, `.,;:!?"'()`)       // buang tanda baca
		if len([]rune(tok)) < 3 || stopwords[tok] { // terlalu pendek / stopword
			continue
		}
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(tok) + "%" // pola LIKE aman
		ors = append(ors, `(LOWER(d.title) LIKE ? ESCAPE '\' OR LOWER(c.content) LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	if len(ors) == 0 { // tidak ada kata kunci berguna
		return nil, nil
	}
	where := "d.is_active=1 AND (" + strings.Join(ors, " OR ") + ")" // hanya dokumen aktif
	if category != "" {                                              // filter kategori
		where += " AND d.category=?"
		args = append(args, category)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT c.id,c.document_id,d.title,d.category,c.content FROM knowledge_chunks c
		JOIN knowledge_documents d ON d.id=c.document_id WHERE `+where+` ORDER BY c.id LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()    // tutup cursor saat selesai
	var out []model.Chunk // hasil
	for rows.Next() {     // baca tiap baris
		var c model.Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.Title, &c.Category, &c.Content); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// stopwords: kata umum yang diabaikan pada keyword fallback.
var stopwords = map[string]bool{"yang": true, "dan": true, "untuk": true, "dengan": true, "atau": true, "dari": true, "apa": true, "bagaimana": true, "berapa": true, "apakah": true}

// ---- embeddings ----

// EmbMeta: metadata baris knowledge_embeddings (tanpa BLOB) untuk keputusan re-embed.
type EmbMeta struct {
	Model       string // model yang dipakai
	Dim         int    // dimensi vektor
	ContentHash string // hash teks sumber
}

// EmbeddingMeta mengembalikan metadata embedding semua chunk (kunci = chunk_id).
func (r *Repo) EmbeddingMeta(ctx context.Context) (map[string]EmbMeta, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT chunk_id,model,dim,content_hash FROM knowledge_embeddings`)
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
		m[id] = e
	}
	return m, rows.Err()
}

// UpsertEmbedding menyimpan/memperbarui vektor embedding satu chunk.
func (r *Repo) UpsertEmbedding(ctx context.Context, chunkID, modelName string, dim int, hash string, vec []float32) error {
	_, err := r.db.ExecContext(ctx,
		// sisipkan baris baru, atau perbarui bila chunk_id sudah ada
		`INSERT INTO knowledge_embeddings (chunk_id,model,dim,content_hash,embedding,updated_at) VALUES (?,?,?,?,?,?)
		 ON CONFLICT(chunk_id) DO UPDATE SET model=excluded.model, dim=excluded.dim, content_hash=excluded.content_hash,
		   embedding=excluded.embedding, updated_at=excluded.updated_at`,
		chunkID, modelName, dim, hash, vector.Encode(vec), sqlitex.NowUTC()) // vektor di-encode ke BLOB
	return err
}

// LoadEmbeddings memuat vektor dengan model & dimensi yang cocok.
func (r *Repo) LoadEmbeddings(ctx context.Context, modelName string, dim int) ([]vector.Item, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT chunk_id,embedding FROM knowledge_embeddings WHERE model=? AND dim=?`, modelName, dim)
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

package store // operasi stok produk (atomik)

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"errors"       // errors.Is

	"toko-ai-microservices/internal/shared/httputil"       // error API standar
	"toko-ai-microservices/internal/shared/sqlitex"        // transaksi & waktu
	"toko-ai-microservices/service-catalog/internal/model" // tipe data
)

// AdjustStock: mode "set" (stok = value) atau "adjust" (stok += value). Hasil < 0 => 409 INSUFFICIENT_STOCK.
func (r *Repo) AdjustStock(ctx context.Context, id, mode string, value int, actor string) (*model.Product, error) {
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // semua dalam satu transaksi
		var cur int                                                                                             // stok saat ini
		if err := tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=?`, id).Scan(&cur); err != nil { // baca stok
			if errors.Is(err, sql.ErrNoRows) { // produk tidak ada
				return notFound(id)
			}
			return err
		}
		next := value         // mode "set": stok baru = value
		if mode == "adjust" { // mode "adjust": tambah/kurang
			next = cur + value
		}
		if next < 0 { // stok tidak boleh negatif
			return httputil.ErrConflict("INSUFFICIENT_STOCK", "Stok tidak boleh bernilai negatif.",
				map[string]any{"product_id": id, "available": cur, "requested": -value})
		}
		// simpan stok baru
		if _, err := tx.ExecContext(ctx, `UPDATE products SET stock=?, updated_at=? WHERE id=?`, next, sqlitex.NowUTC(), id); err != nil {
			return err
		}
		return addMovement(ctx, tx, id, next-cur, next, "ADMIN_ADJUST", nil, actor) // catat riwayat
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id) // kembalikan produk terbaru
}

// Decrement: kurangi stok SEMUA item secara atomik (satu transaksi). Gagal satu => tidak ada yang berubah.
func (r *Repo) Decrement(ctx context.Context, ref string, items []model.StockItem, actor string) ([]model.ReservedItem, error) {
	out := make([]model.ReservedItem, 0, len(items))          // hasil reservasi
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // satu transaksi untuk semua item
		for _, it := range items { // proses tiap item
			var name string              // nama produk
			var price, stock, active int // harga, stok, status aktif
			// baca data produk
			err := tx.QueryRowContext(ctx, `SELECT name,price,stock,is_active FROM products WHERE id=?`, it.ProductID).
				Scan(&name, &price, &stock, &active)
			if errors.Is(err, sql.ErrNoRows) { // produk tidak ada
				e := notFound(it.ProductID)
				e.(*httputil.APIError).Details = map[string]any{"product_id": it.ProductID} // sertakan ID produk
				return e
			}
			if err != nil {
				return err
			}
			if active != 1 { // produk nonaktif tidak boleh dipesan
				return httputil.NewError(422, "PRODUCT_INACTIVE", "Produk "+it.ProductID+" sedang tidak dijual.",
					map[string]any{"product_id": it.ProductID})
			}
			if stock < it.Quantity { // stok kurang
				return httputil.ErrConflict("INSUFFICIENT_STOCK", "Stok produk "+it.ProductID+" tidak mencukupi.",
					map[string]any{"product_id": it.ProductID, "available": stock, "requested": it.Quantity})
			}
			after := stock - it.Quantity // stok sesudah dikurangi
			// simpan stok baru
			if _, err := tx.ExecContext(ctx, `UPDATE products SET stock=?, updated_at=? WHERE id=?`, after, sqlitex.NowUTC(), it.ProductID); err != nil {
				return err
			}
			refCopy := ref                                                                                                    // salinan agar pointer aman per iterasi
			if err := addMovement(ctx, tx, it.ProductID, -it.Quantity, after, "ORDER_RESERVE", &refCopy, actor); err != nil { // catat riwayat
				return err
			}
			out = append(out, model.ReservedItem{ProductID: it.ProductID, Name: name, UnitPrice: price, Quantity: it.Quantity, StockAfter: after}) // tambah ke hasil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Restock mengembalikan stok; idempoten per (reference_id, product_id). Mengembalikan true bila ada stok yang benar-benar dikembalikan.
func (r *Repo) Restock(ctx context.Context, ref string, items []model.StockItem, actor string) (bool, error) {
	applied := false                                          // apakah ada stok yang dikembalikan
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // satu transaksi
		for _, it := range items { // proses tiap item
			var dup int // jumlah restock yang sudah pernah dicatat
			if err := tx.QueryRowContext(ctx,
				// cek apakah restock untuk (ref, produk) ini sudah pernah dilakukan
				`SELECT COUNT(*) FROM stock_movements WHERE reason='ORDER_RESTOCK' AND reference_id=? AND product_id=?`,
				ref, it.ProductID).Scan(&dup); err != nil {
				return err
			}
			if dup > 0 { // sudah pernah: lewati (idempoten)
				continue
			}
			var stock int // stok saat ini
			if err := tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=?`, it.ProductID).Scan(&stock); err != nil {
				if errors.Is(err, sql.ErrNoRows) { // produk tidak ada
					return notFound(it.ProductID)
				}
				return err
			}
			after := stock + it.Quantity // stok sesudah dikembalikan
			// simpan stok baru
			if _, err := tx.ExecContext(ctx, `UPDATE products SET stock=?, updated_at=? WHERE id=?`, after, sqlitex.NowUTC(), it.ProductID); err != nil {
				return err
			}
			refCopy := ref                                                                                                   // salinan agar pointer aman per iterasi
			if err := addMovement(ctx, tx, it.ProductID, it.Quantity, after, "ORDER_RESTOCK", &refCopy, actor); err != nil { // catat riwayat
				return err
			}
			applied = true // minimal satu item dikembalikan
		}
		return nil
	})
	return applied, err
}

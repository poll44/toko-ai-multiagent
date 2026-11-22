package store

import (
	"context"
	"database/sql"
	"errors"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/sqlitex"
	"toko-ai-microservices/service-catalog/internal/model"
)

// AdjustStock: mode "set" (stok = value) atau "adjust" (stok += value). Hasil < 0 => 409 INSUFFICIENT_STOCK.
func (r *Repo) AdjustStock(ctx context.Context, id, mode string, value int, actor string) (*model.Product, error) {
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		var cur int
		if err := tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=?`, id).Scan(&cur); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return notFound(id)
			}
			return err
		}
		next := value
		if mode == "adjust" {
			next = cur + value
		}
		if next < 0 {
			return httputil.ErrConflict("INSUFFICIENT_STOCK", "Stok tidak boleh bernilai negatif.",
				map[string]any{"product_id": id, "available": cur, "requested": -value})
		}
		if _, err := tx.ExecContext(ctx, `UPDATE products SET stock=?, updated_at=? WHERE id=?`, next, sqlitex.NowUTC(), id); err != nil {
			return err
		}
		return addMovement(ctx, tx, id, next-cur, next, "ADMIN_ADJUST", nil, actor)
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// Decrement: kurangi stok SEMUA item secara atomik (satu transaksi). Gagal satu => tidak ada yang berubah.
func (r *Repo) Decrement(ctx context.Context, ref string, items []model.StockItem, actor string) ([]model.ReservedItem, error) {
	out := make([]model.ReservedItem, 0, len(items))
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		for _, it := range items {
			var name string
			var price, stock, active int
			err := tx.QueryRowContext(ctx, `SELECT name,price,stock,is_active FROM products WHERE id=?`, it.ProductID).
				Scan(&name, &price, &stock, &active)
			if errors.Is(err, sql.ErrNoRows) {
				e := notFound(it.ProductID)
				e.(*httputil.APIError).Details = map[string]any{"product_id": it.ProductID}
				return e
			}
			if err != nil {
				return err
			}
			if active != 1 {
				return httputil.NewError(422, "PRODUCT_INACTIVE", "Produk "+it.ProductID+" sedang tidak dijual.",
					map[string]any{"product_id": it.ProductID})
			}
			if stock < it.Quantity {
				return httputil.ErrConflict("INSUFFICIENT_STOCK", "Stok produk "+it.ProductID+" tidak mencukupi.",
					map[string]any{"product_id": it.ProductID, "available": stock, "requested": it.Quantity})
			}
			after := stock - it.Quantity
			if _, err := tx.ExecContext(ctx, `UPDATE products SET stock=?, updated_at=? WHERE id=?`, after, sqlitex.NowUTC(), it.ProductID); err != nil {
				return err
			}
			refCopy := ref
			if err := addMovement(ctx, tx, it.ProductID, -it.Quantity, after, "ORDER_RESERVE", &refCopy, actor); err != nil {
				return err
			}
			out = append(out, model.ReservedItem{ProductID: it.ProductID, Name: name, UnitPrice: price, Quantity: it.Quantity, StockAfter: after})
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
	applied := false
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		for _, it := range items {
			var dup int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM stock_movements WHERE reason='ORDER_RESTOCK' AND reference_id=? AND product_id=?`,
				ref, it.ProductID).Scan(&dup); err != nil {
				return err
			}
			if dup > 0 {
				continue
			}
			var stock int
			if err := tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=?`, it.ProductID).Scan(&stock); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return notFound(it.ProductID)
				}
				return err
			}
			after := stock + it.Quantity
			if _, err := tx.ExecContext(ctx, `UPDATE products SET stock=?, updated_at=? WHERE id=?`, after, sqlitex.NowUTC(), it.ProductID); err != nil {
				return err
			}
			refCopy := ref
			if err := addMovement(ctx, tx, it.ProductID, it.Quantity, after, "ORDER_RESTOCK", &refCopy, actor); err != nil {
				return err
			}
			applied = true
		}
		return nil
	})
	return applied, err
}

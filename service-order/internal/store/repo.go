package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/sqlitex"
	"toko-ai-microservices/service-order/internal/model"
)

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// queryer dipenuhi *sql.DB dan *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func notFound(id string) error { return httputil.ErrNotFound("Pesanan " + id + " tidak ditemukan.") }

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

const orderCols = `id,user_id,status,payment_status,payment_method,total_amount,shipping_address,tracking_number,failure_reason,payment_expires_at,paid_at,created_at,updated_at`

func scanOrder(sc interface{ Scan(...any) error }) (*model.Order, error) {
	var o model.Order
	var track, fail, exp, paid sql.NullString
	if err := sc.Scan(&o.ID, &o.UserID, &o.Status, &o.PaymentStatus, &o.PaymentMethod, &o.TotalAmount,
		&o.ShippingAddress, &track, &fail, &exp, &paid, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return nil, err
	}
	o.TrackingNumber, o.FailureReason, o.PaymentExpiresAt, o.PaidAt = nullStr(track), nullStr(fail), nullStr(exp), nullStr(paid)
	o.Items = []model.OrderItem{}
	return &o, nil
}

func loadItems(ctx context.Context, q queryer, o *model.Order) error {
	rows, err := q.QueryContext(ctx, `SELECT product_id,product_name,unit_price,quantity,subtotal FROM order_items WHERE order_id=? ORDER BY id`, o.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var it model.OrderItem
		if err := rows.Scan(&it.ProductID, &it.ProductName, &it.UnitPrice, &it.Quantity, &it.Subtotal); err != nil {
			return err
		}
		o.Items = append(o.Items, it)
	}
	return rows.Err()
}

func loadHistory(ctx context.Context, q queryer, o *model.Order) error {
	rows, err := q.QueryContext(ctx, `SELECT from_status,to_status,note,actor_id,created_at FROM order_status_history WHERE order_id=? ORDER BY id`, o.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	o.History = []model.HistoryEntry{}
	for rows.Next() {
		var h model.HistoryEntry
		var from, note sql.NullString
		if err := rows.Scan(&from, &h.ToStatus, &note, &h.ActorID, &h.CreatedAt); err != nil {
			return err
		}
		h.FromStatus, h.Note = nullStr(from), nullStr(note)
		o.History = append(o.History, h)
	}
	return rows.Err()
}

func getOrder(ctx context.Context, q queryer, id string, withHistory bool) (*model.Order, error) {
	o, err := scanOrder(q.QueryRowContext(ctx, "SELECT "+orderCols+" FROM orders WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, err
	}
	if err := loadItems(ctx, q, o); err != nil {
		return nil, err
	}
	if withHistory {
		if err := loadHistory(ctx, q, o); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (r *Repo) Get(ctx context.Context, id string, withHistory bool) (*model.Order, error) {
	return getOrder(ctx, r.db, id, withHistory)
}

// NextID: ORD-001, ORD-002, … (pemanggil harus menserialkan pembuatan; lihat service.Create).
func (r *Repo) NextID(ctx context.Context) (string, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(SUBSTR(id,5) AS INTEGER)),0) FROM orders`).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("ORD-%03d", n+1), nil
}

func insertOrder(ctx context.Context, tx *sql.Tx, o *model.Order) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO orders (`+orderCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.UserID, o.Status, o.PaymentStatus, o.PaymentMethod, o.TotalAmount, o.ShippingAddress,
		o.TrackingNumber, o.FailureReason, o.PaymentExpiresAt, o.PaidAt, o.CreatedAt, o.UpdatedAt); err != nil {
		return err
	}
	for _, it := range o.Items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_items (order_id,product_id,product_name,unit_price,quantity,subtotal) VALUES (?,?,?,?,?,?)`,
			o.ID, it.ProductID, it.ProductName, it.UnitPrice, it.Quantity, it.Subtotal); err != nil {
			return err
		}
	}
	return nil
}

func addHistory(ctx context.Context, tx *sql.Tx, orderID string, from *string, to string, note *string, actor, at string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO order_status_history (order_id,from_status,to_status,note,actor_id,created_at) VALUES (?,?,?,?,?,?)`,
		orderID, from, to, note, actor, at)
	return err
}

// Create menyimpan order + item + history awal secara atomik.
func (r *Repo) Create(ctx context.Context, o *model.Order, actor string) error {
	return sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := insertOrder(ctx, tx, o); err != nil {
			return err
		}
		return addHistory(ctx, tx, o.ID, nil, o.Status, nil, actor, o.CreatedAt)
	})
}

// Apply memuat order dalam transaksi, menjalankan mut (boleh mengubah field & mengembalikan note/error), lalu
// menulis perubahan. Bila mut error => rollback. Perubahan status dicatat di order_status_history.
func (r *Repo) Apply(ctx context.Context, id, actor string, mut func(o *model.Order) (note string, err error)) (*model.Order, error) {
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		o, err := getOrder(ctx, tx, id, false)
		if err != nil {
			return err
		}
		oldStatus := o.Status
		note, err := mut(o)
		if err != nil {
			return err
		}
		now := sqlitex.NowUTC()
		if _, err := tx.ExecContext(ctx,
			`UPDATE orders SET status=?,payment_status=?,tracking_number=?,failure_reason=?,payment_expires_at=?,paid_at=?,updated_at=? WHERE id=?`,
			o.Status, o.PaymentStatus, o.TrackingNumber, o.FailureReason, o.PaymentExpiresAt, o.PaidAt, now, id); err != nil {
			return err
		}
		if o.Status != oldStatus {
			var n *string
			if note != "" {
				n = &note
			}
			return addHistory(ctx, tx, id, &oldStatus, o.Status, n, actor, now)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id, true)
}

func (r *Repo) List(ctx context.Context, f model.ListFilter) ([]model.Order, int, error) {
	where, args := []string{"1=1"}, []any{}
	if f.UserID != nil {
		where = append(where, "user_id=?")
		args = append(args, *f.UserID)
	}
	if f.Status != "" {
		where = append(where, "status=?")
		args = append(args, f.Status)
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM orders WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx,
		"SELECT "+orderCols+" FROM orders WHERE "+w+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		append(args, f.Limit, (f.Page-1)*f.Limit)...)
	if err != nil {
		return nil, 0, err
	}
	var out []model.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, *o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out { // MaxOpenConns=1: item dimuat setelah rows ditutup
		if err := loadItems(ctx, r.db, &out[i]); err != nil {
			return nil, 0, err
		}
	}
	if out == nil {
		out = []model.Order{}
	}
	return out, total, nil
}

// ExpiredPending: id order PENDING_PAYMENT dengan payment_expires_at < before (RFC 3339 UTC; urut leksikografis = kronologis).
func (r *Repo) ExpiredPending(ctx context.Context, before string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM orders WHERE status='PENDING_PAYMENT' AND payment_expires_at IS NOT NULL AND payment_expires_at < ? ORDER BY id`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

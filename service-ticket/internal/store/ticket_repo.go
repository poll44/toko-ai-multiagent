package store // repository akses data tiket

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"errors"       // errors.Is
	"fmt"          // format string
	"strings"      // utilitas string

	"toko-ai-microservices/internal/shared/httputil"      // error API standar
	"toko-ai-microservices/internal/shared/sqlitex"       // transaksi & waktu
	"toko-ai-microservices/service-ticket/internal/model" // tipe data
)

// Repo membungkus koneksi database ticket.db.
type Repo struct{ db *sql.DB }

// NewRepo membuat Repo dari koneksi database.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// queryer dipenuhi *sql.DB dan *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) // query banyak baris
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row        // query satu baris
}

// nullStr mengubah sql.NullString menjadi *string (nil bila NULL).
func nullStr(n sql.NullString) *string {
	if !n.Valid { // NULL
		return nil
	}
	s := n.String
	return &s
}

// ticketNotFound membuat error 404 untuk tiket id.
func ticketNotFound(id string) error {
	return httputil.ErrNotFound("Tiket " + id + " tidak ditemukan.")
}

// ticketCols: daftar kolom tiket (urutan harus sama dengan scanTicket).
const ticketCols = `id,user_id,order_id,subject,description,category,priority,status,assigned_to,resolution_note,created_at,updated_at,resolved_at,closed_at`

// scanTicket membaca satu baris tiket ke model.Ticket (tanpa pesan & riwayat).
func scanTicket(sc interface{ Scan(...any) error }) (*model.Ticket, error) {
	var t model.Ticket                                         // hasil
	var order, assigned, note, resolved, closed sql.NullString // kolom yang bisa NULL
	if err := sc.Scan(&t.ID, &t.UserID, &order, &t.Subject, &t.Description, &t.Category, &t.Priority, &t.Status,
		&assigned, &note, &t.CreatedAt, &t.UpdatedAt, &resolved, &closed); err != nil {
		return nil, err
	}
	t.OrderID, t.AssignedTo, t.ResolutionNote, t.ResolvedAt, t.ClosedAt = nullStr(order), nullStr(assigned), nullStr(note), nullStr(resolved), nullStr(closed) // NULL -> nil
	return &t, nil
}

// getTicket mengambil tiket lewat q (DB atau transaksi); detail=true menyertakan pesan & riwayat.
func getTicket(ctx context.Context, q queryer, id string, detail bool) (*model.Ticket, error) {
	t, err := scanTicket(q.QueryRowContext(ctx, "SELECT "+ticketCols+" FROM tickets WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) { // tidak ditemukan
		return nil, ticketNotFound(id)
	}
	if err != nil {
		return nil, err
	}
	if !detail { // tanpa detail
		return t, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id,author_id,author_role,message,created_at FROM ticket_messages WHERE ticket_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	t.Messages = []model.Message{} // slice non-nil
	for rows.Next() {              // baca tiap pesan
		var m model.Message
		if err := rows.Scan(&m.ID, &m.AuthorID, &m.AuthorRole, &m.Message, &m.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		t.Messages = append(t.Messages, m)
	}
	rows.Close() // tutup sebelum query berikutnya (MaxOpenConns=1)
	hrows, err := q.QueryContext(ctx, `SELECT from_status,to_status,actor_id,created_at FROM ticket_status_history WHERE ticket_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer hrows.Close()           // tutup cursor saat selesai
	t.History = []model.History{} // slice non-nil
	for hrows.Next() {            // baca tiap riwayat
		var h model.History
		var from sql.NullString // status sebelumnya bisa NULL
		if err := hrows.Scan(&from, &h.ToStatus, &h.ActorID, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.FromStatus = nullStr(from)
		t.History = append(t.History, h)
	}
	return t, hrows.Err()
}

// Get memuat tiket; detail=true menyertakan messages & history.
func (r *Repo) Get(ctx context.Context, id string, detail bool) (*model.Ticket, error) {
	return getTicket(ctx, r.db, id, detail)
}

// insertMessage menyimpan satu pesan tiket di dalam transaksi tx.
func insertMessage(ctx context.Context, tx *sql.Tx, ticketID, author, role, msg, at string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ticket_messages (ticket_id,author_id,author_role,message,created_at) VALUES (?,?,?,?,?)`,
		ticketID, author, role, msg, at)
	return err
}

// insertHistory mencatat satu perubahan status tiket di dalam transaksi tx.
func insertHistory(ctx context.Context, tx *sql.Tx, ticketID string, from *string, to, actor, at string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ticket_status_history (ticket_id,from_status,to_status,actor_id,created_at) VALUES (?,?,?,?,?)`,
		ticketID, from, to, actor, at)
	return err
}

// insertTicket menyimpan baris tiket di dalam transaksi tx.
func insertTicket(ctx context.Context, tx *sql.Tx, t *model.Ticket) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO tickets (`+ticketCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, t.OrderID, t.Subject, t.Description, t.Category, t.Priority, t.Status,
		t.AssignedTo, t.ResolutionNote, t.CreatedAt, t.UpdatedAt, t.ResolvedAt, t.ClosedAt)
	return err
}

// ActiveForOrder: id tiket aktif (OPEN/IN_PROGRESS) milik user untuk order tertentu ("" bila tidak ada).
func (r *Repo) ActiveForOrder(ctx context.Context, userID, orderID string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM tickets WHERE user_id=? AND order_id=? AND status IN ('OPEN','IN_PROGRESS') ORDER BY id LIMIT 1`, userID, orderID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) { // tidak ada tiket aktif
		return "", nil
	}
	return id, err
}

// Create menyimpan tiket (ID TKT-xxx dibuat dalam transaksi), pesan awal (= description), dan history awal.
func (r *Repo) Create(ctx context.Context, t *model.Ticket, authorRole string) (*model.Ticket, error) {
	var id string                                             // ID tiket baru
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // satu transaksi
		var n int // nomor urut terbesar saat ini
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(SUBSTR(id,5) AS INTEGER)),0) FROM tickets`).Scan(&n); err != nil {
			return err
		}
		id = fmt.Sprintf("TKT-%03d", n+1) // ID berikutnya
		t.ID = id
		if err := insertTicket(ctx, tx, t); err != nil { // simpan tiket
			return err
		}
		if err := insertMessage(ctx, tx, id, t.UserID, authorRole, t.Description, t.CreatedAt); err != nil { // pesan awal
			return err
		}
		return insertHistory(ctx, tx, id, nil, t.Status, t.UserID, t.CreatedAt) // riwayat awal
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id, true) // kembalikan tiket lengkap
}

// List mengembalikan tiket sesuai filter + total untuk paginasi.
func (r *Repo) List(ctx context.Context, f model.TicketFilter) ([]model.Ticket, int, error) {
	where, args := []string{"1=1"}, []any{}                                                 // kondisi dasar & argumen
	add := func(cond string, v any) { where = append(where, cond); args = append(args, v) } // pembantu menambah kondisi
	if f.UserID != nil {                                                                    // filter pemilik
		add("user_id=?", *f.UserID)
	}
	if f.Status != "" { // filter status
		add("status=?", f.Status)
	}
	if f.Category != "" { // filter kategori
		add("category=?", f.Category)
	}
	if f.Priority != "" { // filter prioritas
		add("priority=?", f.Priority)
	}
	if f.OrderID != "" { // filter pesanan
		add("order_id=?", f.OrderID)
	}
	if f.AssignedTo != "" { // filter penanggung jawab
		add("assigned_to=?", f.AssignedTo)
	}
	w := strings.Join(where, " AND ") // gabungkan kondisi
	var total int                     // total baris
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tickets WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+ticketCols+" FROM tickets WHERE "+w+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		append(args, f.Limit, (f.Page-1)*f.Limit)...) // tambah LIMIT & OFFSET
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()      // tutup cursor saat selesai
	out := []model.Ticket{} // slice non-nil
	for rows.Next() {       // baca tiap baris
		t, err := scanTicket(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

// Change: hasil mut pada Apply — pesan opsional yang ditulis atomik bersama perubahan tiket.
type Change struct {
	Message string // kosong = tidak ada pesan
}

// Apply memuat tiket dalam transaksi, menjalankan mut (mengubah field), lalu menulis perubahan.
// Perubahan status dicatat di ticket_status_history; Change.Message disimpan sebagai pesan oleh actor.
func (r *Repo) Apply(ctx context.Context, id, actorID, actorRole string, mut func(t *model.Ticket) (Change, error)) (*model.Ticket, error) {
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // satu transaksi
		t, err := getTicket(ctx, tx, id, false) // muat tiket
		if err != nil {
			return err
		}
		old := t.Status   // status sebelum diubah
		ch, err := mut(t) // terapkan perubahan dari pemanggil
		if err != nil {
			return err // batalkan transaksi
		}
		now := sqlitex.NowUTC() // waktu perubahan
		if _, err := tx.ExecContext(ctx,
			// simpan seluruh field yang boleh berubah
			`UPDATE tickets SET priority=?,status=?,assigned_to=?,resolution_note=?,updated_at=?,resolved_at=?,closed_at=? WHERE id=?`,
			t.Priority, t.Status, t.AssignedTo, t.ResolutionNote, now, t.ResolvedAt, t.ClosedAt, id); err != nil {
			return err
		}
		if t.Status != old { // status berubah => catat riwayat
			if err := insertHistory(ctx, tx, id, &old, t.Status, actorID, now); err != nil {
				return err
			}
		}
		if ch.Message != "" { // simpan pesan bila ada
			return insertMessage(ctx, tx, id, actorID, actorRole, ch.Message, now)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id, true) // kembalikan tiket terbaru
}

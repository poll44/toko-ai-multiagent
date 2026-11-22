package store

import (
	"context"
	"database/sql"
	"errors"

	"toko-ai-microservices/internal/shared/sqlitex"
)

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// Credentials: user + password (plaintext demo) + status aktif, hanya untuk verifikasi login.
type Credentials struct {
	User
	Password string
	Active   bool
}

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// FindByEmail mengembalikan (nil, nil) bila email tidak dikenal.
func (r *Repo) FindByEmail(ctx context.Context, email string) (*Credentials, error) {
	var c Credentials
	var active int
	err := r.db.QueryRowContext(ctx, `SELECT id,email,name,role,password,is_active FROM users WHERE email=?`, email).
		Scan(&c.ID, &c.Email, &c.Name, &c.Role, &c.Password, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Active = active == 1
	return &c, nil
}

// DemoUsers: user aktif, urut CUSTOMER, ADMIN_SISTEM, ADMIN_DEV lalu id. TIDAK memuat password.
func (r *Repo) DemoUsers(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,email,name,role FROM users WHERE is_active=1
		ORDER BY CASE role WHEN 'CUSTOMER' THEN 0 WHEN 'ADMIN_SISTEM' THEN 1 ELSE 2 END, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ExecForTest menjalankan SQL mentah — HANYA untuk menyiapkan kondisi pada test (mis. menonaktifkan akun).
func (r *Repo) ExecForTest(ctx context.Context, query string, args ...any) (int64, error) {
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LogLogin mencatat percobaan login ke login_audit (userID kosong => NULL).
func (r *Repo) LogLogin(ctx context.Context, userID, email string, success bool, ip string) error {
	var uid any
	if userID != "" {
		uid = userID
	}
	ok := 0
	if success {
		ok = 1
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO login_audit (user_id,email,success,ip,created_at) VALUES (?,?,?,?,?)`,
		uid, email, ok, ip, sqlitex.NowUTC())
	return err
}

// Package svcclient: HTTP client service-ke-service (net/http) dengan propagasi identitas & internal key.
package svcclient // nama package: svcclient

import ( // package yang dipakai
	"context"       // context request
	"encoding/json" // encode/decode JSON
	"errors"        // errors.As
	"fmt"           // format string
	"io"            // membaca body respons
	"net/http"      // klien HTTP
	"strings"       // utilitas string
	"time"          // durasi

	"toko-ai-microservices/internal/shared/httputil" // helper HTTP bersama
)

// Client adalah klien HTTP untuk memanggil service lain.
type Client struct {
	base string       // URL dasar service tujuan
	key  string       // internal API key
	name string       // dikirim sebagai x-service-name
	hc   *http.Client // klien HTTP dengan timeout
}

// New membuat Client baru untuk service tujuan baseURL.
func New(baseURL, internalKey, serviceName string, timeout time.Duration) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"), // buang "/" di akhir URL
		key:  internalKey,                     // kunci internal
		name: serviceName,                     // nama service pemanggil
		hc:   &http.Client{Timeout: timeout},  // timeout per request
	}
}

// UpstreamError: error dari service tujuan (atau jaringan) dalam format standar.
type UpstreamError struct {
	Status  int    // status HTTP
	Code    string // kode error
	Message string // pesan error
	Details any    // detail tambahan
}

// Error mengimplementasikan interface error.
func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream %d %s: %s", e.Status, e.Code, e.Message)
}

// APIError mengonversi ke *httputil.APIError agar bisa langsung diteruskan lewat httputil.Fail.
func (e *UpstreamError) APIError() *httputil.APIError {
	return httputil.NewError(e.Status, e.Code, e.Message, e.Details)
}

// Do memanggil {base}{path}. `in` (opsional) di-encode JSON; `out` (opsional) diisi dari field "data".
// Identitas (x-user-id, x-user-role, x-request-id) disalin dari httputil.IdentityFrom(ctx).
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	req, err := c.newRequest(ctx, method, path, in) // bangun request
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req) // kirim request
	if err != nil {
		return transportError(err) // petakan error jaringan
	}
	defer resp.Body.Close()                                // tutup body saat selesai
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // baca body (maks 4 MB)

	if resp.StatusCode >= 400 { // respons error
		var env struct { // amplop error standar
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Details any    `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &env) != nil || env.Error.Code == "" { // format error tidak dikenali
			return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Respons error service tujuan tidak dikenali."}
		}
		return &UpstreamError{Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message, Details: env.Error.Details} // teruskan error asli
	}
	if out == nil || resp.StatusCode == http.StatusNoContent { // tidak perlu decode body
		return nil
	}
	var env struct { // amplop sukses: {"data": ...}
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil { // bukan JSON valid
		return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Respons service tujuan bukan JSON valid."}
	}
	if err := json.Unmarshal(env.Data, out); err != nil { // isi data tidak sesuai tipe out
		return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Format data service tujuan tidak sesuai kontrak."}
	}
	return nil
}

// DoWithRetry mengulang hanya untuk kegagalan jaringan/5xx (gunakan HANYA untuk operasi idempoten, mis. restock).
func (c *Client) DoWithRetry(ctx context.Context, attempts int, backoff time.Duration, method, path string, in, out any) error {
	var err error             // error percobaan terakhir
	for i := range attempts { // ulangi sebanyak attempts
		if err = c.Do(ctx, method, path, in, out); err == nil { // berhasil
			return nil
		}
		var ue *UpstreamError                        // penampung UpstreamError
		if !errors.As(err, &ue) || ue.Status < 500 { // bukan 5xx: tidak perlu retry
			return err
		}
		select { // tunggu sebelum percobaan berikutnya
		case <-ctx.Done(): // dibatalkan
			return err
		case <-time.After(backoff * time.Duration(i+1)): // backoff bertambah linear
		}
	}
	return err // semua percobaan gagal
}

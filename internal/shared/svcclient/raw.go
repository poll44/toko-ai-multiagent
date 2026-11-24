package svcclient // klien service-ke-service

import ( // package yang dipakai
	"bytes"         // body request dari byte
	"context"       // context request
	"encoding/json" // encode/validasi JSON
	"errors"        // errors.Is / errors.As
	"io"            // membaca body
	"net"           // tipe net.Error (timeout)
	"net/http"      // tipe HTTP

	"toko-ai-microservices/internal/shared/httputil" // helper HTTP bersama
)

// newRequest membangun request ke upstream dengan header internal & identitas dari ctx.
func (c *Client) newRequest(ctx context.Context, method, path string, in any) (*http.Request, error) {
	var body io.Reader // body request (nil bila tanpa payload)
	if in != nil {     // ada payload
		b, err := json.Marshal(in) // encode ke JSON
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b) // jadikan reader
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body) // buat request
	if err != nil {
		return nil, err
	}
	if in != nil { // payload JSON
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(httputil.HeaderInternalKey, c.key)  // sertakan kunci internal
	req.Header.Set(httputil.HeaderServiceName, c.name) // sertakan nama service pemanggil
	id := httputil.IdentityFrom(ctx)                   // identitas dari context
	if id.UserID != "" {                               // identitas ada
		req.Header.Set(httputil.HeaderUserID, id.UserID) // teruskan user id
		req.Header.Set(httputil.HeaderUserRole, id.Role) // teruskan role
	}
	if id.RequestID != "" { // ada request id
		req.Header.Set(httputil.HeaderRequestID, id.RequestID) // teruskan untuk pelacakan
	}
	return req, nil
}

// transportError memetakan kegagalan jaringan/timeout ke UpstreamError standar.
func transportError(err error) *UpstreamError {
	var ne net.Error                                                                       // penampung error jaringan
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) { // timeout
		return &UpstreamError{Status: http.StatusGatewayTimeout, Code: "UPSTREAM_TIMEOUT", Message: "Service tujuan tidak merespons tepat waktu."}
	}
	return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Service tujuan tidak dapat dihubungi."} // gagal koneksi
}

// DoRaw memanggil upstream dan mengembalikan status + body JSON APA ADANYA (untuk passthrough, mis. MCP).
// Kegagalan jaringan/timeout dikembalikan sebagai body error standar ({"error":{...}}) dengan status 502/504,
// sehingga pemanggil selalu menerima JSON sesuai kontrak (Master TRD §7).
func (c *Client) DoRaw(ctx context.Context, method, path string, in any) (status int, body []byte) {
	req, err := c.newRequest(ctx, method, path, in) // bangun request
	if err != nil {
		return errorBody(ctx, &UpstreamError{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Gagal menyiapkan request."})
	}
	resp, err := c.hc.Do(req) // kirim request
	if err != nil {
		return errorBody(ctx, transportError(err)) // bungkus error jaringan jadi JSON
	}
	defer resp.Body.Close()                                // tutup body saat selesai
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // baca body (maks 4 MB)
	if len(raw) == 0 || !json.Valid(raw) {                 // body kosong atau bukan JSON
		if resp.StatusCode >= 400 || len(raw) > 0 { // error atau ada isi non-JSON
			return errorBody(ctx, &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Respons service tujuan bukan JSON."})
		}
		return resp.StatusCode, []byte("null") // sukses tanpa body
	}
	return resp.StatusCode, raw // teruskan apa adanya
}

// errorBody membangun body error standar JSON dari UpstreamError.
func errorBody(ctx context.Context, e *UpstreamError) (int, []byte) {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{ // amplop error standar
		"code": e.Code, "message": e.Message, "details": nil, "request_id": httputil.RequestIDFrom(ctx),
	}})
	return e.Status, b // status & body
}

package svcclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"toko-ai-microservices/internal/shared/httputil"
)

// newRequest membangun request ke upstream dengan header internal & identitas dari ctx.
func (c *Client) newRequest(ctx context.Context, method, path string, in any) (*http.Request, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(httputil.HeaderInternalKey, c.key)
	req.Header.Set(httputil.HeaderServiceName, c.name)
	id := httputil.IdentityFrom(ctx)
	if id.UserID != "" {
		req.Header.Set(httputil.HeaderUserID, id.UserID)
		req.Header.Set(httputil.HeaderUserRole, id.Role)
	}
	if id.RequestID != "" {
		req.Header.Set(httputil.HeaderRequestID, id.RequestID)
	}
	return req, nil
}

// transportError memetakan kegagalan jaringan/timeout ke UpstreamError standar.
func transportError(err error) *UpstreamError {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &UpstreamError{Status: http.StatusGatewayTimeout, Code: "UPSTREAM_TIMEOUT", Message: "Service tujuan tidak merespons tepat waktu."}
	}
	return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Service tujuan tidak dapat dihubungi."}
}

// DoRaw memanggil upstream dan mengembalikan status + body JSON APA ADANYA (untuk passthrough, mis. MCP).
// Kegagalan jaringan/timeout dikembalikan sebagai body error standar ({"error":{...}}) dengan status 502/504,
// sehingga pemanggil selalu menerima JSON sesuai kontrak (Master TRD §7).
func (c *Client) DoRaw(ctx context.Context, method, path string, in any) (status int, body []byte) {
	req, err := c.newRequest(ctx, method, path, in)
	if err != nil {
		return errorBody(ctx, &UpstreamError{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Gagal menyiapkan request."})
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return errorBody(ctx, transportError(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if len(raw) == 0 || !json.Valid(raw) {
		if resp.StatusCode >= 400 || len(raw) > 0 {
			return errorBody(ctx, &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Respons service tujuan bukan JSON."})
		}
		return resp.StatusCode, []byte("null")
	}
	return resp.StatusCode, raw
}

func errorBody(ctx context.Context, e *UpstreamError) (int, []byte) {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{
		"code": e.Code, "message": e.Message, "details": nil, "request_id": httputil.RequestIDFrom(ctx),
	}})
	return e.Status, b
}

// Package svcclient: HTTP client service-ke-service (net/http) dengan propagasi identitas & internal key.
package svcclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"toko-ai-microservices/internal/shared/httputil"
)

type Client struct {
	base string
	key  string
	name string // dikirim sebagai x-service-name
	hc   *http.Client
}

func New(baseURL, internalKey, serviceName string, timeout time.Duration) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		key:  internalKey,
		name: serviceName,
		hc:   &http.Client{Timeout: timeout},
	}
}

// UpstreamError: error dari service tujuan (atau jaringan) dalam format standar.
type UpstreamError struct {
	Status  int
	Code    string
	Message string
	Details any
}

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
	req, err := c.newRequest(ctx, method, path, in)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode >= 400 {
		var env struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Details any    `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &env) != nil || env.Error.Code == "" {
			return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Respons error service tujuan tidak dikenali."}
		}
		return &UpstreamError{Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message, Details: env.Error.Details}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Respons service tujuan bukan JSON valid."}
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return &UpstreamError{Status: http.StatusBadGateway, Code: "UPSTREAM_ERROR", Message: "Format data service tujuan tidak sesuai kontrak."}
	}
	return nil
}

// DoWithRetry mengulang hanya untuk kegagalan jaringan/5xx (gunakan HANYA untuk operasi idempoten, mis. restock).
func (c *Client) DoWithRetry(ctx context.Context, attempts int, backoff time.Duration, method, path string, in, out any) error {
	var err error
	for i := range attempts {
		if err = c.Do(ctx, method, path, in, out); err == nil {
			return nil
		}
		var ue *UpstreamError
		if !errors.As(err, &ue) || ue.Status < 500 {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(backoff * time.Duration(i+1)):
		}
	}
	return err
}

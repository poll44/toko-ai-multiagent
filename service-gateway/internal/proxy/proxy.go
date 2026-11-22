// Package proxy: reverse proxy ke domain services memakai httputil.NewSingleHostReverseProxy (PRD 01 §8).
package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	shared "toko-ai-microservices/internal/shared/httputil"
)

type Options struct {
	Target             *url.URL
	InternalKey        string
	Timeout            time.Duration // ResponseHeaderTimeout ke upstream
	StripPrefix        string        // mis. "/api/agent" (path publik -> path upstream)
	StripBrowserOrigin bool          // hapus Origin/Referer (khusus agent; ADK menolak request ber-Origin)
}

// New membangun proxy: menyuntik x-user-id, x-user-role, x-internal-api-key, x-request-id; menghapus Authorization
// (JWT TIDAK diteruskan ke service domain). Respons upstream diteruskan apa adanya; kegagalan jaringan => 502/504 JSON.
func New(o Options) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(o.Target)
	base := rp.Director
	rp.Director = func(r *http.Request) {
		base(r) // set scheme/host & gabung path
		if o.StripPrefix != "" {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, o.StripPrefix)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
			r.URL.RawPath = ""
		}
		id := shared.IdentityFrom(r.Context())
		r.Header.Set(shared.HeaderUserID, id.UserID)
		r.Header.Set(shared.HeaderUserRole, id.Role)
		r.Header.Set(shared.HeaderInternalKey, o.InternalKey)
		r.Header.Set(shared.HeaderRequestID, id.RequestID)
		r.Header.Del("Authorization")
		r.Header.Del(shared.HeaderServiceName) // jaga-jaga; sudah disanitasi di depan
		if o.StripBrowserOrigin {
			r.Header.Del("Origin")
			r.Header.Del("Referer")
		}
		r.Host = o.Target.Host
	}
	rp.Transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		ResponseHeaderTimeout: o.Timeout,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       60 * time.Second,
	}
	rp.FlushInterval = -1 // flush segera: wajib untuk SSE /run_sse
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) ||
			strings.Contains(err.Error(), "timeout awaiting response headers") {
			shared.WriteError(w, r, http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", "Service tujuan tidak merespons tepat waktu.", nil)
			return
		}
		if errors.Is(err, context.Canceled) { // klien memutus koneksi
			return
		}
		shared.WriteError(w, r, http.StatusBadGateway, "UPSTREAM_ERROR", "Service tujuan tidak dapat dihubungi.", nil)
	}
	return rp
}

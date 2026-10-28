package httpapi

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strings"
)

// handle enforces loopback authority, browser origin and API authentication.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip, parseErr := netip.ParseAddr(host)
	if err != nil || parseErr != nil || !ip.Unmap().IsLoopback() || r.Host != s.authority || r.URL.IsAbs() {
		writeError(w, http.StatusForbidden, "untrusted request authority")
		return
	}
	if origins := r.Header.Values("Origin"); len(origins) > 1 || (len(origins) == 1 && !s.trustedOrigin(origins[0])) {
		writeError(w, http.StatusForbidden, "untrusted request origin")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeError(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		if !s.authenticate(r) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
	}
	// Reject ambiguous paths instead of letting ServeMux redirect them into the shell.
	if path.Clean(r.URL.Path) != r.URL.Path || strings.Contains(r.URL.Path, "\\") {
		http.NotFound(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// trustedOrigin accepts the server origin and, only in development, one exact UI origin.
func (s *Server) trustedOrigin(origin string) bool {
	if origin == s.origin {
		return true
	}
	return s.devOrigin != "" && origin == s.devOrigin
}

// authenticate checks the request's explicit API bearer credential.
func (s *Server) authenticate(r *http.Request) bool {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		return false
	}
	token, present := strings.CutPrefix(headers[0], "Bearer ")
	return present && equalSecret(token, s.apiToken)
}

// equalSecret compares local credentials without revealing matching prefixes.
func equalSecret(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

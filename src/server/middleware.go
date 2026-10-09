package server

import (
	"net/http"
	"net/netip"
)

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// No credentials configured → allow all (dev mode)
		if s.Username == "" && s.Password == "" {
			next(w, r)
			return
		}

		// Check HMAC-signed session cookie
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if username, ok := verifyCookie(s.authKey, cookie.Value); ok && username == s.Username {
				next(w, r)
				return
			}
		}

		// Basic Auth is for local API clients / scripts. Resolve the client IP
		// through configured proxies so loopback Caddy peers do not admit remote clients.
		ip, err := netip.ParseAddr(s.clientIP(r))
		localBasic := err == nil && ip.IsLoopback()
		if localBasic {
			user, pass, ok := r.BasicAuth()
			if ok && credentialsMatch(user, pass, s.Username, s.Password) {
				next(w, r)
				return
			}
		}

		// The Web UI entry point always uses the form login. Do not infer a
		// browser from Accept: subresource requests such as text/css also use
		// that header, and browser navigation headers vary.
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		if localBasic {
			w.Header().Set("WWW-Authenticate", `Basic realm="hayari"`)
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

package server

import (
	"net/http"
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

		// Check HTTP Basic Auth (for API clients / scripts)
		user, pass, ok := r.BasicAuth()
		if ok && credentialsMatch(user, pass, s.Username, s.Password) {
			next(w, r)
			return
		}

		// Redirect browser requests to login page
		accept := r.Header.Get("Accept")
		if r.Header.Get("X-Requested-With") == "" && len(accept) > 0 && accept[0] == 't' {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="hayari"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

package httpapi

import (
	"net/http"
	"runtime/debug"

	"patrol-platform/internal/domain"
)

// actorFromRequest extracts the authenticated actor from request headers.
// In production this would be replaced by JWT/session middleware; here we
// use simple headers so the system is self-contained.
func actorFromRequest(r *http.Request) domain.Actor {
	return domain.Actor{
		ID:        r.Header.Get("X-User-ID"),
		Role:      domain.Role(r.Header.Get("X-User-Role")),
		StationID: r.Header.Get("X-Station-ID"),
	}
}

// requireRole wraps a handler so it only proceeds when the caller's role
// matches one of the allowed values.
func (s *Server) requireRole(allowed domain.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if actor.ID == "" || actor.Role == "" {
			writeError(w, http.StatusUnauthorized, "missing authentication headers")
			return
		}
		if actor.Role != allowed {
			writeError(w, http.StatusForbidden, string(domain.ErrRoleForbidden.Error()))
			return
		}
		next(w, r)
	}
}

// loggingMiddleware logs each request at info level.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.clock.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", s.clock.Now().Sub(start),
		)
	})
}

// recoverMiddleware catches panics and returns a 500.
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic", "error", rec, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

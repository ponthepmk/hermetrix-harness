package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const authCookieName = "hermetrix_session"

type principalContextKey struct{}

type authenticator struct {
	tokenHash [32]byte
	cookieKey [32]byte
	principal string
	secure    bool
}

func newAuthenticator(token, principal string, secure bool) *authenticator {
	tokenHash := sha256.Sum256([]byte(token))
	cookieKey := sha256.Sum256([]byte("hermetrix-session-cookie-v1\x00" + token))
	return &authenticator{tokenHash: tokenHash, cookieKey: cookieKey, principal: principal, secure: secure}
}

// PrincipalFromContext returns the authenticated local/network identity. An
// empty value means authentication was not configured for this server.
func PrincipalFromContext(ctx context.Context) string {
	value, _ := ctx.Value(principalContextKey{}).(string)
	return value
}

func (a *authenticator) validToken(token string) bool {
	candidate := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(candidate[:], a.tokenHash[:]) == 1
}

func (a *authenticator) authorized(r *http.Request) bool {
	if scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " "); ok &&
		strings.EqualFold(scheme, "Bearer") && a.validToken(strings.TrimSpace(token)) {
		return true
	}
	cookie, err := r.Cookie(authCookieName)
	return err == nil && a.validCookie(cookie.Value, time.Now())
}

func (a *authenticator) cookieValue(expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, a.cookieKey[:])
	_, _ = mac.Write([]byte(payload + "\x00" + a.principal))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

func (a *authenticator) validCookie(value string, now time.Time) bool {
	payload, signature, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	expires, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || now.Unix() > expires {
		return false
	}
	expected := a.cookieValue(time.Unix(expires, 0))
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1 && signature != ""
}

func (a *authenticator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" || r.URL.Path == "/api/auth/session" || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if !a.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Hermetrix"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		if err := a.bindClaimedActor(r); err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("X-Hermetrix-Principal", a.principal)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, a.principal)))
	})
}

func (a *authenticator) session(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var input struct {
			Token string `json:"token"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || !a.validToken(input.Token) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid authentication token"})
			return
		}
		expires := time.Now().Add(12 * time.Hour)
		http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: a.cookieValue(expires), Path: "/",
			HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60, Expires: expires})
		writeJSON(w, http.StatusOK, map[string]string{"principal": a.principal})
	case http.MethodDelete:
		http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: "", Path: "/", HttpOnly: true,
			Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
	case http.MethodGet:
		if !a.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"principal": a.principal})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "unsupported authentication method"})
	}
}

// bindClaimedActor prevents an authenticated caller from writing a different
// actor into an audit receipt. It only inspects bounded JSON mutation bodies;
// large artifact imports carry no actor claim and stay on their streaming path.
func (a *authenticator) bindClaimedActor(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || r.Body == nil ||
		!strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") || r.ContentLength > 10<<20 {
		return nil
	}
	if r.ContentLength < 0 {
		return fmt.Errorf("authenticated JSON mutations require a Content-Length header")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (10<<20)+1))
	if err != nil {
		return fmt.Errorf("read authenticated request: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) > 10<<20 || len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return nil // the endpoint's strict decoder owns malformed JSON errors
	}
	claims := []string{}
	collectActorClaims(value, &claims)
	for _, claim := range claims {
		if claim != "" && claim != a.principal {
			return fmt.Errorf("actor %q does not match authenticated principal %q", claim, a.principal)
		}
	}
	return nil
}

func effectiveActor(ctx context.Context, claimed string) string {
	if principal := PrincipalFromContext(ctx); principal != "" {
		return principal
	}
	return claimed
}

func collectActorClaims(value any, claims *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "actor" {
				if claim, ok := child.(string); ok {
					*claims = append(*claims, strings.TrimSpace(claim))
				}
				continue
			}
			collectActorClaims(child, claims)
		}
	case []any:
		for _, child := range typed {
			collectActorClaims(child, claims)
		}
	}
}

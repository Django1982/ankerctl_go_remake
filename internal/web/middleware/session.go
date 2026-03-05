package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const sessionCookieName = "ankerctl_session"

type sessionPayload struct {
	Authenticated bool `json:"authenticated"`
}

// SessionManager manages signed authentication cookies.
type SessionManager struct {
	secretKey  []byte
	cookieName string
}

// NewSessionManager creates a SessionManager from a secret key.
func NewSessionManager(secretKey []byte) *SessionManager {
	copied := make([]byte, len(secretKey))
	copy(copied, secretKey)
	return &SessionManager{
		secretKey:  copied,
		cookieName: sessionCookieName,
	}
}

// SetAuthenticated sets or clears the authenticated session cookie.
func (sm *SessionManager) SetAuthenticated(w http.ResponseWriter, r *http.Request, value bool) {
	if !value {
		http.SetCookie(w, &http.Cookie{
			Name:     sm.cookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   false,
		})
		return
	}

	payload := sessionPayload{Authenticated: true}
	valueStr, err := sm.encode(payload)
	if err != nil {
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sm.cookieName,
		Value:    valueStr,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false,
	})
}

// IsAuthenticated checks whether the request carries a valid authenticated session.
func (sm *SessionManager) IsAuthenticated(r *http.Request) bool {
	if sm == nil || len(sm.secretKey) == 0 {
		return false
	}
	cookie, err := r.Cookie(sm.cookieName)
	if err != nil {
		return false
	}
	payload, err := sm.decode(cookie.Value)
	if err != nil {
		return false
	}
	return payload.Authenticated
}

func (sm *SessionManager) encode(payload sessionPayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	bodyEnc := base64.RawURLEncoding.EncodeToString(body)
	sig := sm.sign(bodyEnc)
	sigEnc := base64.RawURLEncoding.EncodeToString(sig)
	return bodyEnc + "." + sigEnc, nil
}

func (sm *SessionManager) decode(value string) (sessionPayload, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return sessionPayload{}, errors.New("invalid cookie format")
	}
	bodyEnc := parts[0]
	sigEnc := parts[1]

	sig, err := base64.RawURLEncoding.DecodeString(sigEnc)
	if err != nil {
		return sessionPayload{}, errors.New("invalid signature encoding")
	}
	expectedSig := sm.sign(bodyEnc)
	if len(sig) != len(expectedSig) || subtle.ConstantTimeCompare(sig, expectedSig) != 1 {
		return sessionPayload{}, errors.New("signature mismatch")
	}

	body, err := base64.RawURLEncoding.DecodeString(bodyEnc)
	if err != nil {
		return sessionPayload{}, errors.New("invalid payload encoding")
	}

	var payload sessionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return sessionPayload{}, errors.New("invalid payload JSON")
	}
	return payload, nil
}

func (sm *SessionManager) sign(bodyEnc string) []byte {
	mac := hmac.New(sha256.New, sm.secretKey)
	_, _ = mac.Write([]byte(bodyEnc))
	return mac.Sum(nil)
}

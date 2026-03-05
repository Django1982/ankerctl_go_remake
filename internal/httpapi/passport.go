package httpapi

import (
	"context"

	"github.com/django1982/ankerctl/internal/crypto"
)

// PassportV1 provides access to /v1/passport endpoints (profile).
type PassportV1 struct {
	*Client
}

// NewPassportV1 creates a PassportV1 API client.
func NewPassportV1(cfg ClientConfig) (*PassportV1, error) {
	c, err := NewClient(cfg, "/v1/passport")
	if err != nil {
		return nil, err
	}
	return &PassportV1{Client: c}, nil
}

// Profile fetches the current user profile.
func (p *PassportV1) Profile(ctx context.Context) (any, error) {
	if err := p.requireAuth(); err != nil {
		return nil, err
	}
	return p.Get(ctx, "/profile", p.AuthHeaders())
}

// PassportV2 provides access to /v2/passport endpoints (login).
type PassportV2 struct {
	*Client
}

// NewPassportV2 creates a PassportV2 API client.
func NewPassportV2(cfg ClientConfig) (*PassportV2, error) {
	c, err := NewClient(cfg, "/v2/passport")
	if err != nil {
		return nil, err
	}
	return &PassportV2{Client: c}, nil
}

// LoginResult holds the parsed login response data.
type LoginResult struct {
	AuthToken string `json:"auth_token"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Nickname  string `json:"nick_name"`
	Country   string `json:"invitation_code"` // country is in the response
}

// Login performs ECDH-encrypted cloud login.
// Python: AnkerHTTPPassportApiV2.login(email, password, captcha_id, captcha_answer)
func (p *PassportV2) Login(ctx context.Context, email, password string, captchaID, captchaAnswer *string) (any, error) {
	pubkeyHex, encryptedB64, err := crypto.EncryptLoginPassword([]byte(password))
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"App_name":    "anker_make",
		"App_version": "",
		"Model_type":  "PC",
		"Os_type":     "windows",
		"Os_version":  "10sp1",
	}

	data := map[string]any{
		"client_secret_info": map[string]any{
			"public_key": pubkeyHex,
		},
		"email":    email,
		"password": encryptedB64,
	}

	if captchaID != nil {
		data["captcha_id"] = *captchaID
	}
	if captchaAnswer != nil {
		data["answer"] = *captchaAnswer
	}

	return p.Post(ctx, "/login", headers, data)
}

package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// --- Test helpers ---

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func successJSON(data string) string {
	return `{"code":0,"data":` + data + `}`
}

// --- Client tests ---

func TestGtokenHeader(t *testing.T) {
	cfg := ClientConfig{UserID: "test-user-123", Region: "us"}
	c, err := NewClient(cfg, "/v1/test")
	if err != nil {
		t.Fatal(err)
	}
	// MD5 of "test-user-123"
	got := c.gtoken()
	if len(got) != 32 {
		t.Fatalf("gtoken length = %d, want 32", len(got))
	}
}

func TestGtokenEmpty(t *testing.T) {
	cfg := ClientConfig{Region: "us"}
	c, err := NewClient(cfg, "/v1/test")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.gtoken(); got != "" {
		t.Fatalf("gtoken = %q, want empty", got)
	}
}

func TestNewClient_InvalidRegion(t *testing.T) {
	cfg := ClientConfig{Region: "invalid"}
	_, err := NewClient(cfg, "/v1/test")
	if err == nil {
		t.Fatal("expected error for invalid region")
	}
}

func TestNewClient_BaseURL(t *testing.T) {
	cfg := ClientConfig{BaseURL: "https://custom.example.com"}
	c, err := NewClient(cfg, "/v1/app")
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "https://custom.example.com" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
}

func TestClient_UnwrapSuccess(t *testing.T) {
	cfg := ClientConfig{Region: "us", AuthToken: "tok", UserID: "uid"}
	c, err := NewClient(cfg, "/v1/app")
	if err != nil {
		t.Fatal(err)
	}

	c.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Verify Gtoken header is set.
		if req.Header.Get("Gtoken") == "" {
			t.Fatal("missing Gtoken header")
		}
		return jsonResponse(200, successJSON(`{"printers":["M5"]}`)), nil
	}))

	data, err := c.Post(context.Background(), "/query_fdm_list", c.AuthHeaders(), nil)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	m, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", data)
	}
	if _, ok := m["printers"]; !ok {
		t.Fatal("missing printers in data")
	}
}

func TestClient_UnwrapAPIError(t *testing.T) {
	cfg := ClientConfig{Region: "us"}
	c, err := NewClient(cfg, "/v1/app")
	if err != nil {
		t.Fatal(err)
	}

	c.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"code":1001,"msg":"bad request"}`), nil
	}))

	_, err = c.Post(context.Background(), "/test", nil, nil)
	if err == nil {
		t.Fatal("expected API error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.JSON == nil {
		t.Fatal("expected JSON in API error")
	}
}

func TestClient_HTTPError(t *testing.T) {
	cfg := ClientConfig{Region: "us"}
	c, err := NewClient(cfg, "/v1/app")
	if err != nil {
		t.Fatal(err)
	}

	c.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(500, `{"error":"internal"}`), nil
	}))

	_, err = c.Post(context.Background(), "/test", nil, nil)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestClient_RequireAuth(t *testing.T) {
	cfg := ClientConfig{Region: "us"} // no auth token
	c, err := NewClient(cfg, "/v1/app")
	if err != nil {
		t.Fatal(err)
	}

	err = c.requireAuth()
	if err == nil {
		t.Fatal("expected auth error")
	}
}

// --- Seccode tests ---

func TestCalcCheckCode(t *testing.T) {
	// Python: calc_check_code("ABCDEF1234", "aabbccddeeff")
	// = md5("ABCDEF1234+1234+aabbccddeeff")
	got := CalcCheckCode("ABCDEF1234", "aabbccddeeff")
	if len(got) != 32 {
		t.Fatalf("check code length = %d, want 32", len(got))
	}
	if got == "" {
		t.Fatal("empty check code")
	}
}

func TestCalcCheckCode_ShortSN(t *testing.T) {
	got := CalcCheckCode("AB", "aabb")
	if got != "" {
		t.Fatalf("expected empty for short SN, got %q", got)
	}
}

func TestCalHwIDSuffix(t *testing.T) {
	// "aabbccddeeff" -> f(15) + f(15) + e(14) + e(14) = 58
	got := CalHwIDSuffix("aabbccddeeff")
	want := 15 + 15 + 14 + 14
	if got != want {
		t.Fatalf("CalHwIDSuffix = %d, want %d", got, want)
	}
}

func TestGenBaseCode(t *testing.T) {
	got := GenBaseCode("ABCDEF1234", "aabbccddeeff")
	if got == "" {
		t.Fatal("empty base code")
	}
	// Should contain digits from suffix at the end.
	if !strings.ContainsAny(got, "0123456789") {
		t.Fatalf("base code missing numeric suffix: %q", got)
	}
}

func TestGenRandSeed(t *testing.T) {
	secTS, secCode := GenRandSeed("aabbccddeeff")
	if !strings.HasPrefix(secTS, "01") {
		t.Fatalf("secTS prefix: %q", secTS)
	}
	if len(secCode) != 32 {
		t.Fatalf("secCode length = %d, want 32", len(secCode))
	}
	// secCode should be uppercase hex.
	if strings.ToUpper(secCode) != secCode {
		t.Fatalf("secCode not uppercase: %q", secCode)
	}
}

func TestCreateCheckCodeV1(t *testing.T) {
	secTS, secCode := CreateCheckCodeV1("ABCDEF1234567890", "aabbccddeeff")
	if !strings.HasPrefix(secTS, "01") {
		t.Fatalf("secTS prefix: %q", secTS)
	}
	if len(secCode) != 32 {
		t.Fatalf("secCode length = %d, want 32", len(secCode))
	}
}

// --- Passport tests ---

func TestPassportV2_Login(t *testing.T) {
	var gotBody map[string]any
	var gotHeaders http.Header

	cfg := ClientConfig{Region: "us"}
	p, err := NewPassportV2(cfg)
	if err != nil {
		t.Fatal(err)
	}

	p.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotHeaders = req.Header
		_ = json.NewDecoder(req.Body).Decode(&gotBody)
		return jsonResponse(200, successJSON(`{"auth_token":"tok123","user_id":"u1"}`)), nil
	}))

	data, err := p.Login(context.Background(), "test@example.com", "secret123", nil, nil)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if data == nil {
		t.Fatal("nil data")
	}

	// Verify headers match Python's AnkerHTTPPassportApiV2._post.
	if gotHeaders.Get("App_name") != "anker_make" {
		t.Fatalf("App_name header = %q", gotHeaders.Get("App_name"))
	}
	// Verify body has required fields.
	if gotBody["email"] != "test@example.com" {
		t.Fatalf("email = %v", gotBody["email"])
	}
	if gotBody["password"] == nil || gotBody["password"] == "" {
		t.Fatal("missing encrypted password")
	}
	secretInfo, ok := gotBody["client_secret_info"].(map[string]any)
	if !ok {
		t.Fatal("missing client_secret_info")
	}
	if secretInfo["public_key"] == nil || secretInfo["public_key"] == "" {
		t.Fatal("missing public_key")
	}
}

// --- App tests ---

func TestAppV1_QueryFDMList(t *testing.T) {
	cfg := ClientConfig{Region: "us", AuthToken: "tok", UserID: "uid"}
	a, err := NewAppV1(cfg)
	if err != nil {
		t.Fatal(err)
	}

	a.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Auth-Token") != "tok" {
			t.Fatal("missing X-Auth-Token")
		}
		return jsonResponse(200, successJSON(`[{"sn":"TEST123"}]`)), nil
	}))

	data, err := a.QueryFDMList(context.Background())
	if err != nil {
		t.Fatalf("QueryFDMList: %v", err)
	}
	if data == nil {
		t.Fatal("nil data")
	}
}

func TestAppV1_RequiresAuth(t *testing.T) {
	cfg := ClientConfig{Region: "us"} // no auth
	a, err := NewAppV1(cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.QueryFDMList(context.Background())
	if err == nil {
		t.Fatal("expected auth error")
	}
}

// --- Region tests ---

func TestGuessRegion_FunctionExists(t *testing.T) {
	// Verify GuessRegion is callable. We don't call it in tests to avoid
	// real network connections; the implementation is straightforward and
	// uses the mockable Dialer variable.
	_ = GuessRegion
}

func TestMeasureConnectTime_WithDialer(t *testing.T) {
	origDialer := Dialer
	defer func() { Dialer = origDialer }()

	// Mock dialer that returns immediately.
	Dialer = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		// Use a pipe to get a closeable connection.
		c1, _ := net.Pipe()
		return c1, nil
	}

	d := measureConnectTime("example.com", 443)
	if d >= regionConnectTimeout {
		t.Fatalf("expected fast connect, got %v", d)
	}
}

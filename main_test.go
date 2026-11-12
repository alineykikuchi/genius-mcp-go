package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// headerRT acrescenta cabeçalhos fixos a toda requisição, como o Genius faz.
type headerRT struct {
	h  http.Header
	rt http.RoundTripper
}

func (t headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.h {
		r.Header[k] = v
	}
	return t.rt.RoundTrip(r)
}

func fakeJWT(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]any{"alg": "none", "typ": "JWT"}) + "." + enc(claims) + ".assinatura"
}

func newTestServer(t *testing.T, authMode string) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(newMux(authMode, log))
	t.Cleanup(ts.Close)
	return ts
}

func connect(t *testing.T, url string, h http.Header) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "teste", Version: "0"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint:   url + "/mcp",
		HTTPClient: &http.Client{Transport: headerRT{h: h, rt: http.DefaultTransport}},
	}
	cs, err := client.Connect(context.Background(), tr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callJSON(t *testing.T, cs *mcp.ClientSession, name string, args any) map[string]any {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("call %s devolveu erro: %+v", name, res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func TestHealthz(t *testing.T) {
	ts := newTestServer(t, "require")
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", err, resp)
	}
}

func TestSemTokenDa401(t *testing.T) {
	ts := newTestServer(t, "require")
	resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("esperava 401, veio %d", resp.StatusCode)
	}
}

func TestTokenExpiradoDa401(t *testing.T) {
	ts := newTestServer(t, "require")
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+fakeJWT(map[string]any{"sub": "x", "exp": time.Now().Add(-time.Hour).Unix()}))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("esperava 401, veio %d", resp.StatusCode)
	}
}

func TestToolsComJWT(t *testing.T) {
	ts := newTestServer(t, "require")
	jwt := fakeJWT(map[string]any{
		"sub": "123", "email": "compradora@exemplo.com", "tenant_id": "1804160",
		"exp": time.Now().Add(time.Hour).Unix(), "segredo": "nao-deve-aparecer",
	})
	h := http.Header{}
	h.Set("Authorization", "Bearer "+jwt)
	h.Set("X-ME-CORRELATION-ID", "abc-123")
	cs := connect(t, ts.URL, h)

	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tl := range list.Tools {
		got[tl.Name] = true
	}
	if !got["ping"] || !got["quem_sou_eu"] {
		t.Fatalf("tools faltando: %v", got)
	}

	p := callJSON(t, cs, "ping", map[string]any{"mensagem": "oi"})
	if p["resposta"] != "pong" || p["eco"] != "oi" {
		t.Fatalf("ping: %v", p)
	}

	w := callJSON(t, cs, "quem_sou_eu", map[string]any{})
	if w["autenticado"] != true || w["formato_token"] != "jwt" || w["usuario"] != "123" {
		t.Fatalf("quem_sou_eu: %v", w)
	}
	claims := w["claims"].(map[string]any)
	if claims["tenant_id"] != "1804160" {
		t.Fatalf("tenant_id faltando: %v", claims)
	}
	if _, vazou := claims["segredo"]; vazou {
		t.Fatal("claim não listada vazou")
	}
	if strings.Contains(w["token_mascarado"].(string), jwt[10:30]) {
		t.Fatal("token não foi mascarado")
	}
	if w["cabecalhos_me"].(map[string]any)["X-Me-Correlation-Id"] != "abc-123" {
		t.Fatalf("cabeçalho X-ME faltando: %v", w["cabecalhos_me"])
	}
}

func TestTokenOpaco(t *testing.T) {
	ts := newTestServer(t, "require")
	h := http.Header{}
	h.Set("Authorization", "Bearer tokenopaco1234567890")
	cs := connect(t, ts.URL, h)
	w := callJSON(t, cs, "quem_sou_eu", map[string]any{})
	if w["formato_token"] != "opaco" || w["autenticado"] != true {
		t.Fatalf("opaco: %v", w)
	}
}

func TestAuthOff(t *testing.T) {
	ts := newTestServer(t, "off")
	cs := connect(t, ts.URL, http.Header{})
	w := callJSON(t, cs, "quem_sou_eu", map[string]any{})
	if w["autenticado"] != false {
		t.Fatalf("auth off: %v", w)
	}
}

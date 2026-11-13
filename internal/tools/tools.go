// Package tools registra as tools do MCP. Para criar uma tool nova, siga o
// modelo de Ping (sem autenticação) ou de WhoAmI (usa o token do usuário).
package tools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"genius-mcp-go/internal/datahub"
	"genius-mcp-go/internal/token"
)

// Register adiciona todas as tools ao servidor.
func Register(s *mcp.Server, version string) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ping",
		Description: "Testa se o MCP está no ar. Devolve 'pong', a mensagem recebida, a versão e a hora do servidor.",
	}, ping(version))

	mcp.AddTool(s, &mcp.Tool{
		Name: "quem_sou_eu",
		Description: "Mostra quem é o usuário que está usando o Genius, a partir do token que o Genius repassa ao MCP " +
			"(formato do token, usuário, validade, claims de identificação e cabeçalhos X-ME-* recebidos). " +
			"Use quando pedirem para testar a autenticação ou 'quem sou eu'.",
	}, whoAmI)

	deps := co2Deps{
		dh:           datahub.NewFromEnv(),
		http:         &http.Client{Timeout: 15 * time.Second},
		tenantClaim:  os.Getenv("TENANT_CLAIM"),
		tenantFiltro: strings.ToLower(os.Getenv("TENANT_FILTRO")),
	}
	registerCO2(s, deps)
	registerRecomendacao(s, deps)
}

// ---- ping ------------------------------------------------------------------

type PingIn struct {
	Mensagem string `json:"mensagem,omitempty" jsonschema:"texto opcional que volta na resposta"`
}

type PingOut struct {
	Resposta string `json:"resposta"`
	Eco      string `json:"eco,omitempty"`
	Versao   string `json:"versao"`
	HoraUTC  string `json:"hora_utc"`
}

func ping(version string) mcp.ToolHandlerFor[PingIn, PingOut] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in PingIn) (*mcp.CallToolResult, PingOut, error) {
		return nil, PingOut{
			Resposta: "pong",
			Eco:      in.Mensagem,
			Versao:   version,
			HoraUTC:  time.Now().UTC().Format(time.RFC3339),
		}, nil
	}
}

// ---- quem_sou_eu -----------------------------------------------------------

type WhoAmIIn struct{}

type WhoAmIOut struct {
	Autenticado  bool              `json:"autenticado"`
	FormatoToken string            `json:"formato_token,omitempty"`
	TokenMascar  string            `json:"token_mascarado,omitempty"`
	Usuario      string            `json:"usuario,omitempty"`
	ExpiraEm     string            `json:"expira_em,omitempty"`
	Claims       map[string]any    `json:"claims,omitempty"`
	CabecalhosME map[string]string `json:"cabecalhos_me,omitempty"`
	Observacao   string            `json:"observacao,omitempty"`
}

// Claims seguras de devolver: identificação, sem nada sensível.
var claimsSeguras = []string{"sub", "user_id", "userId", "email", "name", "preferred_username", "iss", "aud", "client_id", "azp", "iat", "exp"}

// Claims cujo nome contém estes termos também saem (tenant, workflow, borg…).
var termosCliente = []string{"tenant", "client", "workflow", "borg", "main", "profile", "company", "empresa"}

func whoAmI(_ context.Context, req *mcp.CallToolRequest, _ WhoAmIIn) (*mcp.CallToolResult, WhoAmIOut, error) {
	out := WhoAmIOut{}
	extra := req.GetExtra()
	if extra == nil {
		return nil, out, fmt.Errorf("requisição sem contexto HTTP")
	}
	out.CabecalhosME = meHeaders(extra.Header)

	ti := extra.TokenInfo
	if ti == nil {
		out.Observacao = "Nenhum token recebido. Confira se o MCP está cadastrado com credential_mode me_access_token " +
			"e se AUTH_MODE não está 'off'."
		return nil, out, nil
	}

	out.Autenticado = true
	out.Usuario = ti.UserID
	if !ti.Expiration.IsZero() {
		out.ExpiraEm = ti.Expiration.UTC().Format(time.RFC3339)
	}
	if f, ok := ti.Extra[token.ExtraFormat].(string); ok {
		out.FormatoToken = f
	}
	if raw, ok := ti.Extra[token.ExtraRaw].(string); ok {
		out.TokenMascar = token.Mask(raw)
	}
	if c, ok := ti.Extra[token.ExtraClaims].(map[string]any); ok {
		out.Claims = filtrarClaims(c)
	}
	if out.FormatoToken == "opaco" {
		out.Observacao = "Token opaco: não dá para ler o usuário daqui; as APIs da ME validam o token quando ele é repassado."
	}
	return nil, out, nil
}

func filtrarClaims(c map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range claimsSeguras {
		if v, ok := c[k]; ok {
			out[k] = v
		}
	}
	for k, v := range c {
		lk := strings.ToLower(k)
		for _, t := range termosCliente {
			if strings.Contains(lk, t) {
				out[k] = v
				break
			}
		}
	}
	return out
}

// meHeaders devolve os cabeçalhos X-ME-* recebidos (úteis para rastrear a chamada).
func meHeaders(h map[string][]string) map[string]string {
	out := map[string]string{}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lk := strings.ToLower(k)
		if !strings.HasPrefix(lk, "x-me-") || len(h[k]) == 0 {
			continue
		}
		v := h[k][0]
		if strings.Contains(lk, "token") || strings.Contains(lk, "auth") || strings.Contains(lk, "secret") || strings.Contains(lk, "key") {
			v = token.Mask(v)
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Package token lê o token que o Genius repassa ao MCP.
//
// Quando o MCP é cadastrado no Genius Backoffice com credential_mode
// me_access_token, o Genius envia o token do usuário logado na ME em
// "Authorization: Bearer <token>". Este pacote extrai as claims desse token
// para uso das tools (quem é o usuário, de qual cliente etc.).
//
// ATENÇÃO: a assinatura do JWT NÃO é verificada aqui. Isso é aceitável enquanto
// as tools só ecoam dados ou repassam o token para APIs da ME (que validam o
// token elas mesmas). Antes de criar uma tool que confie nas claims para
// autorizar algo localmente, valide a assinatura contra o JWKS do IdP da ME.
package token

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// Chave em TokenInfo.Extra onde ficam as claims e o token bruto.
const (
	ExtraClaims = "claims"
	ExtraRaw    = "raw"
	ExtraFormat = "format" // "jwt" ou "opaco"
)

// Verifier é um auth.TokenVerifier que aceita JWT (lendo as claims) ou token
// opaco. Recusa JWT expirado ou malformado.
func Verifier(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
	info := &auth.TokenInfo{Extra: map[string]any{ExtraRaw: raw}}

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		// Token opaco: não há o que decodificar; quem valida é a API da ME.
		info.Extra[ExtraFormat] = "opaco"
		return info, nil
	}

	claims, err := decodeClaims(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: jwt malformado: %v", auth.ErrInvalidToken, err)
	}
	info.Extra[ExtraFormat] = "jwt"
	info.Extra[ExtraClaims] = claims

	if exp, ok := numberClaim(claims, "exp"); ok {
		info.Expiration = time.Unix(int64(exp), 0)
	}
	for _, k := range []string{"sub", "user_id", "userId", "email"} {
		if v, ok := claims[k].(string); ok && v != "" {
			info.UserID = v
			break
		}
	}
	info.Scopes = scopes(claims)
	return info, nil
}

func decodeClaims(seg string) (map[string]any, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(seg, "="))
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(b, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func numberClaim(c map[string]any, k string) (float64, bool) {
	switch v := c[k].(type) {
	case float64:
		return v, true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

func scopes(c map[string]any) []string {
	switch v := c["scope"].(type) {
	case string:
		return strings.Fields(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// Mask devolve só o começo e o fim de um segredo, para logs e respostas.
func Mask(s string) string {
	if len(s) <= 12 {
		return "***"
	}
	return s[:6] + "…" + s[len(s)-4:]
}

// genius-mcp-go: servidor MCP mínimo para ligar a um agente do Genius (ME).
//
// Rotas:
//
//	POST /mcp      endpoint MCP (Streamable HTTP, sem estado)
//	GET  /healthz  saúde, para o Railway
//
// Variáveis de ambiente:
//
//	PORT       porta HTTP (o Railway define sozinho; padrão 8080)
//	AUTH_MODE  "require" (padrão): exige Authorization: Bearer <token>
//	           "off": não exige token (só para teste local)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"genius-mcp-go/internal/token"
	"genius-mcp-go/internal/tools"
)

// version é preenchida no build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := envOr("PORT", "8080")
	authMode := envOr("AUTH_MODE", "require")

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newMux(authMode, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("genius-mcp-go no ar", "porta", port, "auth_mode", authMode, "versao", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("servidor parou", "erro", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Info("encerrado")
}

func newMux(authMode string, log *slog.Logger) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "genius-mcp-go", Version: version}, nil)
	tools.Register(server, version)

	var mcpHandler http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, Logger: log},
	)
	if authMode != "off" {
		mcpHandler = auth.RequireBearerToken(token.Verifier, &auth.RequireBearerTokenOptions{
			AllowMissingExpiration: true, // token opaco não tem exp
			ClockSkew:              time.Minute,
		})(mcpHandler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","versao":"` + version + `"}`))
	})
	mux.Handle("/mcp", logRequests(mcpHandler, log))
	return mux
}

// logRequests registra método, status e duração. Nunca registra o token.
func logRequests(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("mcp",
			"metodo", r.Method,
			"status", rec.status,
			"ms", time.Since(start).Milliseconds(),
			"tem_token", r.Header.Get("Authorization") != "",
			"correlation_id", r.Header.Get("X-ME-CORRELATION-ID"),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush mantém o streaming (SSE) funcionando através do middleware.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

package api

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"github.com/google/go-github/v50/github"
	"github.com/gorilla/mux"
	"go.lumeweb.com/portal-plugin-frontend/internal"
	"go.uber.org/zap"
	"io"
	"net/http"
	"net/url"
	"strings"

	pluginCfg "go.lumeweb.com/portal-plugin-frontend/internal/config"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/middleware"
	portal_frontend "go.lumeweb.com/web/go/portal-frontend"
)

var _ core.API = (*API)(nil)

type API struct {
	ctx    core.Context
	config config.Manager
	logger *core.Logger
}

func (a *API) Config() config.APIConfig {
	return &pluginCfg.APIConfig{}
}

func (a *API) Name() string {
	return internal.PLUGIN_NAME
}

func NewAPI() (*API, []core.ContextBuilderOption, error) {
	api := &API{}

	opts := core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			api.ctx = ctx
			api.config = ctx.Config()
			api.logger = ctx.APILogger(api)

			return nil
		}),
	)

	return api, opts, nil
}

func (a *API) Configure(router *mux.Router, _ core.AccessService) error {
	// Middleware setup
	corsHandler := middleware.CorsMiddleware(nil)

	router.Use(corsHandler)

	var httpHandler http.Handler
	cfg := a.config.GetAPI(internal.PLUGIN_NAME).(*pluginCfg.APIConfig)
	if cfg.GitRepo != "" {
		u, err := url.Parse(cfg.GitRepo)
		if err != nil {
			return err
		}
		path := u.Path
		path = strings.TrimPrefix(path, "/")
		path = strings.TrimSuffix(path, "/")
		components := strings.Split(path, "/")
		if len(components) < 2 {
			return fmt.Errorf("invalid Git repository URL: %s", cfg.GitRepo)
		}
		owner := components[0]
		repo := components[1]

		client := github.NewClient(nil)
		release, _, err := client.Repositories.GetLatestRelease(context.Background(), owner, repo)
		if err != nil {
			return err
		}
		zipURL := *release.ZipballURL
		resp, err := http.Get(zipURL)
		if err != nil {
			return err
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				a.logger.Error("failed to close response body", zap.Error(err))
			}
		}()

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		byteReader := bytes.NewReader(buf)
		zipFs, err := zip.NewReader(byteReader, int64(byteReader.Len()))
		if err != nil {
			return err
		}

		httpHandler = http.FileServer(http.FS(zipFs))
	} else {
		httpHandler = portal_frontend.Handler()
	}

	router.PathPrefix("/assets/").Handler(httpHandler)
	router.PathPrefix("/").MatcherFunc(func(r *http.Request, rm *mux.RouteMatch) bool {
		return !strings.HasPrefix(r.URL.Path, "/api/")
	}).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/"
		httpHandler.ServeHTTP(w, r)
	}))

	return nil
}

func (a *API) Subdomain() string {
	return ""
}

func (a *API) AuthTokenName() string {
	return core.AUTH_COOKIE_NAME
}

package api

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v50/github"
	"go.lumeweb.com/portal-plugin-frontend/internal"
	pluginConfig "go.lumeweb.com/portal-plugin-frontend/internal/config"
	"go.lumeweb.com/portal-router"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	portal_frontend "go.lumeweb.com/web/go/portal-frontend"
	"go.uber.org/zap"
)

var _ core.API = (*API)(nil)

type API struct {
	ctx    core.Context
	config config.Manager
	logger *core.Logger
}

func (a *API) Config() config.APIConfig {
	return &pluginConfig.APIConfig{}
}

func (a *API) Name() string {
	return internal.PLUGIN_NAME
}

func (a *API) OpenAPIInfo() router.APIInfoDefinition {
	return router.APIInfo().
		Title("Frontend API").
		Description("API for serving the frontend application").
		Version("0.1.0")
}

func NewAPI() (core.API, []core.ContextBuilderOption, error) {
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

func (a *API) Configure(gRouter router.Router, _ core.AccessService) error {
	cfg := a.config.GetAPI(internal.PLUGIN_NAME).(*pluginConfig.APIConfig)

	var fsHandler fs.FS
	if cfg.GitRepo != "" {
		handler, err := a.createGitHubFS(cfg.GitRepo)
		if err != nil {
			return err
		}
		fsHandler = handler
	} else {
		fsHandler = portal_frontend.GetFS()
	}

	echoRouter := router.GetRouter(gRouter)
	if echoRouter == nil {
		return fmt.Errorf("failed to get echo router")
	}

	router.MustMPASetupWithAssets(gRouter, fsHandler, "_astro")
	return nil
}

func (a *API) createGitHubFS(gitRepo string) (fs.FS, error) {
	u, err := url.Parse(gitRepo)
	if err != nil {
		return nil, err
	}
	path := u.Path
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	components := strings.Split(path, "/")
	if len(components) < 2 {
		return nil, fmt.Errorf("invalid Git repository URL: %s", gitRepo)
	}
	owner := components[0]
	repo := components[1]

	client := github.NewClient(nil)
	release, _, err := client.Repositories.GetLatestRelease(context.Background(), owner, repo)
	if err != nil {
		return nil, err
	}
	zipURL := *release.ZipballURL
	resp, err := http.Get(zipURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			a.logger.Error("failed to close response body", zap.Error(err))
		}
	}()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	byteReader := bytes.NewReader(buf)
	zipFs, err := zip.NewReader(byteReader, int64(byteReader.Len()))
	if err != nil {
		return nil, err
	}

	return zipFs, nil
}

func (a *API) Subdomain() string {
	return ""
}

func (a *API) AuthTokenName() string {
	return core.AUTH_COOKIE_NAME
}

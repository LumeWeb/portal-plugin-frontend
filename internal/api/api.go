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
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/v50/github"
	"go.lumeweb.com/portal-plugin-frontend/internal"
	pluginConfig "go.lumeweb.com/portal-plugin-frontend/internal/config"
	"go.lumeweb.com/portal-router"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	portal_frontend "go.lumeweb.com/web/go/portal-frontend"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const (
	GitHubHost     = "github.com"
	AstroAssetsDir = "_astro"
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

	// Ensure fsHandler is rooted at the app directory (e.g., <ziproot>/dist when using Astro)
	router.MustMPASetupWithAssets(gRouter, fsHandler, AstroAssetsDir)
	return nil
}

func (a *API) createGitHubFS(gitRepo string) (fs.FS, error) {
	u, err := url.Parse(gitRepo)
	if err != nil {
		return nil, err
	}
	
	if u.Host != GitHubHost {
		return nil, fmt.Errorf("only %s repos are supported: %s", GitHubHost, gitRepo)
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

	// Build a GitHub client with auth if GITHUB_TOKEN is present and set sane timeouts.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	httpClient := &http.Client{Timeout: 60 * time.Second}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient = oauth2.NewClient(ctx, ts)
		// Ensure the OAuth2 client still has a sane timeout.
		httpClient.Timeout = 60 * time.Second
	}
	client := github.NewClient(httpClient)
	
	release, _, err := client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	
	if release.ZipballURL == nil || *release.ZipballURL == "" {
		return nil, fmt.Errorf("latest release for %s/%s has no zipball URL", owner, repo)
	}
	
	req, err := http.NewRequestWithContext(ctx, "GET", *release.ZipballURL, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "portal-plugin-frontend")
	// Use the same (possibly OAuth2) httpClient for the zipball request.
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	
	defer func() {
		if err := resp.Body.Close(); err != nil {
			a.logger.Error("failed to close response body", zap.Error(err))
		}
	}()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status fetching zipball: %s", resp.Status)
	}

	// Cap the download to a reasonable size (200 MiB) to avoid OOM on malformed releases.
	const maxZipSize = 200 << 20
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxZipSize))
	if err != nil {
		return nil, err
	}
	
	byteReader := bytes.NewReader(buf)
	zipFs, err := zip.NewReader(byteReader, int64(byteReader.Len()))
	if err != nil {
		return nil, err
	}
	
	// Sub-root: handle top-level dir and optional dist/ for Astro builds.
	// Prefer <top>/dist if present, else <top>.
	entries, err := fs.ReadDir(zipFs, ".")
	if err != nil {
		return nil, err
	}
	
	if len(entries) == 1 && entries[0].IsDir() {
		top := entries[0].Name()
		if _, err := fs.Stat(zipFs, path.Join(top, "dist")); err == nil {
			if sub, err := fs.Sub(zipFs, path.Join(top, "dist")); err == nil {
				return sub, nil
			}
		}
		if sub, err := fs.Sub(zipFs, top); err == nil {
			return sub, nil
		}
	}
	
	return zipFs, nil
}

func (a *API) Subdomain() string {
	return ""
}

func (a *API) AuthTokenName() string {
	return core.AUTH_COOKIE_NAME
}

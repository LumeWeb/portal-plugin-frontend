package config

import (
	"go.lumeweb.com/portal/config"
)

const PLUGIN_NAME = "frontend"

var _ config.APIConfig = (*APIConfig)(nil)

type APIConfig struct {
	GitRepo string `config:"git_repo"`
}

func (A APIConfig) Defaults() map[string]any {
	return map[string]any{}
}

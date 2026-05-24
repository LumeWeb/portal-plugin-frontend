package config

import (
	"fmt"
	"net/url"

	"go.lumeweb.com/portal/config"
)

var _ config.APIConfig = (*APIConfig)(nil)

type APIConfig struct {
	GitRepo string `config:"git_repo"`
	Gateway string `config:"gateway"`
}

func (A APIConfig) Defaults() map[string]any {
	return map[string]any{}
}

func (A APIConfig) Validate() error {
	if A.Gateway != "" && A.GitRepo != "" {
		return fmt.Errorf("gateway and git_repo are mutually exclusive — set only one")
	}
	if A.Gateway != "" {
		u, err := url.Parse(A.Gateway)
		if err != nil {
			return fmt.Errorf("gateway must be a valid URL with scheme and host")
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("gateway must be a valid URL with scheme and host")
		}
	}
	return nil
}

package main

import (
	"go.lumeweb.com/portal-plugin-frontend/build"
	"go.lumeweb.com/portal-plugin-frontend/internal"
	"go.lumeweb.com/portal-plugin-frontend/internal/api"
	"go.lumeweb.com/portal/core"
)

func init() {
	core.RegisterPlugin(core.PluginInfo{
		ID:      internal.PLUGIN_NAME,
		Version: build.GetInfo(),
		API: func() (core.API, []core.ContextBuilderOption, error) {
			return api.NewAPI()
		},
	})
}

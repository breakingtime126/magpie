package gui

import (
	"net/http"

	"github.com/yetone/magpie/internal/codexsetup"
)

// NativeCodexAPI composes the isolated native controller into the existing GUI.
// Construction does not read saved providers, launch native, migrate history,
// or invoke ordinary Connect/Sync. Its owner retains controller Close ownership.
type NativeCodexAPI struct {
	actions *codexsetup.ActionsHandler
}

type NativeCodexBoot struct {
	NativeCodexPreview bool   `json:"nativeCodexPreview"`
	NativeCodexToken   string `json:"nativeCodexToken"`
}

func NewNativeCodexAPI(controller codexsetup.Controller, choose func() (string, error)) (*NativeCodexAPI, error) {
	actions, err := codexsetup.NewActionsHandler(controller, choose)
	if err != nil {
		return nil, err
	}
	return &NativeCodexAPI{actions: actions}, nil
}

// Boot returns fields for JSON encoding into the owning Wails window only.
// Do not interpolate them into JavaScript source or publish/cache this response.
func (a *NativeCodexAPI) Boot() NativeCodexBoot {
	return NativeCodexBoot{NativeCodexPreview: true, NativeCodexToken: a.actions.Token()}
}

func (a *NativeCodexAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.actions.ServeHTTP(w, r)
}

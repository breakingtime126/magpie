package main

import (
	"reflect"
	"testing"
)

// Copy this file alone to 643cc5d060659c68c75e342e7e0fd71590b6993c for an
// assertion-level red. It uses no newly added production symbols.
const desktopPluginLiteral = "plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true"

func TestDesktopPluginLiteralReachesLaunchOptions(t *testing.T) {
	for _, flag := range []string{"-c", "--config"} {
		for _, args := range [][]string{
			{flag, desktopPluginLiteral, "app-server"},
			{"app-server", flag, desktopPluginLiteral},
			{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled", flag, desktopPluginLiteral},
		} {
			op, options, _, err := parseArgs(args)
			if err != nil || op != serve {
				t.Fatalf("exact reviewed Desktop plugin literal must reach normal launch: %v", err)
			}
			field := reflect.ValueOf(options).FieldByName("CodexAppToolsMCPEnabled")
			if !field.IsValid() || field.Kind() != reflect.Bool || !field.Bool() {
				t.Fatal("exact reviewed Desktop plugin literal lost its typed launch option")
			}
		}
	}
}

package main

import (
	"sort"
	"strings"
	"testing"
)

func TestFrozenSchemaKeysRemainFinite(t *testing.T) {
	if len(observedSchemaKeys) != 830 || len(observedConfigTypes) != 830 {
		t.Fatal("unexpected frozen property path count")
	}
	if !sort.StringsAreSorted(observedSchemaKeys[:]) {
		t.Fatal("frozen property paths are not sorted")
	}
	seen := make(map[string]bool)
	roots, opaque, quoted := 0, 0, 0
	for _, key := range observedSchemaKeys {
		if seen[key] {
			t.Fatal("duplicate frozen property path")
		}
		seen[key] = true
		if !strings.Contains(key, ".") {
			roots++
		}
		kind, exists := observedConfigTypes[key]
		if !exists {
			t.Fatal("frozen path missing from metadata map")
		}
		if kind == "opaque" {
			opaque++
		}
		if strings.Contains(key, "\"") {
			quoted++
			if key != "notice.\"hide_gpt-5.1-codex-max_migration_prompt\"" {
				t.Fatal("unexpected quoted property path")
			}
			if _, valid := observedConfigKey(key); valid {
				t.Fatal("dotted quoted property acquired ambiguous recognition")
			}
		} else if normalized, valid := observedConfigKey(key); !valid || normalized != key {
			t.Fatal("simple frozen property path cannot be recognized")
		}
	}
	if roots != 103 || opaque != 596 || quoted != 1 {
		t.Fatal("unexpected frozen property path partition")
	}
}

// This independent snapshot protects every pre-expansion type, including enum
// domains. It is copied from baseline 9c69ae37750e25545657f5260e0e903fad7c7810,
// config_metadata.go blob 3c8056881367365a1416eed7c81dd78b0667867b.
func TestLegacyConfigMetadataTypesRemainUnchanged(t *testing.T) {
	legacy := [][2]string{
		{"allow_login_shell", "bool"},
		{"allow_symlinked_codex_home", "bool"},
		{"analytics.enabled", "bool"},
		{"check_for_update_on_startup", "bool"},
		{"cli_auth_credentials_store", "cli_store"},
		{"daybreak", "bool"},
		{"disable_paste_burst", "bool"},
		{"experimental_use_unified_exec_tool", "bool"},
		{"features.agent_message_board", "bool"},
		{"features.analytics_plan_history", "bool"},
		{"features.api_key_cyber_access_programs", "bool"},
		{"features.api_key_model_discovery", "bool"},
		{"features.app_server_websocket_compression", "bool"},
		{"features.apply_patch_freeform", "bool"},
		{"features.apply_patch_preserve_line_endings", "bool"},
		{"features.apply_patch_streaming_events", "bool"},
		{"features.apps", "bool"},
		{"features.apps_early_resource_discovery", "bool"},
		{"features.apps_mcp_path_override", "bool"},
		{"features.async_compaction", "bool"},
		{"features.auth_elicitation", "bool"},
		{"features.background_paginated_rollout_migration", "bool"},
		{"features.bedrock_setup_wizard", "bool"},
		{"features.browser_annotation_api", "bool"},
		{"features.browser_use", "bool"},
		{"features.browser_use_external", "bool"},
		{"features.browser_use_full_cdp_access", "bool"},
		{"features.chronicle", "bool"},
		{"features.cli_daybreak", "bool"},
		{"features.code_mode", "bool"},
		{"features.code_mode_buffered_exec", "bool"},
		{"features.code_mode_host", "bool"},
		{"features.code_mode_host.disable_in_process_fallback", "bool"},
		{"features.code_mode_host.enabled", "bool"},
		{"features.code_mode_host_grpc", "bool"},
		{"features.code_mode_host_output_truncation", "bool"},
		{"features.code_mode_interrupt", "bool"},
		{"features.code_mode_only", "bool"},
		{"features.code_mode_only_strict_3p_tools", "bool"},
		{"features.code_mode_prewarm", "bool"},
		{"features.code_mode_tool_description_first", "bool"},
		{"features.code_mode_tool_search", "bool"},
		{"features.codex_apps_mcp_2026_07_28", "bool"},
		{"features.codex_git_commit", "bool"},
		{"features.codex_hooks", "bool"},
		{"features.collab", "bool"},
		{"features.collaboration_modes", "bool"},
		{"features.compaction_image_budget", "bool"},
		{"features.computer_use", "bool"},
		{"features.concurrent_reasoning_summaries", "bool"},
		{"features.connectors", "bool"},
		{"features.content_item_kinds", "bool"},
		{"features.context_management", "bool"},
		{"features.credential_masking", "bool"},
		{"features.critical_path_timing", "bool"},
		{"features.current_time_reminder", "bool"},
		{"features.cwd_relative_turn_diffs", "bool"},
		{"features.daemon_auto_start", "bool"},
		{"features.default_mode_request_user_input", "bool"},
		{"features.defer_mailbox_preemption", "bool"},
		{"features.deferred_executor", "bool"},
		{"features.deferred_tool_world_state", "bool"},
		{"features.elevated_windows_sandbox", "bool"},
		{"features.enable_experimental_windows_sandbox", "bool"},
		{"features.enable_fanout", "bool"},
		{"features.enable_mcp_apps", "bool"},
		{"features.enable_request_compression", "bool"},
		{"features.exec_permission_approvals", "bool"},
		{"features.executed_tool_call_metadata", "bool"},
		{"features.executor_capability_discovery", "bool"},
		{"features.executor_capability_discovery_v2", "bool"},
		{"features.experimental_use_unified_exec_tool", "bool"},
		{"features.experimental_windows_sandbox", "bool"},
		{"features.external_agent_memory_import", "bool"},
		{"features.external_migration", "bool"},
		{"features.fast_mode", "bool"},
		{"features.gcp_setup_wizard", "bool"},
		{"features.goals", "bool"},
		{"features.guardian_approval", "bool"},
		{"features.guardian_conversation_history_tools", "bool"},
		{"features.guardian_enhanced_node_repl_transcripts", "bool"},
		{"features.guardian_ext", "bool"},
		{"features.guardian_native_multi_agent_history", "bool"},
		{"features.guardian_node_repl_transcript_images", "bool"},
		{"features.guardian_recorded_items", "bool"},
		{"features.guardian_reuse_parent_compaction", "bool"},
		{"features.guardian_root_handoff_context", "bool"},
		{"features.guardian_trust_orchestrator_connectors", "bool"},
		{"features.guardianv2", "bool"},
		{"features.guardianv2_decisions_comparison", "bool"},
		{"features.guardianv3", "bool"},
		{"features.hooks", "bool"},
		{"features.image_detail_original", "bool"},
		{"features.image_generation", "bool"},
		{"features.image_resize_notice", "bool"},
		{"features.imagegenext", "bool"},
		{"features.in_app_browser", "bool"},
		{"features.in_app_chat", "bool"},
		{"features.in_app_dictation", "bool"},
		{"features.in_app_local_automation", "bool"},
		{"features.in_app_updates", "bool"},
		{"features.in_app_voice", "bool"},
		{"features.incremental_exec_tools", "bool"},
		{"features.incremental_tools", "bool"},
		{"features.instant_interrupt", "bool"},
		{"features.interrupt_on_cancel", "bool"},
		{"features.item_ids", "bool"},
		{"features.js_repl", "bool"},
		{"features.js_repl_tools_only", "bool"},
		{"features.local_thread_store_compression", "bool"},
		{"features.local_thread_store_shared_compression", "bool"},
		{"features.login_shell_package_path", "bool"},
		{"features.mcp_2026_07_28", "bool"},
		{"features.mcp_connection_sharing", "bool"},
		{"features.mcp_oauth_refresh_coordination", "bool"},
		{"features.mcp_tool_list_changed", "bool"},
		{"features.memories", "bool"},
		{"features.memory_tool", "bool"},
		{"features.mentions_v2", "bool"},
		{"features.model_catalog_in_context", "bool"},
		{"features.multi_agent", "bool"},
		{"features.multi_agent_mode", "bool"},
		{"features.multi_agent_v2", "bool"},
		{"features.multi_agent_v2_dynamic_tools", "bool"},
		{"features.multi_env_copy_file", "bool"},
		{"features.network_proxy", "bool"},
		{"features.non_prefixed_mcp_tool_names", "bool"},
		{"features.nonfatal_clock_read_errors", "bool"},
		{"features.omit_app_server_notification_media", "bool"},
		{"features.omit_skill_descriptions", "bool"},
		{"features.output_token_replay", "bool"},
		{"features.personality", "bool"},
		{"features.plugin_discovery_prefetch", "bool"},
		{"features.plugin_hooks", "bool"},
		{"features.plugin_sharing", "bool"},
		{"features.plugins", "bool"},
		{"features.powershell_shell_version", "bool"},
		{"features.prediction_preserve_parent_tools", "bool"},
		{"features.prefer_mxc", "bool"},
		{"features.prevent_idle_sleep", "bool"},
		{"features.psp", "bool"},
		{"features.realtime_conversation", "bool"},
		{"features.reasoning_effort_override", "bool"},
		{"features.recommended_plugins", "bool"},
		{"features.remote_compaction_v2", "bool"},
		{"features.remote_control", "bool"},
		{"features.remote_models", "bool"},
		{"features.remote_plugin", "bool"},
		{"features.request_permissions", "bool"},
		{"features.request_permissions_tool", "bool"},
		{"features.request_rule", "bool"},
		{"features.resize_all_images", "bool"},
		{"features.respect_system_proxy", "bool"},
		{"features.responses_websockets", "bool"},
		{"features.responses_websockets_v2", "bool"},
		{"features.retain_client_developer_messages", "bool"},
		{"features.retain_client_tool_outputs", "bool"},
		{"features.rollout_budget", "bool"},
		{"features.runtime_agent_instances", "bool"},
		{"features.runtime_metrics", "bool"},
		{"features.search_tool", "bool"},
		{"features.secret_auth_storage", "bool"},
		{"features.send_async_message", "bool"},
		{"features.send_message_to_user_async", "bool"},
		{"features.shell_snapshot", "bool"},
		{"features.shell_snapshot_v2", "bool"},
		{"features.shell_tool", "bool"},
		{"features.shell_zsh_fork", "bool"},
		{"features.simplified_selection_hints", "bool"},
		{"features.skill_catalog_deltas", "bool"},
		{"features.skill_env_var_dependency_prompt", "bool"},
		{"features.skill_mcp_dependency_install", "bool"},
		{"features.skill_search", "bool"},
		{"features.skip_host_skill_discovery", "bool"},
		{"features.sleep_tool", "bool"},
		{"features.sqlite", "bool"},
		{"features.stable_environment_tools", "bool"},
		{"features.standalone_web_search", "bool"},
		{"features.steer", "bool"},
		{"features.step_model_switching", "bool"},
		{"features.subagent_default_context_limits", "bool"},
		{"features.subagent_thread_environments", "bool"},
		{"features.suppress_model_change_announcements", "bool"},
		{"features.system_proxy_fallback", "bool"},
		{"features.telepathy", "bool"},
		{"features.terminal_resize_reflow", "bool"},
		{"features.terminal_visualization_instructions", "bool"},
		{"features.token_budget", "bool"},
		{"features.tool_call_mcp_elicitation", "bool"},
		{"features.tool_registry", "object"},
		{"features.tool_search", "bool"},
		{"features.tool_search_always_defer_mcp_tools", "bool"},
		{"features.tool_suggest", "bool"},
		{"features.transcript_v2", "bool"},
		{"features.tui_app_server", "bool"},
		{"features.tui_extensions_statusline", "bool"},
		{"features.tui_multi_backend", "bool"},
		{"features.ultrafast_mode", "bool"},
		{"features.unavailable_dummy_tools", "bool"},
		{"features.unbounded_connection_retries", "bool"},
		{"features.undo", "bool"},
		{"features.unified_exec", "bool"},
		{"features.unified_exec_tty", "bool"},
		{"features.unified_exec_zsh_fork", "bool"},
		{"features.unified_image_budget", "bool"},
		{"features.untrusted_as_tool_output", "bool"},
		{"features.use_agent_identity", "bool"},
		{"features.use_legacy_landlock", "bool"},
		{"features.use_linux_sandbox_bwrap", "bool"},
		{"features.use_xaa", "bool"},
		{"features.view_image", "bool"},
		{"features.wake_on_agent_messages", "bool"},
		{"features.web_search", "bool"},
		{"features.web_search_cached", "bool"},
		{"features.web_search_request", "bool"},
		{"features.windows_sandbox_service", "bool"},
		{"features.workspace_dependencies", "bool"},
		{"features.workspace_owner_usage_nudge", "bool"},
		{"features.worktrees", "bool"},
		{"features.write_stdin_approval", "bool"},
		{"feedback.enabled", "bool"},
		{"forced_login_method", "login_method"},
		{"hide_agent_reasoning", "bool"},
		{"include_apps_instructions", "bool"},
		{"include_collaboration_mode_instructions", "bool"},
		{"include_environment_context", "bool"},
		{"include_environment_context_time", "bool"},
		{"include_permissions_instructions", "bool"},
		{"mcp_oauth_credentials_store", "mcp_store"},
		{"plugins._default.enabled", "bool"},
		{"show_raw_agent_reasoning", "bool"},
		{"suppress_unstable_features_warning", "bool"},
		{"windows.allow_mxc", "bool"},
		{"windows.sandbox", "sandbox"},
	}
	if len(legacy) != 234 {
		t.Fatal("unexpected legacy metadata snapshot")
	}
	for _, row := range legacy {
		if observedConfigTypes[row[0]] != row[1] {
			t.Fatal("legacy metadata type changed")
		}
	}
}

func TestSchemaExpansionDoesNotInventDynamicPaths(t *testing.T) {
	for _, key := range []string{
		"desktop.SYNTHETIC_SECRET",
		"model_providers.SYNTHETIC_SECRET",
		"model_providers.SYNTHETIC_SECRET.base_url",
		"plugins.SYNTHETIC_SECRET.enabled",
		"apps.SYNTHETIC_SECRET.enabled",
		"agents.SYNTHETIC_SECRET.config_file",
		"mcp_servers.SYNTHETIC_SECRET.command",
		"profiles.SYNTHETIC_SECRET.model",
		"projects.SYNTHETIC_SECRET.trust_level",
		"otel.exporter.otlp-http.headers.SYNTHETIC_SECRET",
		"features.network_proxy.credentials.SYNTHETIC_SECRET",
		"skills.config.path",
		"skills.config.0.path",
		"notice.hide_gpt-5.1-codex-max_migration_prompt",
	} {
		if _, exists := observedConfigTypes[key]; exists {
			t.Fatal("dynamic, array or ambiguous path entered the frozen set")
		}
	}
}

func TestNewSchemaPathsNeverInterpretValues(t *testing.T) {
	for key, kind := range observedConfigTypes {
		if kind != "opaque" {
			continue
		}
		for _, value := range []string{"true", "false", "api", "'chatgpt'", "\"keyring\"", "1", "[]", "{}", "SYNTHETIC_SECRET"} {
			if observedConfigValue(kind, value) != "other" {
				t.Fatal("new schema path acquired value interpretation")
			}
		}
		if strings.Contains(key, "\"") {
			continue
		}
		e := observedConfigArgument(key + "=SYNTHETIC_SECRET")
		if e.ConfigKey != key || e.ConfigValue != "other" || e.Class != "" || e.ConfigShape != "" || e.ConfigFamily != "" {
			t.Fatal("explicit schema path was not recognized opaquely")
		}
	}
}

func TestFrozenConfigFamiliesContainOnlySchemaRoots(t *testing.T) {
	if len(observedSchemaRoots) != 103 || len(observedConfigFamilies) != 103 ||
		!sort.StringsAreSorted(observedSchemaRoots[:]) {
		t.Fatal("unexpected fixed family set")
	}
	seen := make(map[string]bool)
	for _, root := range observedSchemaRoots {
		if seen[root] || strings.Contains(root, ".") || observedConfigFamilies[root] != root {
			t.Fatal("invalid or duplicate fixed family")
		}
		seen[root] = true
		if _, exists := observedConfigTypes[root]; !exists {
			t.Fatal("family is not an explicit root property")
		}
	}
	for key := range observedConfigTypes {
		if !strings.Contains(key, ".") && !seen[key] {
			t.Fatal("schema root is missing from the family set")
		}
	}
	if _, exists := observedConfigFamilies["other"]; exists {
		t.Fatal("fallback family is not a schema root")
	}
}

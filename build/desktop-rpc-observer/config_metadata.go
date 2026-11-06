package main

import "strings"

// Frozen metadata-only keys from openai/codex config.schema.json at
// c40729ea5cf8e947481b130c59807273eda2f8e9, blob
// f5149c6efdf7ab9f1550aa0639dd5f2401205f25. No schema is fetched at runtime.
// Recognition never authorizes forwarding or changes production CLI parsing.
var observedConfigTypes = func() map[string]string {
	keys := make(map[string]string, len(observedSchemaKeys))
	for _, key := range observedSchemaKeys {
		keys[key] = "opaque"
	}
	keys["features.tool_registry"] = "object"
	keys["cli_auth_credentials_store"] = "cli_store"
	keys["mcp_oauth_credentials_store"] = "mcp_store"
	keys["windows.sandbox"] = "sandbox"
	keys["forced_login_method"] = "login_method"
	for _, key := range []string{
		"allow_login_shell",
		"allow_symlinked_codex_home",
		"analytics.enabled",
		"check_for_update_on_startup",
		"daybreak",
		"disable_paste_burst",
		"experimental_use_unified_exec_tool",
		"features.agent_message_board",
		"features.analytics_plan_history",
		"features.api_key_cyber_access_programs",
		"features.api_key_model_discovery",
		"features.app_server_websocket_compression",
		"features.apply_patch_freeform",
		"features.apply_patch_preserve_line_endings",
		"features.apply_patch_streaming_events",
		"features.apps",
		"features.apps_early_resource_discovery",
		"features.apps_mcp_path_override",
		"features.async_compaction",
		"features.auth_elicitation",
		"features.background_paginated_rollout_migration",
		"features.bedrock_setup_wizard",
		"features.browser_annotation_api",
		"features.browser_use",
		"features.browser_use_external",
		"features.browser_use_full_cdp_access",
		"features.chronicle",
		"features.cli_daybreak",
		"features.code_mode",
		"features.code_mode_buffered_exec",
		"features.code_mode_host",
		"features.code_mode_host.disable_in_process_fallback",
		"features.code_mode_host.enabled",
		"features.code_mode_host_grpc",
		"features.code_mode_host_output_truncation",
		"features.code_mode_interrupt",
		"features.code_mode_only",
		"features.code_mode_only_strict_3p_tools",
		"features.code_mode_prewarm",
		"features.code_mode_tool_description_first",
		"features.code_mode_tool_search",
		"features.codex_apps_mcp_2026_07_28",
		"features.codex_git_commit",
		"features.codex_hooks",
		"features.collab",
		"features.collaboration_modes",
		"features.compaction_image_budget",
		"features.computer_use",
		"features.concurrent_reasoning_summaries",
		"features.connectors",
		"features.content_item_kinds",
		"features.context_management",
		"features.credential_masking",
		"features.critical_path_timing",
		"features.current_time_reminder",
		"features.cwd_relative_turn_diffs",
		"features.daemon_auto_start",
		"features.default_mode_request_user_input",
		"features.defer_mailbox_preemption",
		"features.deferred_executor",
		"features.deferred_tool_world_state",
		"features.elevated_windows_sandbox",
		"features.enable_experimental_windows_sandbox",
		"features.enable_fanout",
		"features.enable_mcp_apps",
		"features.enable_request_compression",
		"features.exec_permission_approvals",
		"features.executed_tool_call_metadata",
		"features.executor_capability_discovery",
		"features.executor_capability_discovery_v2",
		"features.experimental_use_unified_exec_tool",
		"features.experimental_windows_sandbox",
		"features.external_agent_memory_import",
		"features.external_migration",
		"features.fast_mode",
		"features.gcp_setup_wizard",
		"features.goals",
		"features.guardian_approval",
		"features.guardian_conversation_history_tools",
		"features.guardian_enhanced_node_repl_transcripts",
		"features.guardian_ext",
		"features.guardian_native_multi_agent_history",
		"features.guardian_node_repl_transcript_images",
		"features.guardian_recorded_items",
		"features.guardian_reuse_parent_compaction",
		"features.guardian_root_handoff_context",
		"features.guardian_trust_orchestrator_connectors",
		"features.guardianv2",
		"features.guardianv2_decisions_comparison",
		"features.guardianv3",
		"features.hooks",
		"features.image_detail_original",
		"features.image_generation",
		"features.image_resize_notice",
		"features.imagegenext",
		"features.in_app_browser",
		"features.in_app_chat",
		"features.in_app_dictation",
		"features.in_app_local_automation",
		"features.in_app_updates",
		"features.in_app_voice",
		"features.incremental_exec_tools",
		"features.incremental_tools",
		"features.instant_interrupt",
		"features.interrupt_on_cancel",
		"features.item_ids",
		"features.js_repl",
		"features.js_repl_tools_only",
		"features.local_thread_store_compression",
		"features.local_thread_store_shared_compression",
		"features.login_shell_package_path",
		"features.mcp_2026_07_28",
		"features.mcp_connection_sharing",
		"features.mcp_oauth_refresh_coordination",
		"features.mcp_tool_list_changed",
		"features.memories",
		"features.memory_tool",
		"features.mentions_v2",
		"features.model_catalog_in_context",
		"features.multi_agent",
		"features.multi_agent_mode",
		"features.multi_agent_v2",
		"features.multi_agent_v2_dynamic_tools",
		"features.multi_env_copy_file",
		"features.network_proxy",
		"features.non_prefixed_mcp_tool_names",
		"features.nonfatal_clock_read_errors",
		"features.omit_app_server_notification_media",
		"features.omit_skill_descriptions",
		"features.output_token_replay",
		"features.personality",
		"features.plugin_discovery_prefetch",
		"features.plugin_hooks",
		"features.plugin_sharing",
		"features.plugins",
		"features.powershell_shell_version",
		"features.prediction_preserve_parent_tools",
		"features.prefer_mxc",
		"features.prevent_idle_sleep",
		"features.psp",
		"features.realtime_conversation",
		"features.reasoning_effort_override",
		"features.recommended_plugins",
		"features.remote_compaction_v2",
		"features.remote_control",
		"features.remote_models",
		"features.remote_plugin",
		"features.request_permissions",
		"features.request_permissions_tool",
		"features.request_rule",
		"features.resize_all_images",
		"features.respect_system_proxy",
		"features.responses_websockets",
		"features.responses_websockets_v2",
		"features.retain_client_developer_messages",
		"features.retain_client_tool_outputs",
		"features.rollout_budget",
		"features.runtime_agent_instances",
		"features.runtime_metrics",
		"features.search_tool",
		"features.secret_auth_storage",
		"features.send_async_message",
		"features.send_message_to_user_async",
		"features.shell_snapshot",
		"features.shell_snapshot_v2",
		"features.shell_tool",
		"features.shell_zsh_fork",
		"features.simplified_selection_hints",
		"features.skill_catalog_deltas",
		"features.skill_env_var_dependency_prompt",
		"features.skill_mcp_dependency_install",
		"features.skill_search",
		"features.skip_host_skill_discovery",
		"features.sleep_tool",
		"features.sqlite",
		"features.stable_environment_tools",
		"features.standalone_web_search",
		"features.steer",
		"features.step_model_switching",
		"features.subagent_default_context_limits",
		"features.subagent_thread_environments",
		"features.suppress_model_change_announcements",
		"features.system_proxy_fallback",
		"features.telepathy",
		"features.terminal_resize_reflow",
		"features.terminal_visualization_instructions",
		"features.token_budget",
		"features.tool_call_mcp_elicitation",
		"features.tool_search",
		"features.tool_search_always_defer_mcp_tools",
		"features.tool_suggest",
		"features.transcript_v2",
		"features.tui_app_server",
		"features.tui_extensions_statusline",
		"features.tui_multi_backend",
		"features.ultrafast_mode",
		"features.unavailable_dummy_tools",
		"features.unbounded_connection_retries",
		"features.undo",
		"features.unified_exec",
		"features.unified_exec_tty",
		"features.unified_exec_zsh_fork",
		"features.unified_image_budget",
		"features.untrusted_as_tool_output",
		"features.use_agent_identity",
		"features.use_legacy_landlock",
		"features.use_linux_sandbox_bwrap",
		"features.use_xaa",
		"features.view_image",
		"features.wake_on_agent_messages",
		"features.web_search",
		"features.web_search_cached",
		"features.web_search_request",
		"features.windows_sandbox_service",
		"features.workspace_dependencies",
		"features.workspace_owner_usage_nudge",
		"features.worktrees",
		"features.write_stdin_approval",
		"feedback.enabled",
		"hide_agent_reasoning",
		"include_apps_instructions",
		"include_collaboration_mode_instructions",
		"include_environment_context",
		"include_environment_context_time",
		"include_permissions_instructions",
		"plugins._default.enabled",
		"show_raw_agent_reasoning",
		"suppress_unstable_features_warning",
		"windows.allow_mxc",
	} {
		keys[key] = "bool"
	}
	return keys
}()

// Only literal public booleans/enums become fixed labels. No raw value,
// arbitrary key, parsed structure, hash or path can enter the metadata.
func observedConfigValue(kind, value string) string {
	if kind == "bool" {
		switch value {
		case "true":
			return "bool_true"
		case "false":
			return "bool_false"
		}
		return "other"
	}
	var allowed []string
	switch kind {
	case "cli_store":
		allowed = []string{"file", "keyring", "auto", "ephemeral"}
	case "mcp_store":
		allowed = []string{"file", "keyring", "auto"}
	case "sandbox":
		allowed = []string{"elevated", "unelevated", "mxc"}
	case "login_method":
		allowed = []string{"chatgpt", "api"}
	default:
		return "other"
	}
	for _, literal := range allowed {
		if value == literal || value == "'"+literal+"'" || value == "\""+literal+"\"" {
			return "enum_" + literal
		}
	}
	return "other"
}

// observedConfigArgument recognizes only a conservative subset of the native
// CLI's literal dotted paths. Metadata never authorizes production forwarding.
// Unknown assignments expose only fixed classifications, never raw keys or values.
func observedConfigArgument(argument string) event {
	key, value, hasEquals := strings.Cut(argument, "=")
	if !hasEquals {
		return event{
			Kind: "argv_config", Class: "unknown", ConfigShape: "no_equals", ConfigFamily: "other",
			ConfigLiteral: "other", ConfigLexicalRoot: "other", ConfigKeyClasses: []string{"none"},
		}
	}
	if strings.Trim(key, observedConfigASCIIWhitespace) == "" {
		return observedUnknownConfigArgument(key, value, "empty_key", "other")
	}
	if fixed, known := observedPublicPluginConfigKey(key); known {
		return event{Kind: "argv_config", ConfigKey: fixed, ConfigValue: observedConfigValue("bool", value)}
	}
	normalized, valid := observedConfigKey(key)
	if valid {
		if kind, known := observedConfigTypes[normalized]; known {
			return event{Kind: "argv_config", ConfigKey: normalized, ConfigValue: observedConfigValue(kind, value)}
		}
	}
	shape := "other_key"
	if strings.ContainsAny(key, "\"'") {
		shape = "quoted_key"
	} else if valid {
		shape = "plain_key"
	}
	family := "other"
	if valid {
		first, _, _ := strings.Cut(normalized, ".")
		if fixed, known := observedConfigFamilies[first]; known {
			family = fixed
		}
	}
	return observedUnknownConfigArgument(key, value, shape, family)
}

// observedConfigKey accepts ASCII bare segments separated by dots, after only
// whole-key ASCII whitespace trimming. The pinned native CLI trims the entire
// key and then splits literal dots; it does not unquote keys or trim individual
// segments. This intentionally rejects additional native-accepted spellings.
// This is only a lookup candidate, not a complete native configuration parser.
func observedConfigKey(key string) (string, bool) {
	key = strings.Trim(key, observedConfigASCIIWhitespace)
	if key == "" {
		return "", false
	}
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			if i == start {
				return "", false
			}
			start = i + 1
		} else if !observedConfigKeyByte(key[i]) {
			return "", false
		}
	}
	if start == len(key) {
		return "", false
	}
	return key, true
}

func observedConfigKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '-'
}

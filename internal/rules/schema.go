package rules

import "github.com/trustabl/trustabl/internal/models"

// PolicyFile is the top-level structure of a .yaml policy file.
type PolicyFile struct {
	Policy PolicyMeta `yaml:"policy"`
	Rules  []RuleDef  `yaml:"rules"`
}

// PolicyMeta holds the policy-level metadata.
type PolicyMeta struct {
	ID          string                  `yaml:"id"`
	Name        string                  `yaml:"name"`
	Category    models.DetectorCategory `yaml:"category"`
	Description string                  `yaml:"description"`
}

// RuleDef is one rule entry inside a policy file.
// Category is not in YAML — the loader copies it from PolicyMeta.Category.
// Language defaults to "python" if absent in YAML — the loader fills it in.
type RuleDef struct {
	ID          string                  `yaml:"id"`
	Title       string                  `yaml:"title"`
	Scope       models.Scope            `yaml:"scope"`
	Severity    models.Severity         `yaml:"severity"`
	Confidence  float64                 `yaml:"confidence"`
	Language    models.Language         `yaml:"language,omitempty"`
	AppliesTo   []string                `yaml:"applies_to"`
	Match       MatchExpr               `yaml:"match"`
	Explanation string                  `yaml:"explanation"`
	Fix         string                  `yaml:"fix"`
	Category    models.DetectorCategory `yaml:"-"` // populated by loader
}

// MatchExpr is a recursive predicate or combinator. All set fields are ANDed.
type MatchExpr struct {
	// Combinators
	All []MatchExpr `yaml:"all,omitempty"`
	Any []MatchExpr `yaml:"any,omitempty"`
	Not *MatchExpr  `yaml:"not,omitempty"`

	// Bool predicates — pointer distinguishes "set to false" from "absent"
	HasDocstring      *bool `yaml:"has_docstring,omitempty"`
	HasParams         *bool `yaml:"has_params,omitempty"`
	HasTypedParams    *bool `yaml:"has_typed_params,omitempty"`
	HasRaise          *bool `yaml:"has_raise,omitempty"`
	HasTryExcept      *bool `yaml:"has_try_except,omitempty"`
	HasShellCall      *bool `yaml:"has_shell_call,omitempty"`
	HasCodeExecCall   *bool `yaml:"has_code_exec_call,omitempty"`
	HasPrintCall      *bool `yaml:"has_print_call,omitempty"`
	HasWriteCall      *bool `yaml:"has_write_call,omitempty"`
	HasDynamicURLCall *bool `yaml:"has_dynamic_url_call,omitempty"`
	// HasHTTPCallWithoutTimeout is TypeScript-only (backed by the http_no_timeout
	// discovery fact); see PredHasHTTPCallWithoutTimeout.
	HasHTTPCallWithoutTimeout *bool `yaml:"has_http_call_without_timeout,omitempty"`
	Always                    *bool `yaml:"always,omitempty"`

	// String-list predicates
	NameIn        []string `yaml:"name_in,omitempty"`
	NameHasPrefix []string `yaml:"name_has_prefix,omitempty"`
	HasBodyText   []string `yaml:"has_body_text,omitempty"`

	// Nested struct predicates (tool scope)
	ParamNameMatches              *ParamNameMatchExpr                `yaml:"param_name_matches,omitempty"`
	CallWithoutKwarg              *CallWithoutKwargExpr              `yaml:"call_without_kwarg,omitempty"`
	CallUsesUnnormalizedPathParam *CallUsesUnnormalizedPathParamExpr `yaml:"call_uses_unnormalized_path_param,omitempty"`

	// Tool-scope decorator predicates
	ToolDecoratorKwargValue   *ToolDecoratorKwargValueExpr `yaml:"tool_decorator_kwarg_value,omitempty"`
	ToolDecoratorKwargPresent []string                     `yaml:"tool_decorator_kwarg_present,omitempty"`

	// Agent-scope predicates
	AgentClass                  []string                  `yaml:"agent_class,omitempty"`
	AgentKwargPresent           []string                  `yaml:"agent_kwarg_present,omitempty"`
	AgentKwargMissing           []string                  `yaml:"agent_kwarg_missing,omitempty"`
	AgentKwargListEmpty         []string                  `yaml:"agent_kwarg_list_empty,omitempty"`
	AgentKwargValue             *AgentKwargValueExpr      `yaml:"agent_kwarg_value,omitempty"`
	AgentUsesToolKind           []string                  `yaml:"agent_uses_tool_kind,omitempty"`
	AgentGrantsBuiltinTool      []string                  `yaml:"agent_grants_builtin_tool,omitempty"`
	AgentUsesHostedToolClass    []string                  `yaml:"agent_uses_hosted_tool_class,omitempty"`
	AgentIsSubagentOfAny        *bool                     `yaml:"agent_is_subagent_of_any,omitempty"`
	AgentHostedToolKwargPresent *HostedToolKwargExpr      `yaml:"agent_hosted_tool_kwarg_present,omitempty"`
	AgentHostedToolKwargValue   *HostedToolKwargValueExpr `yaml:"agent_hosted_tool_kwarg_value,omitempty"`

	// Subagent-scope predicates
	SubagentGrantsTool []string `yaml:"subagent_grants_tool,omitempty"`

	// Skill-scope predicates
	SkillAllowsUnrestrictedShell            *bool    `yaml:"skill_allows_unrestricted_shell,omitempty"`
	SkillAllowsTool                         []string `yaml:"skill_allows_tool,omitempty"`
	SkillModelInvocable                     *bool    `yaml:"skill_model_invocable,omitempty"`
	SkillBodyHasDynamicExec                 *bool    `yaml:"skill_body_has_dynamic_exec,omitempty"`
	SkillDynamicExecTouchesNetworkOrSecrets *bool    `yaml:"skill_dynamic_exec_touches_network_or_secrets,omitempty"`
	SkillReferencesExternalURL              *bool    `yaml:"skill_references_external_url,omitempty"`
	SkillBodyHasInjectionMarker             *bool    `yaml:"skill_body_has_injection_marker,omitempty"`
	SkillBundledScriptNetworkEgress         *bool    `yaml:"skill_bundled_script_network_egress,omitempty"`
	SkillBundledScriptReadsSecrets          *bool    `yaml:"skill_bundled_script_reads_secrets,omitempty"`
	SkillBundledFileHasHardcodedSecret      *bool    `yaml:"skill_bundled_file_has_hardcoded_secret,omitempty"`
	SkillDescriptionToolMismatch            *bool    `yaml:"skill_description_tool_mismatch,omitempty"`

	// Repo-scope predicates
	RepoHasSDKInCode                  []string `yaml:"repo_has_sdk_in_code,omitempty"`
	RepoComponentPresent              []string `yaml:"repo_component_present,omitempty"`
	RepoUsesDefaultTracing            *bool    `yaml:"repo_uses_default_tracing,omitempty"`
	RepoClaudeDefaultModeIs           []string `yaml:"repo_claude_default_mode_is,omitempty"`
	RepoClaudeOptionsPermissionModeIs []string `yaml:"repo_claude_options_permission_mode_is,omitempty"`
}

// ToolDecoratorKwargValueExpr matches a decorator kwarg to a specific value.
type ToolDecoratorKwargValueExpr struct {
	Kwarg string `yaml:"kwarg"`
	Value string `yaml:"value"`
}

// AgentKwargValueExpr matches an agent constructor kwarg (dotted-path) to a value.
type AgentKwargValueExpr struct {
	Kwarg string `yaml:"kwarg"`
	Value string `yaml:"value"` // compared after quote-stripping for string literals
}

// HostedToolKwargExpr matches the presence of a kwarg on a hosted-tool instance
// of the named class wired to the agent (e.g. BashTool's `policy`).
type HostedToolKwargExpr struct {
	Class string `yaml:"class"`
	Kwarg string `yaml:"kwarg"` // dotted-path supported
}

// HostedToolKwargValueExpr matches a hosted-tool instance's kwarg to a value
// (e.g. ShellTool's needs_approval == "True").
type HostedToolKwargValueExpr struct {
	Class string `yaml:"class"`
	Kwarg string `yaml:"kwarg"`
	Value string `yaml:"value"` // compared after quote-stripping for string literals
}

// ParamNameMatchExpr matches parameter names against exact/contains/suffix/prefix patterns.
type ParamNameMatchExpr struct {
	Exact    []string `yaml:"exact,omitempty"`
	Contains []string `yaml:"contains,omitempty"`
	Suffixes []string `yaml:"suffixes,omitempty"`
	Prefixes []string `yaml:"prefixes,omitempty"`
}

// CallWithoutKwargExpr fires when a matching call is missing the named keyword argument.
type CallWithoutKwargExpr struct {
	Callees []string `yaml:"callees"`
	Missing string   `yaml:"missing"`
}

// CallUsesUnnormalizedPathParamExpr fires when a path-like param flows to an
// I/O call AND that specific param has not been normalized
// (.resolve()/realpath()) elsewhere in the function. Per-param: a tool with
// two path params and one .resolve() still fires on the unresolved one.
type CallUsesUnnormalizedPathParamExpr struct {
	Callees        []string `yaml:"callees,omitempty"`
	CalleePrefixes []string `yaml:"callee_prefixes,omitempty"`
}

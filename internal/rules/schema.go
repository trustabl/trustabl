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
	HasWriteCall      *bool `yaml:"has_write_call,omitempty"`
	HasDynamicURLCall *bool `yaml:"has_dynamic_url_call,omitempty"`
	Always            *bool `yaml:"always,omitempty"`

	// String-list predicates
	NameIn        []string `yaml:"name_in,omitempty"`
	NameHasPrefix []string `yaml:"name_has_prefix,omitempty"`
	HasBodyText   []string `yaml:"has_body_text,omitempty"`

	// Nested struct predicates (tool scope)
	ParamNameMatches              *ParamNameMatchExpr                `yaml:"param_name_matches,omitempty"`
	CallWithoutKwarg              *CallWithoutKwargExpr              `yaml:"call_without_kwarg,omitempty"`
	CallWithKwargValue            *CallWithKwargValueExpr            `yaml:"call_with_kwarg_value,omitempty"`
	CallUsesUnnormalizedPathParam *CallUsesUnnormalizedPathParamExpr `yaml:"call_uses_unnormalized_path_param,omitempty"`

	// Tool-scope decorator predicates
	ToolDecoratorKwargValue   *ToolDecoratorKwargValueExpr `yaml:"tool_decorator_kwarg_value,omitempty"`
	ToolDecoratorKwargPresent []string                     `yaml:"tool_decorator_kwarg_present,omitempty"`

	// Agent-scope predicates
	AgentClass               []string             `yaml:"agent_class,omitempty"`
	AgentKwargPresent        []string             `yaml:"agent_kwarg_present,omitempty"`
	AgentKwargMissing        []string             `yaml:"agent_kwarg_missing,omitempty"`
	AgentKwargListEmpty      []string             `yaml:"agent_kwarg_list_empty,omitempty"`
	AgentKwargValue          *AgentKwargValueExpr `yaml:"agent_kwarg_value,omitempty"`
	AgentUsesToolKind        []string             `yaml:"agent_uses_tool_kind,omitempty"`
	AgentGrantsBuiltinTool   []string             `yaml:"agent_grants_builtin_tool,omitempty"`
	AgentHandoffToClass      []string             `yaml:"agent_handoff_to_class,omitempty"`
	AgentUsesHostedToolClass []string             `yaml:"agent_uses_hosted_tool_class,omitempty"`
	AgentIsSubagentOfAny     *bool                `yaml:"agent_is_subagent_of_any,omitempty"`

	// Repo-scope predicates
	RepoHasSDKDep          []string `yaml:"repo_has_sdk_dep,omitempty"`
	RepoHasSDKInCode       []string `yaml:"repo_has_sdk_in_code,omitempty"`
	RepoHasAgentClass      []string `yaml:"repo_has_agent_class,omitempty"`
	RepoHasNoAgentClass    []string `yaml:"repo_has_no_agent_class,omitempty"`
	RepoComponentPresent   []string `yaml:"repo_component_present,omitempty"`
	RepoUsesDefaultTracing *bool    `yaml:"repo_uses_default_tracing,omitempty"`
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

// CallWithKwargValueExpr fires when a matching call has kwarg == value.
type CallWithKwargValueExpr struct {
	CalleePrefix string   `yaml:"callee_prefix,omitempty"`
	Callees      []string `yaml:"callees,omitempty"`
	Kwarg        string   `yaml:"kwarg"`
	Value        string   `yaml:"value"`
}

// CallUsesUnnormalizedPathParamExpr fires when a path-like param flows to an
// I/O call AND that specific param has not been normalized
// (.resolve()/realpath()) elsewhere in the function. Per-param: a tool with
// two path params and one .resolve() still fires on the unresolved one.
type CallUsesUnnormalizedPathParamExpr struct {
	Callees        []string `yaml:"callees,omitempty"`
	CalleePrefixes []string `yaml:"callee_prefixes,omitempty"`
}

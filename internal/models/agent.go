package models

// Location is the file/line attribution for a discovered inventory entity.
// Line and EndLine are both 1-indexed and inclusive. Single-line entities
// set EndLine == Line; that is a valid state, not a placeholder.
//
// Location is intended to be embedded anonymously into entity structs so
// JSON serialization stays flat (entity.file_path, entity.start_line,
// entity.end_line).
type Location struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"start_line"`
	EndLine  int    `json:"end_line"`
}

// KwargTree represents a kwarg value as either a leaf (Value) or a nested
// tree (Children, e.g. for model_settings.tool_choice).
type KwargTree struct {
	Value    *Expr                 `json:"value,omitempty"`
	Children map[string]*KwargTree `json:"children,omitempty"`
}

// Expr is a typed wrapper around a parsed AST node.
type Expr struct {
	Kind    ExprKind `json:"kind"`
	Text    string   `json:"text"`           // raw source text
	List    []Expr   `json:"list,omitempty"` // populated when Kind == ExprList
	Line    int      `json:"-"`              // 1-indexed start line; in-memory carrier, not serialized
	EndLine int      `json:"-"`              // 1-indexed end line; in-memory carrier, not serialized
	// CallKwargs holds the keyword arguments of a call expression (Kind ==
	// ExprCall), e.g. ShellTool(needs_approval=False) -> {"needs_approval": ...}.
	// In-memory carrier so hosted-tool discovery can attach a call's kwargs to
	// the HostedToolDef; not serialized.
	CallKwargs map[string]*KwargTree `json:"-"`
}

type ExprKind string

const (
	ExprLiteralString ExprKind = "literal_string"
	ExprLiteralInt    ExprKind = "literal_int"
	ExprLiteralFloat  ExprKind = "literal_float"
	ExprLiteralBool   ExprKind = "literal_bool"
	ExprLiteralNone   ExprKind = "literal_none"
	ExprNameRef       ExprKind = "name_ref"
	ExprList          ExprKind = "list"
	ExprCall          ExprKind = "call"
	ExprUnknown       ExprKind = "unknown"
)

type ToolRef struct {
	Name     string   `json:"name"`
	Resolved *ToolDef `json:"-"`
	External bool     `json:"external"`
}

type AgentRef struct {
	Name     string    `json:"name"`
	Resolved *AgentDef `json:"-"`
	External bool      `json:"external"`
}

type GuardrailRef struct {
	Name     string        `json:"name"`
	Resolved *GuardrailDef `json:"-"`
	External bool          `json:"external"`
}

// AgentDef is one discovered agent declaration in the repo.
type AgentDef struct {
	SDK            SDK             `json:"sdk"`
	Class          string          `json:"class"`    // "Agent", "SandboxAgent", "AgentDefinition", "QueryMainAgent" (TS: main thread of a query() call), or one of the ADK Class values
	Language       Language        `json:"language"` // populated by every discovery path
	Location                       // file_path / start_line / end_line (flat in JSON via anonymous embed)
	Name           string          `json:"name"` // from name= kwarg literal
	VarName        string          `json:"-"`    // assignment-target identifier (for in-file edge resolution; not serialized)
	Kwargs         *KwargTree      `json:"kwargs"`
	ToolRefs       []ToolRef       `json:"tool_refs"`
	HostedToolRefs []HostedToolRef `json:"hosted_tool_refs"`
	MCPServerRefs  []MCPServerRef  `json:"mcp_server_refs"`
	HandoffRefs    []AgentRef      `json:"handoff_refs"`
	InputGuards    []GuardrailRef  `json:"input_guards"`
	OutputGuards   []GuardrailRef  `json:"output_guards"`
	Opaque         bool            `json:"opaque"` // true if Agent(**config) or tools=non-literal
}

type GuardrailKind string

const (
	GuardrailInput      GuardrailKind = "input"
	GuardrailOutput     GuardrailKind = "output"
	GuardrailToolInput  GuardrailKind = "tool_input"
	GuardrailToolOutput GuardrailKind = "tool_output"
)

type GuardrailDef struct {
	Name     string        `json:"name"`
	VarName  string        `json:"var_name,omitempty"` // const-binding name (TS); empty for Python decorator-defined guardrails
	Kind     GuardrailKind `json:"kind"`
	Location               // file_path / start_line / end_line (flat in JSON via anonymous embed)
}

type SessionUse struct {
	Class    string `json:"class"` // "SQLiteSession", "EncryptedSession", ...
	Location        // file_path / start_line / end_line (flat in JSON via anonymous embed)
}

// HostedToolDef is one OpenAI Agents SDK hosted tool instance (WebSearchTool,
// FileSearchTool, ComputerTool, etc.) found inside an agent's tools=[...]
// list. Hosted tools have no function body — they are SDK-managed runtimes —
// so unlike ToolDef they carry no docstring, params, or facts.
type HostedToolDef struct {
	Class    string     `json:"class"` // "WebSearchTool", "FileSearchTool", "ComputerTool", ...
	SDK      SDK        `json:"sdk"`
	Location            // file_path / start_line / end_line (flat in JSON via anonymous embed)
	Kwargs   *KwargTree `json:"kwargs,omitempty"`
}

// HostedToolRef points from an AgentDef to a HostedToolDef. Parallels ToolRef.
type HostedToolRef struct {
	Class    string         `json:"class"`
	Resolved *HostedToolDef `json:"-"`
	DefIndex int            `json:"-"` // pre-sort index into inv.HostedTools; remapped after sort. -1 = not resolvable via inventory remap.
}

// MCPServerDef is one discovered MCP server. Source of truth for class
// names:
//   - Python: openai-agents-python/src/agents/mcp/server.py
//     ("MCPServerStdio" | "MCPServerSse" | "MCPServerStreamableHttp")
//   - TS:     @anthropic-ai/claude-agent-sdk type defs
//     ("McpStdioServerConfig" | "McpSSEServerConfig" |
//     "McpHttpServerConfig" | "McpSdkServerConfigWithInstance" |
//     "createSdkMcpServer")
type MCPServerDef struct {
	Class     string     `json:"class"`
	VarName   string     `json:"var_name,omitempty"` // const-binding name (TS); empty for Python
	Transport string     `json:"transport"`          // "stdio" | "sse" | "streamable_http" | "http" | "sdk"
	SDK       SDK        `json:"sdk"`
	Language  Language   `json:"language"` // populated by every discovery path
	Location             // file_path / start_line / end_line (flat in JSON via anonymous embed)
	Kwargs    *KwargTree `json:"kwargs,omitempty"`
}

// MCPServerRef points from an AgentDef to a discovered MCPServerDef.
type MCPServerRef struct {
	Class    string        `json:"class"`
	Resolved *MCPServerDef `json:"-"`
	External bool          `json:"external"`
	DefIndex int           `json:"-"` // pre-sort index into inv.MCPServers; remapped after sort. -1 = external / TS / not resolvable.
}

// SubagentDef is one parsed Claude Code subagent markdown definition (a
// `.claude/agents/*.md` file, or a flat-collection .md matched by frontmatter
// shape). Tools keeps the raw frontmatter tokens verbatim (both built-in names
// like "Read" and MCP refs like "mcp__server__tool"); ToolGrants carries the
// same tokens parsed through the permission grammar.
type SubagentDef struct {
	Name            string      `json:"name"`
	Description     string      `json:"description,omitempty"`
	Tools           []string    `json:"tools,omitempty"`
	ToolGrants      []ToolGrant `json:"tool_grants,omitempty"`
	DisallowedTools []string    `json:"disallowed_tools,omitempty"`
	Model           string      `json:"model,omitempty"`
	PermissionMode  string      `json:"permission_mode,omitempty"`
	MCPServers      []string    `json:"mcp_servers,omitempty"`
	Skills          []string    `json:"skills,omitempty"`
	HasHooks        bool        `json:"has_hooks,omitempty"`
	Isolation       string      `json:"isolation,omitempty"`
	Location                    // file_path / start_line / end_line (flat in JSON via anonymous embed)
}

// SkillDef is one parsed Claude Code skill (a SKILL.md file). allowed-tools and
// disallowed-tools may be space-separated or a YAML list; AllowedTools keeps the
// verbatim tokens and ToolGrants the parsed grammar. DisableModelInvocation and
// UserInvocable mirror the invocation-control frontmatter; Context/Agent capture
// the `context: fork` subagent-execution form; HasHooks records a `hooks:` block.
//
// The body-fact fields are parsed from the markdown body below the frontmatter —
// a skill's high-risk surface. DynamicExecCommands holds the shell payloads from
// dynamic-context injection (the inline !`cmd` form and ```! fenced blocks),
// which Claude Code runs during preprocessing, before the model sees the
// rendered skill — so model-level prompt-injection defenses never see them.
// ExternalURLs and InjectionMarkers carry the indirect- and direct-injection
// signals found in the body. BundledFiles inventories the skill's other shipped
// files (scripts are the highest-risk bundled surface).
type SkillDef struct {
	Name                   string        `json:"name"`
	Description            string        `json:"description,omitempty"`
	AllowedTools           []string      `json:"allowed_tools,omitempty"`
	ToolGrants             []ToolGrant   `json:"tool_grants,omitempty"`
	DisallowedTools        []string      `json:"disallowed_tools,omitempty"`
	ArgumentHint           string        `json:"argument_hint,omitempty"`
	DisableModelInvocation bool          `json:"disable_model_invocation,omitempty"`
	UserInvocable          *bool         `json:"user_invocable,omitempty"`
	Context                string        `json:"context,omitempty"`
	Agent                  string        `json:"agent,omitempty"`
	HasHooks               bool          `json:"has_hooks,omitempty"`
	DynamicExecCommands    []string      `json:"dynamic_exec_commands,omitempty"`
	ExternalURLs           []string      `json:"external_urls,omitempty"`
	InjectionMarkers       []string      `json:"injection_markers,omitempty"`
	BundledFiles           []BundledFile `json:"bundled_files,omitempty"`
	Location                             // file_path = SKILL.md path
}

// BundledFile is one non-SKILL.md file shipped in a skill's directory tree. Kind
// classifies it by extension for downstream analysis: "script" (executable code
// a skill runs via bash — the highest-risk bundled surface), "markdown"
// (additional instructions/reference), "binary" (precompiled/opaque), or
// "resource" (everything else: templates, data, assets).
type BundledFile struct {
	Path string `json:"path"` // repo-relative, slash-separated
	Kind string `json:"kind"`
}

// SlashCommandDef is one parsed .claude/commands/*.md slash command. The command
// name is the file basename without extension (Claude Code derives the command
// from the path, not frontmatter). allowed-tools follows the skill grammar.
type SlashCommandDef struct {
	Name                   string      `json:"name"`
	Description            string      `json:"description,omitempty"`
	AllowedTools           []string    `json:"allowed_tools,omitempty"`
	ToolGrants             []ToolGrant `json:"tool_grants,omitempty"`
	Model                  string      `json:"model,omitempty"`
	ArgumentHint           string      `json:"argument_hint,omitempty"`
	DisableModelInvocation bool        `json:"disable_model_invocation,omitempty"`
	Location                           // file_path = command .md path
}

// PluginManifest is one parsed .claude-plugin manifest: a plugin.json (single
// plugin) or a marketplace.json (a catalog of plugins each pointing at a source
// directory). Kind distinguishes the two. Plugins lists the catalog entries
// (empty for a plain plugin.json).
type PluginManifest struct {
	Kind     string        `json:"kind"` // "plugin" | "marketplace"
	Name     string        `json:"name,omitempty"`
	Plugins  []PluginEntry `json:"plugins,omitempty"`
	Location               // file_path = the .json path
}

// PluginEntry is one entry in a marketplace.json plugins[] array.
type PluginEntry struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

// PermissionRule is one parsed entry from .claude/settings.json permissions
// lists. Raw preserves the original string for finding attribution; Tool and
// Pattern carry the parsed grammar.
type PermissionRule struct {
	Tool    string `json:"tool"`              // "Bash" | "Read" | "Edit" | "WebFetch" | "MCP" | "Agent"
	Pattern string `json:"pattern,omitempty"` // empty for bare "Bash", "npm run *" for "Bash(npm run *)"
	Raw     string `json:"raw"`
	Line    int    `json:"line"` // 1-indexed line of this rule's string literal in settings.json
}

// ToolGrant is one parsed entry from a markdown agent's `tools:` or
// `allowed-tools:` frontmatter list. It reuses the settings.json permission
// grammar (see ParsePermissionRule): a bare tool ("Read"), a parametered tool
// ("Bash(npm run *)", "Agent(worker, researcher)"), or an MCP tool reference
// ("mcp__server__tool", parsed as Tool="MCP"). Raw preserves the original
// token verbatim for attribution.
type ToolGrant struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern,omitempty"`
	Raw     string `json:"raw"`
}

// ClaudePermissions is the parsed permissions block.
type ClaudePermissions struct {
	Allow []PermissionRule `json:"allow,omitempty"`
	Deny  []PermissionRule `json:"deny,omitempty"`
	Ask   []PermissionRule `json:"ask,omitempty"`
}

// ClaudeSettings is one parsed .claude/settings.json (or settings.local.json).
type ClaudeSettings struct {
	Location                          // file_path / start_line / end_line (flat in JSON via anonymous embed)
	Permissions     ClaudePermissions `json:"permissions"`
	DefaultMode     string            `json:"default_mode,omitempty"`
	AdditionalDirs  []string          `json:"additional_directories,omitempty"`
	HasEnvBlock     bool              `json:"has_env_block"`
	HasHooks        bool              `json:"has_hooks"`
	HasSandboxBlock bool              `json:"has_sandbox_block"`
}

// ClaudeAgentOptionsDef is one ClaudeAgentOptions(...) construction discovered
// in code (claude-agent-sdk). It carries the constructor kwargs so repo-scope
// rules can inspect session-level configuration — notably permission_mode,
// which is the session-wide analogue of .claude/settings.json's defaultMode and
// the place most apps actually set a permission bypass. Kwargs is an in-memory
// carrier (not serialized); Opaque is true when the call used **unpacking that
// makes the kwarg set untrustworthy.
type ClaudeAgentOptionsDef struct {
	Location
	Kwargs *KwargTree `json:"-"`
	Opaque bool       `json:"opaque,omitempty"`
}

package models

// Location is the file/line attribution for a discovered inventory entity.
// Line and EndLine are both 1-indexed and inclusive. Single-line entities
// set EndLine == Line; that is a valid state, not a placeholder.
//
// Location is intended to be embedded anonymously into entity structs so
// JSON serialization stays flat (entity.file_path, entity.line,
// entity.end_line). Consumers that read entity.file_path / entity.line
// today are unaffected; entity.end_line is additive.
type Location struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
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
	Kind ExprKind `json:"kind"`
	Text string   `json:"text"`           // raw source text
	List []Expr   `json:"list,omitempty"` // populated when Kind == ExprList
}

type ExprKind string

const (
	ExprLiteralString ExprKind = "literal_string"
	ExprLiteralInt    ExprKind = "literal_int"
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
	Location                        // file_path / line / end_line (flat in JSON via anonymous embed)
	Name           string          `json:"name"`           // from name= kwarg literal
	VarName        string          `json:"-"`              // assignment-target identifier (for in-file edge resolution; not serialized)
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
	GuardrailInput  GuardrailKind = "input"
	GuardrailOutput GuardrailKind = "output"
)

type GuardrailDef struct {
	Name     string        `json:"name"`
	Kind     GuardrailKind `json:"kind"`
	Location               // file_path / line / end_line (flat in JSON via anonymous embed)
}

type SessionUse struct {
	Class    string `json:"class"` // "SQLiteSession", "EncryptedSession", ...
	Location          // file_path / line / end_line (flat in JSON via anonymous embed)
}

// HostedToolDef is one OpenAI Agents SDK hosted tool instance (WebSearchTool,
// FileSearchTool, ComputerTool, etc.) found inside an agent's tools=[...]
// list. Hosted tools have no function body — they are SDK-managed runtimes —
// so unlike ToolDef they carry no docstring, params, or facts.
type HostedToolDef struct {
	Class    string     `json:"class"` // "WebSearchTool", "FileSearchTool", "ComputerTool", ...
	SDK      SDK        `json:"sdk"`
	Location              // file_path / line / end_line (flat in JSON via anonymous embed)
	Kwargs   *KwargTree `json:"kwargs,omitempty"`
}

// HostedToolRef points from an AgentDef to a HostedToolDef. Parallels ToolRef.
type HostedToolRef struct {
	Class    string         `json:"class"`
	Resolved *HostedToolDef `json:"-"`
}

// MCPServerDef is one discovered MCP server. Source of truth for class
// names:
//   - Python: openai-agents-python/src/agents/mcp/server.py
//     ("MCPServerStdio" | "MCPServerSse" | "MCPServerStreamableHttp")
//   - TS:     @anthropic-ai/claude-agent-sdk type defs
//     ("McpStdioServerConfig" | "McpSSEServerConfig" |
//      "McpHttpServerConfig" | "McpSdkServerConfigWithInstance" |
//      "createSdkMcpServer")
type MCPServerDef struct {
	Class     string     `json:"class"`
	Transport string     `json:"transport"` // "stdio" | "sse" | "streamable_http" | "http" | "sdk"
	SDK       SDK        `json:"sdk"`
	Language  Language   `json:"language"` // populated by every discovery path
	Location              // file_path / line / end_line (flat in JSON via anonymous embed)
	Kwargs    *KwargTree `json:"kwargs,omitempty"`
}

// MCPServerRef points from an AgentDef to a discovered MCPServerDef.
type MCPServerRef struct {
	Class    string        `json:"class"`
	Resolved *MCPServerDef `json:"-"`
	External bool          `json:"external"`
}

// SubagentDef is one parsed `.claude/agents/*.md` definition. The tools field
// is the comma-separated list from frontmatter; both built-in tool names
// ("Read", "Bash") and MCP-tool names ("mcp__server__tool") appear here.
type SubagentDef struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`
	Location              // file_path / line / end_line (flat in JSON via anonymous embed)
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

// ClaudePermissions is the parsed permissions block.
type ClaudePermissions struct {
	Allow []PermissionRule `json:"allow,omitempty"`
	Deny  []PermissionRule `json:"deny,omitempty"`
	Ask   []PermissionRule `json:"ask,omitempty"`
}

// ClaudeSettings is one parsed .claude/settings.json (or settings.local.json).
type ClaudeSettings struct {
	Location                          // file_path / line / end_line (flat in JSON via anonymous embed)
	Permissions     ClaudePermissions `json:"permissions"`
	DefaultMode     string            `json:"default_mode,omitempty"`
	AdditionalDirs  []string          `json:"additional_directories,omitempty"`
	HasEnvBlock     bool              `json:"has_env_block"`
	HasHooks        bool              `json:"has_hooks"`
	HasSandboxBlock bool              `json:"has_sandbox_block"`
}

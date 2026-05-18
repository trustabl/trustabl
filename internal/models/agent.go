package models

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
	SDK          SDK            `json:"sdk"`
	Class        string         `json:"class"`    // "Agent", "SandboxAgent", "AgentDefinition"
	FilePath     string         `json:"file_path"`
	Line         int            `json:"line"`
	EndLine      int            `json:"end_line"`
	Name         string         `json:"name"` // from name= kwarg literal
	Kwargs       *KwargTree     `json:"kwargs"`
	ToolRefs     []ToolRef      `json:"tool_refs"`
	HandoffRefs  []AgentRef     `json:"handoff_refs"`
	InputGuards  []GuardrailRef `json:"input_guards"`
	OutputGuards []GuardrailRef `json:"output_guards"`
	Opaque       bool           `json:"opaque"` // true if Agent(**config) or tools=non-literal
}

type GuardrailKind string

const (
	GuardrailInput  GuardrailKind = "input"
	GuardrailOutput GuardrailKind = "output"
)

type GuardrailDef struct {
	Name     string        `json:"name"`
	Kind     GuardrailKind `json:"kind"`
	FilePath string        `json:"file_path"`
	Line     int           `json:"line"`
}

type SessionUse struct {
	Class    string `json:"class"` // "SQLiteSession", "EncryptedSession", ...
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
}

type HostedToolDef struct {
	Class    string     `json:"class"` // "WebSearchTool", "ComputerTool", ...
	FilePath string     `json:"file_path"`
	Line     int        `json:"line"`
	Kwargs   *KwargTree `json:"kwargs"`
}

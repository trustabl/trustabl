// Package sarif renders a Trustabl ScanResult as a SARIF 2.1.0 document for
// stdout consumption by GitHub Code Scanning and other SARIF-aware tools.
// See .superpowers/specs/2026-05-24-sarif-output-design.md for the decisions
// behind the field mapping.
package sarif

// Log is the SARIF document root. Trustabl always emits version "2.1.0" with
// exactly one run.
type Log struct {
	Version string `json:"version"`
	Schema  string `json:"$schema"`
	Runs    []Run  `json:"runs"`
}

type Run struct {
	Tool                     Tool                        `json:"tool"`
	Invocations              []Invocation                `json:"invocations,omitempty"`
	Results                  []Result                    `json:"results"`
	AutomationDetails        *AutomationDetails          `json:"automationDetails,omitempty"`
	OriginalUriBaseIds       map[string]ArtifactLocation `json:"originalUriBaseIds,omitempty"`
	VersionControlProvenance []VersionControlProvenance  `json:"versionControlProvenance,omitempty"`
}

type Tool struct {
	Driver ToolComponent `json:"driver"`
}

type ToolComponent struct {
	Name            string                `json:"name"`
	FullName        string                `json:"fullName,omitempty"`
	InformationURI  string                `json:"informationUri,omitempty"`
	Version         string                `json:"version,omitempty"`
	SemanticVersion string                `json:"semanticVersion,omitempty"`
	Rules           []ReportingDescriptor `json:"rules,omitempty"`
	Properties      map[string]any        `json:"properties,omitempty"`
}

// ReportingDescriptor is one rule entry in tool.driver.rules.
type ReportingDescriptor struct {
	ID                   string                  `json:"id"`
	ShortDescription     *Message                `json:"shortDescription,omitempty"`
	FullDescription      *Message                `json:"fullDescription,omitempty"`
	Help                 *Message                `json:"help,omitempty"`
	DefaultConfiguration *ReportingConfiguration `json:"defaultConfiguration,omitempty"`
	Properties           map[string]any          `json:"properties,omitempty"`
}

type ReportingConfiguration struct {
	Level string `json:"level,omitempty"` // none | note | warning | error
}

// Result is one finding.
type Result struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           *int              `json:"ruleIndex,omitempty"`
	Kind                string            `json:"kind,omitempty"` // omit for default "fail"; set "informational" otherwise
	Message             Message           `json:"message"`
	Locations           []Location        `json:"locations,omitempty"`
	Rank                *float64          `json:"rank,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	Properties          map[string]any    `json:"properties,omitempty"`
}

type Location struct {
	PhysicalLocation *PhysicalLocation `json:"physicalLocation,omitempty"`
	LogicalLocations []LogicalLocation `json:"logicalLocations,omitempty"`
}

type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           *Region          `json:"region,omitempty"`
}

type ArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

type Region struct {
	StartLine int `json:"startLine,omitempty"`
}

type LogicalLocation struct {
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
}

type Invocation struct {
	ExecutionSuccessful        bool           `json:"executionSuccessful"`
	ToolExecutionNotifications []Notification `json:"toolExecutionNotifications,omitempty"`
}

type Notification struct {
	Level      string                        `json:"level,omitempty"`
	Message    Message                       `json:"message"`
	Descriptor *ReportingDescriptorReference `json:"descriptor,omitempty"`
	Properties map[string]any                `json:"properties,omitempty"`
}

type ReportingDescriptorReference struct {
	Index int `json:"index"`
}

type AutomationDetails struct {
	ID string `json:"id,omitempty"`
}

type VersionControlProvenance struct {
	RepositoryURI string `json:"repositoryUri"`
}

// Message is SARIF's MultiformatMessageString reduced to plain text. Trustabl
// rule explanations and fixes are paragraph prose, not rich text.
type Message struct {
	Text string `json:"text"`
}

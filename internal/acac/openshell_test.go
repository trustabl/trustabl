package acac

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func toolWithCaptures(name, file string, hosts, writes []string, facts map[string]string) models.ToolDef {
	t := models.ToolDef{
		Name:         name,
		Kind:         models.KindOpenAITool,
		Language:     models.LanguagePython,
		HTTPHosts:    hosts,
		FSWritePaths: writes,
		Facts:        facts,
	}
	t.FilePath = file
	return t
}

func agentWiring(tools ...*models.ToolDef) models.AgentDef {
	a := models.AgentDef{Name: "main"}
	a.FilePath = "main.py"
	for _, t := range tools {
		a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: t.Name, Resolved: t})
	}
	return a
}

func TestBuildOpenShellPolicy_Derivations(t *testing.T) {
	fetch := toolWithCaptures("fetch_status", "tools.py",
		[]string{"status.example.com:443"}, nil, map[string]string{"http_call": "true"})
	save := toolWithCaptures("save_report", "tools.py",
		nil, []string{"/workspace/out/report.txt", "relative.txt"}, nil)
	private := toolWithCaptures("internal_probe", "tools.py",
		[]string{"10.0.0.5:443", "localhost:8080"}, nil, nil)
	dynamic := toolWithCaptures("fetch_user", "tools.py",
		nil, nil, map[string]string{"http_call": "true", "dynamic_url": "true"})
	agent := agentWiring(&fetch, &save, &private, &dynamic)

	p := BuildOpenShellPolicy(models.ScanResult{}, agent)

	// Baseline (incl. /dev/null) + captured absolute write path; the relative
	// one is a note. read_write is sorted, so /dev/null leads.
	wantRW := []string{"/dev/null", "/sandbox", "/tmp", "/workspace/out/report.txt"}
	if strings.Join(p.ReadWrite, "|") != strings.Join(wantRW, "|") {
		t.Errorf("ReadWrite = %v, want %v", p.ReadWrite, wantRW)
	}
	if p.RunAsUser != "sandbox" || p.RunAsGroup != "sandbox" {
		t.Errorf("process = %s/%s, want sandbox/sandbox", p.RunAsUser, p.RunAsGroup)
	}

	// fetch_status (public host) and internal_probe (private IP via allowed_ips)
	// produce network policies; loopback and dynamic URLs do not.
	if len(p.Network) != 2 {
		t.Fatalf("network policies = %d, want 2: %+v", len(p.Network), p.Network)
	}
	np := p.Network[0]
	if np.Key != "fetch_status" || np.Name != "fetch-status" {
		t.Errorf("policy key/name = %s/%s", np.Key, np.Name)
	}
	if len(np.Endpoints) != 1 || np.Endpoints[0].Host != "status.example.com" || np.Endpoints[0].Port != 443 {
		t.Errorf("endpoints = %+v", np.Endpoints)
	}
	// The agent entrypoint (main.py) is derivable, so binaries reflect the real
	// requesting script mapped into the sandbox app root — not the interpreter
	// guess — and carry no review marker.
	if len(np.Binaries) != 1 || np.Binaries[0].Path != "/app/main.py" || !np.Binaries[0].Derived {
		t.Errorf("binaries = %+v, want one derived /app/main.py", np.Binaries)
	}

	// internal_probe: the private 10.0.0.5 becomes a host-less allowed_ips
	// endpoint; localhost (loopback) stays dropped.
	probe := p.Network[1]
	if probe.Key != "internal_probe" || len(probe.Endpoints) != 1 {
		t.Fatalf("internal_probe policy = %+v", probe)
	}
	pe := probe.Endpoints[0]
	if pe.Host != "" || len(pe.AllowedIPs) != 1 || pe.AllowedIPs[0] != "10.0.0.5" || pe.Marker == "" {
		t.Errorf("internal_probe endpoint = %+v, want host-less allowed_ips [10.0.0.5] with marker", pe)
	}

	// Review notes: relative write path, loopback host, one dynamic URL.
	if len(p.ReviewNotes) != 3 {
		t.Errorf("review notes = %d, want 3:\n%s", len(p.ReviewNotes), strings.Join(p.ReviewNotes, "\n"))
	}

	if err := ValidateOpenShellPolicy(p); err != nil {
		t.Errorf("built policy must validate: %v", err)
	}
}

func TestBuildOpenShellPolicy_EntrypointFallbackToGuess(t *testing.T) {
	fetch := toolWithCaptures("fetch_status", "tools.py",
		[]string{"status.example.com:443"}, nil, map[string]string{"http_call": "true"})
	agent := agentWiring(&fetch)
	agent.FilePath = "" // entrypoint not derivable → fall back to the guess

	p := BuildOpenShellPolicy(models.ScanResult{}, agent)
	if len(p.Network) != 1 {
		t.Fatalf("network policies = %d, want 1", len(p.Network))
	}
	bins := p.Network[0].Binaries
	if len(bins) != 1 || bins[0].Path != "/usr/bin/python3" || bins[0].Derived {
		t.Errorf("binaries = %+v, want the non-derived interpreter guess /usr/bin/python3", bins)
	}
	if err := ValidateOpenShellPolicy(p); err != nil {
		t.Errorf("policy must validate: %v", err)
	}
}

func TestNormalizeEntrypointPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"main.py", "main.py"},
		{"./main.py", "main.py"},
		{"/abs/main.py", "abs/main.py"},
		{"src\\agent\\main.py", "src/agent/main.py"},
		{"pkg/../etc/passwd", ""}, // escapes the app root → not derivable
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeEntrypointPath(c.in); got != c.want {
			t.Errorf("normalizeEntrypointPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEmitOpenShell_BinaryMarkerOnlyWhenGuessed(t *testing.T) {
	emit := func(b OpenShellBinary) string {
		p := OpenShellPolicy{
			ReadOnly:   []string{"/usr"},
			ReadWrite:  []string{"/sandbox"},
			RunAsUser:  "sandbox",
			RunAsGroup: "sandbox",
			Network: []OpenShellNetworkPolicy{{
				Key: "t", Name: "t",
				Endpoints: []OpenShellEndpoint{{Host: "api.example.com", Port: 443, Access: "read-only"}},
				Binaries:  []OpenShellBinary{b},
			}},
		}
		out, err := EmitOpenShellPolicy(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	const marker = "confirm the interpreter path"
	if got := emit(OpenShellBinary{Path: "/app/main.py", Derived: true}); strings.Contains(got, marker) {
		t.Errorf("derived binary must not carry the interpreter marker:\n%s", got)
	}
	if got := emit(OpenShellBinary{Path: "/usr/bin/python3", Derived: false}); !strings.Contains(got, marker) {
		t.Errorf("guessed binary must carry the interpreter marker:\n%s", got)
	}
}

func TestBuildOpenShellPolicy_AccessFromMethods(t *testing.T) {
	cases := []struct {
		methods []string
		want    string
	}{
		{nil, "read-only"},                        // no methods → conservative
		{[]string{"GET"}, "read-only"},            // read verbs only
		{[]string{"GET", "HEAD", "OPTIONS"}, "read-only"},
		{[]string{"GET", "POST"}, "read-write"},   // a write verb
		{[]string{"PUT"}, "read-write"},
		{[]string{"PATCH"}, "read-write"},
		{[]string{"POST", "DELETE"}, "full"},      // delete escalates to full
		{[]string{"DELETE"}, "full"},
	}
	for _, c := range cases {
		tool := toolWithCaptures("api", "tools.py",
			[]string{"api.example.com:443"}, nil, map[string]string{"http_call": "true"})
		tool.HTTPMethods = c.methods
		agent := agentWiring(&tool)
		p := BuildOpenShellPolicy(models.ScanResult{}, agent)
		if len(p.Network) != 1 || len(p.Network[0].Endpoints) != 1 {
			t.Fatalf("methods %v: want one endpoint, got %+v", c.methods, p.Network)
		}
		if got := p.Network[0].Endpoints[0].Access; got != c.want {
			t.Errorf("methods %v: access = %q, want %q", c.methods, got, c.want)
		}
	}
}

func TestBuildOpenShellPolicy_UnspecifiedHostDropped(t *testing.T) {
	// 0.0.0.0 is always blocked by OpenShell — it must become a review note, not
	// an endpoint or an allowed_ips entry (audit finding).
	tool := toolWithCaptures("probe", "tools.py",
		[]string{"0.0.0.0:443"}, nil, map[string]string{"http_call": "true"})
	p := BuildOpenShellPolicy(models.ScanResult{}, agentWiring(&tool))
	if len(p.Network) != 0 {
		t.Errorf("0.0.0.0 must not produce a network policy, got %+v", p.Network)
	}
	hasNote := false
	for _, n := range p.ReviewNotes {
		if strings.Contains(n, "unspecified") {
			hasNote = true
		}
	}
	if !hasNote {
		t.Errorf("expected an 'unspecified' review note, got %v", p.ReviewNotes)
	}
}

func TestBuildOpenShellPolicy_L7Rules(t *testing.T) {
	// Every call to the host has a specific path → emit rules, omit access.
	tool := toolWithCaptures("api", "tools.py",
		[]string{"api.example.com:443"}, nil, map[string]string{"http_call": "true"})
	tool.HTTPMethods = []string{"GET", "POST"}
	tool.HTTPCalls = []models.HTTPCall{
		{HostPort: "api.example.com:443", Method: "GET", Path: "/status"},
		{HostPort: "api.example.com:443", Method: "POST", Path: "/ingest"},
	}
	p := BuildOpenShellPolicy(models.ScanResult{}, agentWiring(&tool))
	ep := p.Network[0].Endpoints[0]
	if ep.Access != "" {
		t.Errorf("rules present → access must be empty, got %q", ep.Access)
	}
	if len(ep.Rules) != 2 ||
		ep.Rules[0] != (OpenShellL7Rule{Method: "GET", Path: "/status"}) ||
		ep.Rules[1] != (OpenShellL7Rule{Method: "POST", Path: "/ingest"}) {
		t.Errorf("Rules = %+v", ep.Rules)
	}
	if err := ValidateOpenShellPolicy(p); err != nil {
		t.Errorf("rules policy must validate: %v", err)
	}
}

func TestBuildOpenShellPolicy_RulesFallbackToAccess(t *testing.T) {
	// A call without a specific path (root) means we cannot scope by path, so
	// the whole host falls back to the coarse access preset.
	tool := toolWithCaptures("api", "tools.py",
		[]string{"api.example.com:443"}, nil, map[string]string{"http_call": "true"})
	tool.HTTPMethods = []string{"GET", "POST"}
	tool.HTTPCalls = []models.HTTPCall{
		{HostPort: "api.example.com:443", Method: "POST", Path: "/ingest"},
		{HostPort: "api.example.com:443", Method: "GET", Path: ""}, // root → unscopable
	}
	p := BuildOpenShellPolicy(models.ScanResult{}, agentWiring(&tool))
	ep := p.Network[0].Endpoints[0]
	if len(ep.Rules) != 0 {
		t.Errorf("a path-less call must force access fallback, got rules %+v", ep.Rules)
	}
	if ep.Access != "read-write" {
		t.Errorf("access fallback = %q, want read-write", ep.Access)
	}
}

func TestValidateOpenShellPolicy_RejectsEachConstraint(t *testing.T) {
	valid := func() OpenShellPolicy {
		return OpenShellPolicy{
			ReadOnly:   []string{"/usr", "/lib", "/etc"},
			ReadWrite:  []string{"/sandbox", "/tmp"},
			RunAsUser:  "sandbox",
			RunAsGroup: "sandbox",
			Network: []OpenShellNetworkPolicy{{
				Key: "t", Name: "t",
				Endpoints: []OpenShellEndpoint{{Host: "api.example.com", Port: 443, Access: "read-only"}},
				Binaries:  []OpenShellBinary{{Path: "/usr/bin/python3"}},
			}},
		}
	}
	if err := ValidateOpenShellPolicy(valid()); err != nil {
		t.Fatalf("baseline fixture must validate: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*OpenShellPolicy)
		wantSub string
	}{
		{"relative path", func(p *OpenShellPolicy) { p.ReadWrite = append(p.ReadWrite, "out.txt") }, "not absolute"},
		{"dotdot segment", func(p *OpenShellPolicy) { p.ReadWrite = append(p.ReadWrite, "/sandbox/../etc") }, "'..' segment"},
		{"bare slash", func(p *OpenShellPolicy) { p.ReadWrite = append(p.ReadWrite, "/") }, "overly broad"},
		{"broad writable root", func(p *OpenShellPolicy) { p.ReadWrite = append(p.ReadWrite, "/etc") }, "overly broad"},
		{"path too long", func(p *OpenShellPolicy) { p.ReadWrite = append(p.ReadWrite, "/"+strings.Repeat("a", 4096)) }, "exceeds 4096"},
		{"root user", func(p *OpenShellPolicy) { p.RunAsUser = "root" }, "must not run as root"},
		{"uid zero", func(p *OpenShellPolicy) { p.RunAsUser = "0" }, "must not run as root"},
		{"empty user", func(p *OpenShellPolicy) { p.RunAsUser = "" }, "must be set"},
		{"mid-label wildcard", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].Host = "api.*.example.com"
		}, "leading first-label"},
		{"second wildcard", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].Host = "*.sub.*.example.com"
		}, "leading first-label"},
		{"tld wildcard", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].Host = "*.com"
		}, "TLD wildcard"},
		{"loopback allowed_ip", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].AllowedIPs = []string{"127.0.0.1"}
		}, "loopback/link-local"},
		{"unspecified allowed_ip", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].AllowedIPs = []string{"0.0.0.0"}
		}, "always blocked"},
		{"loopback endpoint", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].Host = "127.0.0.1"
		}, "loopback"},
		{"link-local endpoint", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].Host = "169.254.1.1"
		}, "link-local"},
		{"private endpoint", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].Host = "192.168.1.10"
		}, "private-range"},
		{"port out of range", func(p *OpenShellPolicy) {
			p.Network[0].Endpoints[0].Port = 0
		}, "out-of-range port"},
		{"relative binary", func(p *OpenShellPolicy) {
			p.Network[0].Binaries = []OpenShellBinary{{Path: "python3"}}
		}, "not absolute"},
		{"path-count cap", func(p *OpenShellPolicy) {
			for i := 0; i < 300; i++ {
				p.ReadWrite = append(p.ReadWrite, "/sandbox/d"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+"/"+strings.Repeat("y", 1+i%5))
			}
		}, "path cap"},
	}
	for _, c := range cases {
		p := valid()
		c.mutate(&p)
		err := ValidateOpenShellPolicy(p)
		if err == nil {
			t.Errorf("%s: validation passed, want error containing %q", c.name, c.wantSub)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: error %q does not contain %q", c.name, err, c.wantSub)
		}
	}
}

func TestOpenShellGoldenPolicy(t *testing.T) {
	result := scanCorpus(t, "acac-static-capture")
	agent, err := SelectAgent(result, "")
	if err != nil {
		t.Fatal(err)
	}
	policy := BuildOpenShellPolicy(result, agent)
	if err := ValidateOpenShellPolicy(policy); err != nil {
		t.Fatalf("validate: %v", err)
	}
	got, err := EmitOpenShellPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "acac-static-capture.openshell.yaml.golden")
	if *update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to regenerate): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("policy differs from golden (run with -update if intended)\n--- got ---\n%s", got)
	}

	// Byte-stability across full re-generation.
	delete(corpusScans, "acac-static-capture")
	result2 := scanCorpus(t, "acac-static-capture")
	agent2, _ := SelectAgent(result2, "")
	again, err := EmitOpenShellPolicy(BuildOpenShellPolicy(result2, agent2))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, again) {
		t.Error("second generation differs: the policy path is not deterministic")
	}
}

func TestOpenShellDynamicOnlyToolYieldsNoEndpointAndAMarker(t *testing.T) {
	dynamic := toolWithCaptures("fetch_user", "tools.py",
		nil, nil, map[string]string{"http_call": "true", "dynamic_url": "true"})
	agent := agentWiring(&dynamic)
	p := BuildOpenShellPolicy(models.ScanResult{}, agent)
	if len(p.Network) != 0 {
		t.Errorf("dynamic-only tool must yield no network policy, got %+v", p.Network)
	}
	if len(p.ReviewNotes) != 1 || !strings.Contains(p.ReviewNotes[0], "fetch_user") {
		t.Errorf("expected one review note naming the tool, got %v", p.ReviewNotes)
	}
	out, err := EmitOpenShellPolicy(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), MarkerReview) {
		t.Error("emitted policy must carry the review marker comment")
	}
	if !strings.Contains(string(out), "network_policies: {}") {
		t.Errorf("expected explicit empty network_policies block:\n%s", out)
	}
}

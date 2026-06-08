package vulndb

import (
	"math"
	"strings"
)

// cvssBaseScore parses a CVSS v3.0/v3.1 vector and computes its base score per
// the CVSS specification. Returns ok=false for non-3.x vectors or malformed
// input, so the caller can fall back to a default severity. (CVSS 2.0/4.0 are
// rare in OSV records and intentionally not computed here.)
func cvssBaseScore(vector string) (float64, bool) {
	if !strings.HasPrefix(vector, "CVSS:3.0") && !strings.HasPrefix(vector, "CVSS:3.1") {
		return 0, false
	}
	m := map[string]string{}
	for _, part := range strings.Split(vector, "/") {
		if kv := strings.SplitN(part, ":", 2); len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	scope := m["S"]
	prTable := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	if scope == "C" {
		prTable = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
	}
	cia := map[string]float64{"N": 0, "L": 0.22, "H": 0.56}
	av, ok1 := map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}[m["AV"]]
	ac, ok2 := map[string]float64{"L": 0.77, "H": 0.44}[m["AC"]]
	ui, ok3 := map[string]float64{"N": 0.85, "R": 0.62}[m["UI"]]
	pr, ok4 := prTable[m["PR"]]
	c, ok5 := cia[m["C"]]
	i, ok6 := cia[m["I"]]
	a, ok7 := cia[m["A"]]
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7) || (scope != "U" && scope != "C") {
		return 0, false
	}

	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if scope == "U" {
		impact = 6.42 * iss
	} else {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	}
	if impact <= 0 {
		return 0, true
	}
	expl := 8.22 * av * ac * pr * ui
	base := impact + expl
	if scope == "C" {
		base = 1.08 * base
	}
	base = math.Min(base, 10)
	// CVSS roundup to one decimal place.
	return math.Ceil(base*10) / 10, true
}

package attest

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// cosignVersionRE pulls the major version out of `cosign version` output, whose
// relevant line looks like `GitVersion:    v3.1.1`.
var cosignVersionRE = regexp.MustCompile(`GitVersion:\s*v?(\d+)\.`)

// cosignMajor returns cosign's major version number. cosign v3 removed the
// --tlog-upload flag (it defaults --use-signing-config=true, which conflicts with
// it), so no-tlog signing on v3+ must instead pass an explicit no-Rekor
// --signing-config. Callers use this to pick the right flag; on any error they
// fall back to the pre-v3 behavior.
func cosignMajor(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, cosignBinary, "version").CombinedOutput()
	if err != nil {
		return 0, err
	}
	m := cosignVersionRE.FindSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("attest: cannot parse cosign version from %q", out)
	}
	return strconv.Atoi(string(m[1]))
}

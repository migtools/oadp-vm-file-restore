package velerohelpers

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestStreamWithBSLCACert(t *testing.T) {
	cfg := &packages.Config{Mode: packages.NeedTypes | packages.NeedImports}
	pkgs, err := packages.Load(cfg, "github.com/vmware-tanzu/velero/pkg/cmd/util/downloadrequest")
	if err != nil {
		t.Fatalf("failed to load package: %v", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatalf("failed to load package due to errors")
	}

	scope := pkgs[0].Types.Scope()
	if scope.Lookup("StreamWithBSLCACert") != nil {
		t.Logf(`
⚠️  WARNING: StreamWithBSLCACert is now available in the OADP Velero version !

StreamWithBSLCACert is now available in the OADP Velero version !
Add StreamWithBSLCACert() for proper BSL CA certificate handling.

See: https://github.com/vmware-tanzu/velero/pull/8557 for more details.
`)
	}
}

package authorization

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestT1438ResolverEntrypointIsStableAndServiceBacked(t *testing.T) {
	root := repoRoot(t)
	types := readFile(t, filepath.Join(root, "internal/authorization/types.go"))
	service := readFile(t, filepath.Join(root, "internal/authorization/service.go"))

	for _, needle := range []string{
		"type EffectiveResolver interface",
		"ResolveEffective(context.Context, CheckRequest) (ExplainResult, error)",
	} {
		if !strings.Contains(types, needle) {
			t.Fatalf("authorization/types.go missing stable resolver contract %q", needle)
		}
	}
	for _, needle := range []string{
		"func (s *Service) ResolveEffective(ctx context.Context, req CheckRequest) (ExplainResult, error)",
		"exp, err := s.ResolveEffective(ctx, req)",
		"effective, deniedBy, err := s.deriveEffective(ctx, req)",
	} {
		if !strings.Contains(service, needle) {
			t.Fatalf("authorization/service.go missing resolver service chain %q", needle)
		}
	}
}

func TestT1438RealEntrypointsCallResolverNotTransportCheckWrappers(t *testing.T) {
	root := repoRoot(t)
	required := []string{
		"internal/webconsole/api/authz_write.go",
		"internal/admin/api/authz_write.go",
		"internal/projectmanager/service/service.go",
	}
	for _, rel := range required {
		body := readFile(t, filepath.Join(root, rel))
		if !strings.Contains(body, ".ResolveEffective(ctx, req)") && !strings.Contains(body, ".ResolveEffective(r.Context(),") && !strings.Contains(body, ".ResolveEffective(ctx, authz.CheckRequest") {
			t.Fatalf("%s does not call the unified ResolveEffective entrypoint", rel)
		}
	}

	for _, rel := range walkGoFiles(t, filepath.Join(root, "internal/webconsole/api"), filepath.Join(root, "internal/admin/api"), filepath.Join(root, "internal/projectmanager/service")) {
		body := readFile(t, rel)
		for _, forbidden := range []string{".Authorizer.Check(", ".authorizer.Check(", "checkAdminAuthorization(ctx context.Context, d HandlerDeps, req authz.CheckRequest) (authz.AccessDecision, error) {\n\treturn d.Authorizer.Check"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s bypasses ResolveEffective via %q", rel, forbidden)
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		next := filepath.Dir(wd)
		if next == wd {
			t.Fatal("go.mod not found")
		}
		wd = next
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func walkGoFiles(t *testing.T, roots ...string) []string {
	t.Helper()
	var out []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

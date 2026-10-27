// Package archtest enforces the dependency rules of AGENTS.md section 4. A violation
// fails the build, so the architecture cannot erode without somebody deciding it should.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/AtmosferaSoltec/atm-pulso-api"

// goFile is one non-test source file with the packages it imports.
type goFile struct {
	path    string // relative to the repository root, slash separated
	imports []string
	source  string
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found")
		dir = parent
	}
}

func sources(t *testing.T) []goFile {
	t.Helper()
	root := repoRoot(t)
	var files []goFile
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		file := goFile{path: filepath.ToSlash(rel), source: string(raw)}
		for _, spec := range parsed.Imports {
			file.imports = append(file.imports, strings.Trim(spec.Path.Value, `"`))
		}
		files = append(files, file)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)
	return files
}

// moduleOf returns "accounts" for "internal/modules/accounts/app/service.go".
func moduleOf(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "internal/modules/")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "/")
	return name, true
}

func inLayer(path, layer string) bool {
	_, rest, _ := strings.Cut(strings.TrimPrefix(path, "internal/modules/"), "/")
	return strings.HasPrefix(rest, layer+"/")
}

func internalPath(importPath string) (string, bool) {
	return strings.CutPrefix(importPath, modulePath+"/")
}

func TestDomainIsPlainGo(t *testing.T) {
	forbidden := []string{"github.com/gin-gonic", "github.com/jackc", "net/http", "database/sql"}
	for _, file := range sources(t) {
		if _, ok := moduleOf(file.path); !ok || !inLayer(file.path, "domain") {
			continue
		}
		module, _ := moduleOf(file.path)
		for _, imported := range file.imports {
			for _, bad := range forbidden {
				require.Falsef(t, strings.HasPrefix(imported, bad), "%s imports %s: the domain must stay plain Go", file.path, imported)
			}
			if internal, ok := internalPath(imported); ok {
				require.Truef(t, strings.HasPrefix(internal, "internal/modules/"+module+"/domain"),
					"%s imports %s: the domain may only import its own module's domain", file.path, imported)
			}
		}
	}
}

func TestApplicationLayerNeverDependsOnAdapters(t *testing.T) {
	for _, file := range sources(t) {
		if _, ok := moduleOf(file.path); !ok || !inLayer(file.path, "app") {
			continue
		}
		for _, imported := range file.imports {
			require.NotContainsf(t, imported, "/adapters/", "%s imports %s: use cases depend on ports, not adapters", file.path, imported)
			require.Falsef(t, strings.HasPrefix(imported, "github.com/gin-gonic"), "%s imports gin: no HTTP in use cases", file.path)
			require.Falsef(t, strings.HasPrefix(imported, "github.com/jackc"), "%s imports pgx: no SQL in use cases", file.path)
		}
	}
}

func TestModulesNeverImportEachOther(t *testing.T) {
	for _, file := range sources(t) {
		module, ok := moduleOf(file.path)
		if !ok {
			continue
		}
		for _, imported := range file.imports {
			internal, isInternal := internalPath(imported)
			if !isInternal || !strings.HasPrefix(internal, "internal/modules/") {
				continue
			}
			other, _, _ := strings.Cut(strings.TrimPrefix(internal, "internal/modules/"), "/")
			require.Equalf(t, module, other, "%s imports module %q: modules talk through interfaces wired in main, never by importing", file.path, other)
		}
	}
}

func TestPlatformKnowsNothingOfModules(t *testing.T) {
	for _, file := range sources(t) {
		if !strings.HasPrefix(file.path, "internal/platform/") && !strings.HasPrefix(file.path, "internal/integrations/") {
			continue
		}
		for _, imported := range file.imports {
			require.NotContainsf(t, imported, "/internal/modules/", "%s imports a module: platform and integrations are below the modules", file.path)
		}
	}
}

func TestOnlyApierrorWritesJSON(t *testing.T) {
	for _, file := range sources(t) {
		if strings.HasPrefix(file.path, "internal/platform/apierror/") {
			continue
		}
		for _, call := range []string{"c.JSON(", "c.AbortWithStatusJSON(", "c.IndentedJSON("} {
			require.NotContainsf(t, file.source, call, "%s calls %s: answer through apierror.OK/Created/Respond", file.path, call)
		}
	}
}

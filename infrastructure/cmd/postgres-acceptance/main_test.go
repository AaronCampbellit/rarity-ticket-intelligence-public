package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunRejectsUnsafeDatabaseURLsBeforeUsingExternalBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "missing", wantErr: "TEST_DATABASE_URL is required"},
		{name: "wrong scheme", url: "mysql://postgres:postgres@127.0.0.1/rarity_test", wantErr: "PostgreSQL URL"},
		{name: "remote host", url: "postgres://postgres:postgres@database.example/rarity_test", wantErr: "loopback host"},
		{name: "remote query host", url: "postgres://postgres:postgres@127.0.0.1/rarity_test?host=database.example", wantErr: "loopback host"},
		{name: "wrong database", url: "postgres://postgres:postgres@127.0.0.1/production", wantErr: "rarity_test database"},
		{name: "query database override", url: "postgres://postgres:postgres@127.0.0.1/rarity_test?dbname=production", wantErr: "rarity_test database"},
		{name: "query database pins derived URLs", url: "postgres://postgres:postgres@127.0.0.1/rarity_test?dbname=rarity_test", wantErr: "must not override database selection"},
		{name: "missing host", url: "postgres:///rarity_test", wantErr: "loopback host"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			commands := &recordingCommands{}
			connectorCalled := false
			var stderr strings.Builder
			exitCode := run(context.Background(), runConfig{
				patterns: []string{"./backend/...", "./tests/..."},
				environ:  environmentWithDatabaseURL(test.url),
				stdout:   io.Discard,
				stderr:   &stderr,
				commands: commands,
				connect: func(context.Context, string) (maintenanceDatabase, error) {
					connectorCalled = true
					return &recordingDatabase{}, nil
				},
			})

			if exitCode == 0 {
				t.Fatal("exit code = 0, want nonzero")
			}
			if !strings.Contains(stderr.String(), test.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), test.wantErr)
			}
			if connectorCalled {
				t.Fatal("database connector was called for an unsafe URL")
			}
			if len(commands.events) != 0 {
				t.Fatalf("command events = %#v, want none", commands.events)
			}
		})
	}
}

func TestAcceptanceURLsPreserveConnectionDetailsAndReplaceOnlyDatabase(t *testing.T) {
	input := "postgresql://tester:secret@[::1]:55433/rarity_test?sslmode=disable&application_name=acceptance"

	urls, err := validateAndDeriveURLs(input)
	if err != nil {
		t.Fatalf("validate URL: %v", err)
	}
	if urls.maintenance != "postgresql://tester:secret@[::1]:55433/postgres?sslmode=disable&application_name=acceptance" {
		t.Fatalf("maintenance URL = %q", urls.maintenance)
	}
	if got := urls.forDatabase("rarity_test_acceptance_007"); got != "postgresql://tester:secret@[::1]:55433/rarity_test_acceptance_007?sslmode=disable&application_name=acceptance" {
		t.Fatalf("test URL = %q", got)
	}
}

func TestAcceptanceDatabaseNamesAreStable(t *testing.T) {
	tests := []struct {
		index int
		want  string
	}{
		{index: 1, want: "rarity_test_acceptance_001"},
		{index: 12, want: "rarity_test_acceptance_012"},
		{index: 999, want: "rarity_test_acceptance_999"},
	}
	for _, test := range tests {
		if got := acceptanceDatabaseName(test.index); got != test.want {
			t.Fatalf("acceptanceDatabaseName(%d) = %q, want %q", test.index, got, test.want)
		}
	}
}

func TestRunDiscoversSortedDatabaseBackedFileGroupsAfterPortablePackages(t *testing.T) {
	temporaryRoot := t.TempDir()
	alphaDirectory := filepath.Join(temporaryRoot, "alpha")
	zuluDirectory := filepath.Join(temporaryRoot, "zulu")
	for _, directory := range []string{alphaDirectory, zuluDirectory} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create temporary package directory: %v", err)
		}
	}
	writeTemporaryTestFile(t, filepath.Join(alphaDirectory, "z_database_test.go"), `package alpha
import (
	"os"
	"testing"
)
func TestZeta(t *testing.T) { _ = os.Getenv("TEST_DATABASE_URL") }
func ExampleAlpha() {
	// Output:
}
func FuzzBeta(f *testing.F) {}
type fixture struct{}
func (fixture) TestMethod(t *testing.T) {}
`)
	writeTemporaryTestFile(t, filepath.Join(alphaDirectory, "a_database_test.go"), `package alpha_test
import (
	"os"
	"testing"
)
func TestExternal(t *testing.T) { _ = os.Getenv("TEST_DATABASE_URL") }
`)
	writeTemporaryTestFile(t, filepath.Join(alphaDirectory, "portable_test.go"), `package alpha
import (
	"os"
	"testing"
)
func TestPortable(t *testing.T) { _ = os.Getenv("TEST_DATABASE_UR") }
`)
	writeTemporaryTestFile(t, filepath.Join(zuluDirectory, "database_test.go"), `package zulu
import (
	"os"
	"testing"
)
func TestOnly(t *testing.T) { _ = os.Getenv("TEST_DATABASE_URL") }
`)

	commands := &recordingCommands{listOutput: strings.Join([]string{
		fmt.Sprintf(`{"Dir":%q,"ImportPath":"example/zulu","TestGoFiles":["database_test.go"]}`, zuluDirectory),
		fmt.Sprintf(`{"Dir":%q,"ImportPath":"example/alpha","TestGoFiles":["z_database_test.go","portable_test.go"],"XTestGoFiles":["a_database_test.go"]}`, alphaDirectory),
	}, "\n")}
	database := &recordingDatabase{events: &commands.events}
	var stderr strings.Builder
	exitCode := run(context.Background(), runConfig{
		patterns: []string{"./backend/...", "./tests/..."},
		environ: []string{
			"TEST_DATABASE_URL=postgres://postgres@127.0.0.1/rarity_test",
			"KEEP=value",
			"TEST_DATABASE_URL=postgres://duplicate.invalid/production",
		},
		stdout:   io.Discard,
		stderr:   &stderr,
		commands: commands,
		connect: func(_ context.Context, databaseURL string) (maintenanceDatabase, error) {
			commands.events = append(commands.events, "connect "+databaseURL)
			return database, nil
		},
	})

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	wantArguments := [][]string{
		{"example/alpha", "-count=1"},
		{"example/zulu", "-count=1"},
		{"example/alpha", "-run", "^(TestExternal)$", "-count=1"},
		{"example/alpha", "-run", "^(ExampleAlpha|FuzzBeta|TestZeta)$", "-count=1"},
		{"example/zulu", "-run", "^(TestOnly)$", "-count=1"},
	}
	if !reflect.DeepEqual(commands.testArguments, wantArguments) {
		t.Fatalf("test arguments = %#v, want %#v", commands.testArguments, wantArguments)
	}
	for index, environment := range commands.testEnvironments[:2] {
		if got := countEnvironmentEntries(environment, databaseURLVariable); got != 0 {
			t.Fatalf("portable environment %d contains %d %s entries, want 0: %#v", index, got, databaseURLVariable, environment)
		}
		if !reflect.DeepEqual(environment, []string{"KEEP=value"}) {
			t.Fatalf("portable environment %d = %#v, want only inherited non-database variables", index, environment)
		}
	}
	for index, environment := range commands.testEnvironments[2:] {
		if got := countEnvironmentEntries(environment, databaseURLVariable); got != 1 {
			t.Fatalf("database environment %d contains %d %s entries, want 1: %#v", index, got, databaseURLVariable, environment)
		}
	}
	wantEvents := []string{
		"list -json ./backend/... ./tests/...",
		"test example/alpha ",
		"test example/zulu ",
		"connect postgres://postgres@127.0.0.1/postgres",
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_001" WITH (FORCE)`,
		`exec CREATE DATABASE "rarity_test_acceptance_001"`,
		"test example/alpha postgres://postgres@127.0.0.1/rarity_test_acceptance_001",
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_001" WITH (FORCE)`,
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_002" WITH (FORCE)`,
		`exec CREATE DATABASE "rarity_test_acceptance_002"`,
		"test example/alpha postgres://postgres@127.0.0.1/rarity_test_acceptance_002",
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_002" WITH (FORCE)`,
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_003" WITH (FORCE)`,
		`exec CREATE DATABASE "rarity_test_acceptance_003"`,
		"test example/zulu postgres://postgres@127.0.0.1/rarity_test_acceptance_003",
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_003" WITH (FORCE)`,
		"close",
	}
	if !reflect.DeepEqual(commands.events, wantEvents) {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(commands.events, "\n"), strings.Join(wantEvents, "\n"))
	}
}

func TestRunFailsClosedWhenMarkedFileHasNoRunnableTopLevelFunction(t *testing.T) {
	directory := t.TempDir()
	writeTemporaryTestFile(t, filepath.Join(directory, "marked_test.go"), `package marked
import (
	"os"
	"testing"
)
type fixture struct{}
func (fixture) TestMethod(t *testing.T) { _ = os.Getenv("TEST_DATABASE_URL") }
func TestWrongArgument(f *testing.F) {}
func FuzzWrongArgument(t *testing.T) {}
func ExampleNoOutput() {}
func ExampleGeneric[T any]() {
	// Output: generic
}
func TestMain(m *testing.M) {}
`)
	commands := &recordingCommands{listOutput: fmt.Sprintf(
		`{"Dir":%q,"ImportPath":"example/marked","TestGoFiles":["marked_test.go"]}`,
		directory,
	)}
	connectorCalled := false
	var stderr strings.Builder
	exitCode := run(context.Background(), runConfig{
		patterns: []string{"./backend/..."},
		environ:  environmentWithDatabaseURL("postgres://postgres@localhost/rarity_test"),
		stdout:   io.Discard,
		stderr:   &stderr,
		commands: commands,
		connect: func(context.Context, string) (maintenanceDatabase, error) {
			connectorCalled = true
			return &recordingDatabase{}, nil
		},
	})

	if exitCode == 0 {
		t.Fatal("exit code = 0, want nonzero")
	}
	if !strings.Contains(stderr.String(), "example/marked") || !strings.Contains(stderr.String(), "marked_test.go") || !strings.Contains(stderr.String(), "no runnable top-level Test, Example, or Fuzz function") {
		t.Fatalf("stderr = %q, want marked-file discovery failure", stderr.String())
	}
	if connectorCalled {
		t.Fatal("connected to PostgreSQL after marked-file discovery failed")
	}
	if len(commands.testArguments) != 0 {
		t.Fatalf("test invocations = %#v, want none", commands.testArguments)
	}
}

func TestRunFailsClosedWhenListedTestFileCannotBeReadOrMarkedFileCannotBeParsed(t *testing.T) {
	tests := []struct {
		name       string
		fileName   string
		writeFile  bool
		fileSource string
		wantError  string
	}{
		{
			name:      "listed file cannot be read",
			fileName:  "missing_test.go",
			wantError: "read example/failing missing_test.go",
		},
		{
			name:       "marked file cannot be parsed",
			fileName:   "malformed_test.go",
			writeFile:  true,
			fileSource: "package failing\nconst key = `TEST_DATABASE_URL`\nfunc TestBroken(",
			wantError:  "parse example/failing malformed_test.go",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			if test.writeFile {
				writeTemporaryTestFile(t, filepath.Join(directory, test.fileName), test.fileSource)
			}
			commands := &recordingCommands{listOutput: fmt.Sprintf(
				`{"Dir":%q,"ImportPath":"example/failing","TestGoFiles":[%q]}`,
				directory,
				test.fileName,
			)}
			connectorCalled := false
			var stderr strings.Builder
			exitCode := run(context.Background(), runConfig{
				patterns: []string{"./backend/..."},
				environ:  environmentWithDatabaseURL("postgres://postgres@localhost/rarity_test"),
				stdout:   io.Discard,
				stderr:   &stderr,
				commands: commands,
				connect: func(context.Context, string) (maintenanceDatabase, error) {
					connectorCalled = true
					return &recordingDatabase{}, nil
				},
			})

			if exitCode == 0 {
				t.Fatal("exit code = 0, want nonzero")
			}
			if !strings.Contains(stderr.String(), test.wantError) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.wantError)
			}
			if connectorCalled || len(commands.testArguments) != 0 {
				t.Fatalf("used external boundary after discovery failure: connected=%v tests=%#v", connectorCalled, commands.testArguments)
			}
		})
	}
}

func TestExactTestRunPatternRegexpQuotesEverySelectedName(t *testing.T) {
	got := exactTestRunPattern([]string{"TestA+B", "Example(group)", "Fuzz.Case"})
	want := `^(TestA\+B|Example\(group\)|Fuzz\.Case)$`
	if got != want {
		t.Fatalf("exact test run pattern = %q, want %q", got, want)
	}
}

func TestTopLevelTestNamesIncludesBareRunnableGoTestPrefixes(t *testing.T) {
	names, err := topLevelTestNames("bare_test.go", []byte(`package bare
import "testing"
func Test(t *testing.T) {}
func Example() {
	// Output:
}
func Fuzz(f *testing.F) {}
func TestMain(m *testing.M) {}
`))
	if err != nil {
		t.Fatalf("parse top-level test names: %v", err)
	}
	want := []string{"Example", "Fuzz", "Test"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("top-level test names = %#v, want %#v", names, want)
	}
}

func TestTopLevelTestNamesMirrorsGoRunnableSignaturesAndExamples(t *testing.T) {
	names, err := topLevelTestNames("runnable_test.go", []byte(`package runnable
import (
	"fmt"
	"testing"
)
func TestValid(t *testing.T) {}
func TestUnnamed(*testing.T) {}
func TestWrongArgument(f *testing.F) {}
func TestResult(t *testing.T) error { return nil }
func TestTwoArguments(first, second *testing.T) {}
func TestGeneric[T any](t *testing.T) {}
func FuzzValid(f *testing.F) {}
func FuzzWrongArgument(t *testing.T) {}
func FuzzResult(f *testing.F) error { return nil }
func ExampleOutput() {
	fmt.Println("output")
	// Output: output
}
func ExampleEmptyOutput() {
	// Output:
}
func ExampleNoOutput() {}
func ExampleWithArgument(value int) {
	// Output:
}
func ExampleGeneric[T any]() {
	fmt.Println("generic")
	// Output: generic
}
`))
	if err != nil {
		t.Fatalf("parse top-level test names: %v", err)
	}
	want := []string{"ExampleEmptyOutput", "ExampleOutput", "FuzzValid", "TestUnnamed", "TestValid"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("top-level test names = %#v, want %#v", names, want)
	}
}

func TestTopLevelTestNamesTreatsOnlyTestingMTestMainAsNonSelectable(t *testing.T) {
	tests := []struct {
		name string
		decl string
		want []string
	}{
		{name: "testing M is harness", decl: "func TestMain(m *testing.M) {}"},
		{name: "testing T is selectable test", decl: "func TestMain(t *testing.T) {}", want: []string{"TestMain"}},
		{name: "wrong signature is not runnable", decl: "func TestMain(f *testing.F) {}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "package testmain\nimport \"testing\"\n" + test.decl + "\n"
			names, err := topLevelTestNames("testmain_test.go", []byte(source))
			if err != nil {
				t.Fatalf("parse top-level test names: %v", err)
			}
			if !reflect.DeepEqual(names, test.want) {
				t.Fatalf("top-level test names = %#v, want %#v", names, test.want)
			}
		})
	}
}

func TestRunStopsAfterPortableFailureWithoutConnectingToPostgres(t *testing.T) {
	commands := &recordingCommands{
		listOutput: markedPackageListOutput(t, "example/package"),
		testErrors: map[string]error{"example/package -count=1": processExitError(29)},
	}
	connectorCalled := false
	var stderr strings.Builder
	exitCode := run(context.Background(), runConfig{
		patterns: []string{"./backend/..."},
		environ: []string{
			"TEST_DATABASE_URL=postgres://postgres@localhost/rarity_test",
			"KEEP=value",
			"TEST_DATABASE_URL=postgres://duplicate.invalid/production",
		},
		stdout:   io.Discard,
		stderr:   &stderr,
		commands: commands,
		connect: func(context.Context, string) (maintenanceDatabase, error) {
			connectorCalled = true
			return &recordingDatabase{}, nil
		},
	})

	if exitCode != 29 {
		t.Fatalf("exit code = %d, want 29", exitCode)
	}
	if connectorCalled {
		t.Fatal("connected to PostgreSQL after portable failure")
	}
	if !reflect.DeepEqual(commands.events, []string{"list -json ./backend/...", "test example/package "}) {
		t.Fatalf("events = %#v, want list and portable test only", commands.events)
	}
	if got := countEnvironmentEntries(commands.testEnvironments[0], databaseURLVariable); got != 0 {
		t.Fatalf("portable failure environment contains %d database URL entries, want 0", got)
	}
	if !strings.Contains(stderr.String(), "portable package example/package") {
		t.Fatalf("stderr = %q, want labeled portable failure", stderr.String())
	}
}

func TestDiscoveryRecognizesOnlyExactStaticallyEvaluableOSEnvironmentLookups(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		marked bool
	}{
		{
			name:   "direct getenv literal",
			body:   `_ = os.Getenv("TEST_DATABASE_URL")`,
			marked: true,
		},
		{
			name: "constructed local constant lookup",
			body: `const prefix = "TEST_"
	const suffix = "DATABASE_URL"
	const key = prefix + suffix
	_, _ = os.LookupEnv(key)`,
			marked: true,
		},
		{
			name:   "parenthesized constant expression",
			body:   `_ = os.Getenv(("TEST_" + "DATABASE_") + "URL")`,
			marked: true,
		},
		{
			name:   "comment only",
			body:   `// TEST_DATABASE_URL`,
			marked: false,
		},
		{
			name:   "skip message only",
			body:   `t.Skip("TEST_DATABASE_URL is required")`,
			marked: false,
		},
		{
			name:   "unused exact constant",
			body:   `const key = "TEST_DATABASE_URL"`,
			marked: false,
		},
		{
			name:   "suffix lookup",
			body:   `_ = os.Getenv("TEST_DATABASE_URL_SUFFIX")`,
			marked: false,
		},
		{
			name:   "prefix lookup",
			body:   `_ = os.Getenv("PREFIX_TEST_DATABASE_URL")`,
			marked: false,
		},
		{
			name:   "setenv is not consumption",
			body:   `_ = os.Setenv("TEST_DATABASE_URL", "ignored")`,
			marked: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			source := "package marker\nimport (\"os\"; \"testing\")\nfunc TestMarker(t *testing.T) {\n" + test.body + "\n}\n"
			writeTemporaryTestFile(t, filepath.Join(directory, "marker_test.go"), source)
			output := []byte(fmt.Sprintf(
				`{"Dir":%q,"ImportPath":"example/marker","TestGoFiles":["marker_test.go"]}`,
				directory,
			))
			_, groups, err := discoverDatabaseTestFileGroups(output)
			if err != nil {
				t.Fatalf("discover database test groups: %v", err)
			}
			if got := len(groups); (got == 1) != test.marked {
				t.Fatalf("discovered groups = %d, marked=%v", got, test.marked)
			}
		})
	}
}

func TestDiscoveryRecognizesAliasedAndDotImportedOSLookups(t *testing.T) {
	tests := []struct {
		name       string
		importLine string
		body       string
	}{
		{
			name:       "aliased os",
			importLine: `env "os"`,
			body:       `const key = "TEST_" + "DATABASE_URL"; _ = env.Getenv(key)`,
		},
		{
			name:       "dot imported os",
			importLine: `. "os"`,
			body:       `_ = Getenv("TEST_DATABASE_URL")`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			source := "package marker\nimport (" + test.importLine + "; \"testing\")\nfunc TestMarker(t *testing.T) { " + test.body + " }\n"
			writeTemporaryTestFile(t, filepath.Join(directory, "marker_test.go"), source)
			output := []byte(fmt.Sprintf(
				`{"Dir":%q,"ImportPath":"example/marker","TestGoFiles":["marker_test.go"]}`,
				directory,
			))
			_, groups, err := discoverDatabaseTestFileGroups(output)
			if err != nil {
				t.Fatalf("discover database test groups: %v", err)
			}
			if len(groups) != 1 {
				t.Fatalf("discovered groups = %d, want 1", len(groups))
			}
		})
	}
}

func TestDiscoveryDoesNotTreatShadowedOSIdentifierAsEnvironmentLookup(t *testing.T) {
	directory := t.TempDir()
	writeTemporaryTestFile(t, filepath.Join(directory, "marker_test.go"), `package marker
import "testing"
type fakeOS struct{}
func (fakeOS) Getenv(string) string { return "" }
func TestMarker(t *testing.T) {
	os := fakeOS{}
	_ = os.Getenv("TEST_DATABASE_URL")
}
`)
	output := []byte(fmt.Sprintf(
		`{"Dir":%q,"ImportPath":"example/marker","TestGoFiles":["marker_test.go"]}`,
		directory,
	))
	_, groups, err := discoverDatabaseTestFileGroups(output)
	if err != nil {
		t.Fatalf("discover database test groups: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("discovered groups = %d, want 0", len(groups))
	}
}

func TestRunFailsClosedOnSelectableNameCollisionAcrossTestPackageVariants(t *testing.T) {
	directory := t.TempDir()
	writeTemporaryTestFile(t, filepath.Join(directory, "internal_test.go"), `package collision
import (
	"os"
	"testing"
)
func TestShared(t *testing.T) { _ = os.Getenv("TEST_DATABASE_URL") }
`)
	writeTemporaryTestFile(t, filepath.Join(directory, "external_test.go"), `package collision_test
import "testing"
func TestShared(t *testing.T) {}
`)
	commands := &recordingCommands{listOutput: fmt.Sprintf(
		`{"Dir":%q,"ImportPath":"example/collision","TestGoFiles":["internal_test.go"],"XTestGoFiles":["external_test.go"]}`,
		directory,
	)}
	connectorCalled := false
	var stderr strings.Builder
	exitCode := run(context.Background(), runConfig{
		patterns: []string{"./backend/..."},
		environ:  environmentWithDatabaseURL("postgres://postgres@localhost/rarity_test"),
		stdout:   io.Discard,
		stderr:   &stderr,
		commands: commands,
		connect: func(context.Context, string) (maintenanceDatabase, error) {
			connectorCalled = true
			return &recordingDatabase{}, nil
		},
	})

	if exitCode == 0 {
		t.Fatal("exit code = 0, want nonzero")
	}
	for _, expected := range []string{"example/collision", "TestShared", "internal_test.go", "external_test.go", "selectable test name collision"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr = %q, want collision detail %q", stderr.String(), expected)
		}
	}
	if connectorCalled {
		t.Fatal("connected to PostgreSQL after selectable-name collision")
	}
	if len(commands.testArguments) != 0 {
		t.Fatalf("test invocations = %#v, want none after discovery collision", commands.testArguments)
	}
}

func TestDiscoveryIgnoresGenericExampleWhenCheckingSelectableNameCollisions(t *testing.T) {
	directory := t.TempDir()
	writeTemporaryTestFile(t, filepath.Join(directory, "internal_test.go"), `package collision
import "os"
func ExampleShared() {
	_ = os.Getenv("TEST_DATABASE_URL")
	// Output:
}
`)
	writeTemporaryTestFile(t, filepath.Join(directory, "external_test.go"), `package collision_test
func ExampleShared[T any]() {
	// Output: generic
}
`)
	output := []byte(fmt.Sprintf(
		`{"Dir":%q,"ImportPath":"example/collision","TestGoFiles":["internal_test.go"],"XTestGoFiles":["external_test.go"]}`,
		directory,
	))
	_, groups, err := discoverDatabaseTestFileGroups(output)
	if err != nil {
		t.Fatalf("discover database test groups: %v", err)
	}
	want := []databaseTestFileGroup{{
		packageName: "example/collision",
		fileName:    "internal_test.go",
		testNames:   []string{"ExampleShared"},
	}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("database test groups = %#v, want %#v", groups, want)
	}
}

func writeTemporaryTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temporary test file: %v", err)
	}
}

func countEnvironmentEntries(environment []string, name string) int {
	prefix := name + "="
	count := 0
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			count++
		}
	}
	return count
}

func markedPackageListOutput(t *testing.T, importPaths ...string) string {
	t.Helper()
	root := t.TempDir()
	objects := make([]string, 0, len(importPaths))
	for index, importPath := range importPaths {
		directory := filepath.Join(root, strconv.Itoa(index))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create marked package directory: %v", err)
		}
		writeTemporaryTestFile(t, filepath.Join(directory, "database_test.go"), `package fixture
import (
	"os"
	"testing"
)
func TestDatabase(t *testing.T) { _ = os.Getenv("TEST_DATABASE_URL") }
`)
		objects = append(objects, fmt.Sprintf(
			`{"Dir":%q,"ImportPath":%q,"TestGoFiles":["database_test.go"]}`,
			directory,
			importPath,
		))
	}
	return strings.Join(objects, "\n")
}

func TestRunCleansFailedFileGroupStopsAndPropagatesItsExitCode(t *testing.T) {
	commands := &recordingCommands{
		listOutput: markedPackageListOutput(t, "example/first", "example/second", "example/third"),
		testErrors: map[string]error{
			"example/second -run ^(TestDatabase)$ -count=1": processExitError(23),
		},
	}
	database := &recordingDatabase{events: &commands.events}
	var stderr strings.Builder
	exitCode := run(context.Background(), runConfig{
		patterns: []string{"./backend/...", "./tests/..."},
		environ:  environmentWithDatabaseURL("postgres://postgres@localhost/rarity_test"),
		stdout:   io.Discard,
		stderr:   &stderr,
		commands: commands,
		connect: func(context.Context, string) (maintenanceDatabase, error) {
			return database, nil
		},
	})

	if exitCode != 23 {
		t.Fatalf("exit code = %d, want 23", exitCode)
	}
	wantTail := []string{
		"test example/second postgres://postgres@localhost/rarity_test_acceptance_002",
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_002" WITH (FORCE)`,
		"close",
	}
	if got := commands.events[len(commands.events)-len(wantTail):]; !reflect.DeepEqual(got, wantTail) {
		t.Fatalf("event tail = %#v, want %#v", got, wantTail)
	}
	if !strings.Contains(stderr.String(), "example/second") || !strings.Contains(stderr.String(), "database_test.go") {
		t.Fatalf("stderr = %q, want package and source filename", stderr.String())
	}
	for _, event := range commands.events {
		if strings.Contains(event, "acceptance_003") {
			t.Fatalf("ran database work after first failing file group: %q", event)
		}
	}
}

func TestRunPreservesTestExitCodeWhenForcedCleanupAlsoFails(t *testing.T) {
	commands := &recordingCommands{
		listOutput: markedPackageListOutput(t, "example/failing"),
		testErrors: map[string]error{"example/failing -run ^(TestDatabase)$ -count=1": processExitError(17)},
	}
	database := &recordingDatabase{
		events: &commands.events,
		execErrors: map[int]error{
			3: errors.New("cleanup unavailable"),
		},
	}
	var stderr strings.Builder
	exitCode := run(context.Background(), runConfig{
		patterns: []string{"./backend/...", "./tests/..."},
		environ:  environmentWithDatabaseURL("postgres://postgres@127.0.0.1/rarity_test"),
		stdout:   io.Discard,
		stderr:   &stderr,
		commands: commands,
		connect: func(context.Context, string) (maintenanceDatabase, error) {
			return database, nil
		},
	})

	if exitCode != 17 {
		t.Fatalf("exit code = %d, want original test exit 17", exitCode)
	}
	if !strings.Contains(stderr.String(), "cleanup unavailable") {
		t.Fatalf("stderr = %q, want cleanup failure", stderr.String())
	}
}

func TestRunDropsDatabaseWhenCreateReturnsOutcomeAmbiguousError(t *testing.T) {
	commands := &recordingCommands{listOutput: markedPackageListOutput(t, "example/package")}
	database := &recordingDatabase{
		events: &commands.events,
		execErrors: map[int]error{
			2: errors.New("connection lost after create was sent"),
		},
	}
	var stderr strings.Builder
	exitCode := run(context.Background(), runConfig{
		patterns: []string{"./backend/...", "./tests/..."},
		environ:  environmentWithDatabaseURL("postgres://postgres@127.0.0.1/rarity_test"),
		stdout:   io.Discard,
		stderr:   &stderr,
		commands: commands,
		connect: func(context.Context, string) (maintenanceDatabase, error) {
			return database, nil
		},
	})

	if exitCode == 0 {
		t.Fatal("exit code = 0, want nonzero")
	}
	wantEvents := []string{
		"list -json ./backend/... ./tests/...",
		"test example/package ",
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_001" WITH (FORCE)`,
		`exec CREATE DATABASE "rarity_test_acceptance_001"`,
		`exec DROP DATABASE IF EXISTS "rarity_test_acceptance_001" WITH (FORCE)`,
		"close",
	}
	if !reflect.DeepEqual(commands.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", commands.events, wantEvents)
	}
	if len(commands.testArguments) != 1 {
		t.Fatalf("test invocations = %d, want only the portable pass", len(commands.testArguments))
	}
}

func TestRunUsesDetachedBoundedContextsForDropsAndMaintenanceClose(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	commands := &recordingCommands{
		listOutput: markedPackageListOutput(t, "example/package"),
		testErrors: map[string]error{"example/package -run ^(TestDatabase)$ -count=1": context.Canceled},
	}
	commands.testHook = func() {
		if len(commands.testArguments) == 2 {
			cancelParent()
		}
	}
	database := &recordingDatabase{events: &commands.events}
	exitCode := run(parent, runConfig{
		patterns: []string{"./backend/...", "./tests/..."},
		environ:  environmentWithDatabaseURL("postgres://postgres@127.0.0.1/rarity_test"),
		stdout:   io.Discard,
		stderr:   io.Discard,
		commands: commands,
		connect: func(context.Context, string) (maintenanceDatabase, error) {
			return database, nil
		},
	})

	if exitCode == 0 {
		t.Fatal("exit code = 0, want nonzero")
	}
	if len(database.execContexts) != 3 {
		t.Fatalf("exec contexts = %d, want 3", len(database.execContexts))
	}
	for _, index := range []int{0, 2} {
		assertDetachedBoundedContext(t, database.execContexts[index])
	}
	if len(database.closeContexts) != 1 {
		t.Fatalf("close contexts = %d, want 1", len(database.closeContexts))
	}
	assertDetachedBoundedContext(t, database.closeContexts[0])
}

func TestRunCommandInProcessGroupTerminatesDescendantBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pidPath := filepath.Join(t.TempDir(), "descendant.pid")
	command := exec.CommandContext(ctx, "sh", "-c", `sleep 30 & echo $! > "$1"; wait`, "process-group-test", pidPath)
	command.Stdout = io.Discard
	command.Stderr = io.Discard

	done := make(chan error, 1)
	go func() {
		done <- runCommandInProcessGroup(ctx, command)
	}()

	descendantPID := waitForPIDFile(t, pidPath)
	t.Cleanup(func() {
		if descendantPID > 0 {
			_ = syscall.Kill(descendantPID, syscall.SIGKILL)
		}
	})
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("process-group command returned nil after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process-group command did not return after cancellation")
	}
	if processIsRunning(descendantPID) {
		t.Fatalf("descendant process %d is still running after command returned", descendantPID)
	}
	descendantPID = 0
}

func TestRestoreDefaultSignalHandlingAfterFirstCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	restoreDefaultSignalHandlingAfterCancellation(ctx, func() {
		close(stopped)
	})

	select {
	case <-stopped:
		t.Fatal("signal handling restored before cancellation")
	default:
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("signal handling was not restored after cancellation")
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
			if parseErr != nil {
				t.Fatalf("parse descendant PID: %v", parseErr)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read descendant PID: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("descendant PID file was not created")
	return 0
}

func processIsRunning(pid int) bool {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	closingParenthesis := strings.LastIndexByte(string(content), ')')
	if closingParenthesis < 0 {
		return true
	}
	fields := strings.Fields(string(content)[closingParenthesis+1:])
	return len(fields) == 0 || fields[0] != "Z"
}

func assertDetachedBoundedContext(t *testing.T, observation contextObservation) {
	t.Helper()
	if observation.canceled {
		t.Fatal("cleanup context inherited parent cancellation")
	}
	if !observation.hasDeadline {
		t.Fatal("cleanup context has no deadline")
	}
	if observation.remaining <= 0 || observation.remaining > time.Minute {
		t.Fatalf("cleanup deadline remaining = %s, want within one minute", observation.remaining)
	}
}

func environmentWithDatabaseURL(url string) []string {
	if url == "" {
		return []string{"OTHER=value"}
	}
	return []string{"OTHER=value", "TEST_DATABASE_URL=" + url}
}

type recordingCommands struct {
	events           []string
	listOutput       string
	listError        error
	testErrors       map[string]error
	testArguments    [][]string
	testEnvironments [][]string
	testHook         func()
}

func (commands *recordingCommands) list(_ context.Context, patterns []string, _ io.Writer) ([]byte, error) {
	commands.events = append(commands.events, "list "+strings.Join(patterns, " "))
	return []byte(commands.listOutput), commands.listError
}

func (commands *recordingCommands) test(_ context.Context, arguments, environment []string, _ io.Writer, _ io.Writer) error {
	argumentsCopy := append([]string(nil), arguments...)
	environmentCopy := append([]string(nil), environment...)
	commands.testArguments = append(commands.testArguments, argumentsCopy)
	commands.testEnvironments = append(commands.testEnvironments, environmentCopy)
	commands.events = append(commands.events, "test "+arguments[0]+" "+databaseURLFromEnvironment(environment))
	if commands.testHook != nil {
		commands.testHook()
	}
	return commands.testErrors[strings.Join(arguments, " ")]
}

type contextObservation struct {
	canceled    bool
	hasDeadline bool
	remaining   time.Duration
}

type recordingDatabase struct {
	events        *[]string
	execErrors    map[int]error
	execCount     int
	execContexts  []contextObservation
	closeContexts []contextObservation
}

func (database *recordingDatabase) exec(ctx context.Context, statement string) error {
	database.execCount++
	database.execContexts = append(database.execContexts, observeContext(ctx))
	if database.events != nil {
		*database.events = append(*database.events, "exec "+statement)
	}
	return database.execErrors[database.execCount]
}

func (database *recordingDatabase) close(ctx context.Context) error {
	database.closeContexts = append(database.closeContexts, observeContext(ctx))
	if database.events != nil {
		*database.events = append(*database.events, "close")
	}
	return nil
}

func observeContext(ctx context.Context) contextObservation {
	deadline, hasDeadline := ctx.Deadline()
	remaining := time.Duration(0)
	if hasDeadline {
		remaining = time.Until(deadline)
	}
	return contextObservation{
		canceled:    ctx.Err() != nil,
		hasDeadline: hasDeadline,
		remaining:   remaining,
	}
}

type processExitError int

func (err processExitError) Error() string { return "process failed" }
func (err processExitError) ExitCode() int { return int(err) }

func databaseURLFromEnvironment(environment []string) string {
	for _, variable := range environment {
		if strings.HasPrefix(variable, "TEST_DATABASE_URL=") {
			return strings.TrimPrefix(variable, "TEST_DATABASE_URL=")
		}
	}
	return ""
}

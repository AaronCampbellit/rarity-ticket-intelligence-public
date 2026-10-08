package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	databaseURLVariable = "TEST_DATABASE_URL"
	cleanupTimeout      = 30 * time.Second
)

type commandRunner interface {
	list(context.Context, []string, io.Writer) ([]byte, error)
	test(context.Context, []string, []string, io.Writer, io.Writer) error
}

type maintenanceDatabase interface {
	exec(context.Context, string) error
	close(context.Context) error
}

type databaseConnector func(context.Context, string) (maintenanceDatabase, error)

type runConfig struct {
	patterns []string
	environ  []string
	stdout   io.Writer
	stderr   io.Writer
	commands commandRunner
	connect  databaseConnector
}

type acceptanceURLs struct {
	parsed      *url.URL
	maintenance string
}

type listedPackage struct {
	Dir          string
	ImportPath   string
	TestGoFiles  []string
	XTestGoFiles []string
}

type databaseTestFileGroup struct {
	packageName string
	fileName    string
	testNames   []string
}

type testFileAnalysis struct {
	usesDatabaseURL bool
	testNames       []string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	restoreDefaultSignalHandlingAfterCancellation(ctx, stop)

	exitCode := run(ctx, runConfig{
		patterns: os.Args[1:],
		environ:  os.Environ(),
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		commands: operatingSystemCommands{},
		connect:  connectPostgres,
	})
	stop()
	os.Exit(exitCode)
}

func restoreDefaultSignalHandlingAfterCancellation(ctx context.Context, stop func()) {
	go func() {
		<-ctx.Done()
		stop()
	}()
}

func run(ctx context.Context, config runConfig) (exitCode int) {
	databaseURL := lookupEnvironment(config.environ, databaseURLVariable)
	urls, err := validateAndDeriveURLs(databaseURL)
	if err != nil {
		fmt.Fprintf(config.stderr, "postgres acceptance: %v\n", err)
		return 1
	}

	listArguments := append([]string{"-json"}, config.patterns...)
	packageOutput, err := config.commands.list(ctx, listArguments, config.stderr)
	if err != nil {
		fmt.Fprintf(config.stderr, "postgres acceptance: list packages: %v\n", err)
		return exitCodeFor(err)
	}
	packages, groups, err := discoverDatabaseTestFileGroups(packageOutput)
	if err != nil {
		fmt.Fprintf(config.stderr, "postgres acceptance: discover database-backed tests: %v\n", err)
		return 1
	}
	if len(packages) == 0 {
		fmt.Fprintln(config.stderr, "postgres acceptance: go list returned no packages")
		return 1
	}
	fmt.Fprintf(config.stdout, "postgres acceptance: discovered %d database-backed test-file groups\n", len(groups))

	portableEnvironment := removeEnvironment(config.environ, databaseURLVariable)
	for _, listedPackage := range packages {
		testErr := config.commands.test(
			ctx,
			[]string{listedPackage.ImportPath, "-count=1"},
			portableEnvironment,
			config.stdout,
			config.stderr,
		)
		if testErr != nil {
			fmt.Fprintf(config.stderr, "postgres acceptance: test portable package %s: %v\n", listedPackage.ImportPath, testErr)
			return exitCodeFor(testErr)
		}
	}
	if len(groups) == 0 {
		return 0
	}

	database, err := config.connect(ctx, urls.maintenance)
	if err != nil {
		fmt.Fprintf(config.stderr, "postgres acceptance: connect to maintenance database: %v\n", err)
		return 1
	}
	defer func() {
		cleanupCtx, cancelCleanup := detachedCleanupContext(ctx)
		defer cancelCleanup()
		if err := database.close(cleanupCtx); err != nil {
			fmt.Fprintf(config.stderr, "postgres acceptance: close maintenance database: %v\n", err)
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}()

	for index, group := range groups {
		databaseName := acceptanceDatabaseName(index + 1)
		dropStatement := fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, quoteIdentifier(databaseName))
		staleDropCtx, cancelStaleDrop := detachedCleanupContext(ctx)
		err := database.exec(staleDropCtx, dropStatement)
		cancelStaleDrop()
		if err != nil {
			fmt.Fprintf(config.stderr, "postgres acceptance: remove stale database %s: %v\n", databaseName, err)
			return 1
		}
		createStatement := fmt.Sprintf(`CREATE DATABASE %s`, quoteIdentifier(databaseName))
		var testErr, cleanupErr error
		createErr := func() error {
			defer func() {
				cleanupCtx, cancelCleanup := detachedCleanupContext(ctx)
				defer cancelCleanup()
				cleanupErr = database.exec(cleanupCtx, dropStatement)
			}()

			if err := database.exec(ctx, createStatement); err != nil {
				return err
			}

			testEnvironment := replaceEnvironment(
				config.environ,
				databaseURLVariable,
				urls.forDatabase(databaseName),
			)
			testErr = config.commands.test(
				ctx,
				[]string{group.packageName, "-run", exactTestRunPattern(group.testNames), "-count=1"},
				testEnvironment,
				config.stdout,
				config.stderr,
			)
			return nil
		}()

		if cleanupErr != nil {
			fmt.Fprintf(config.stderr, "postgres acceptance: drop database %s: %v\n", databaseName, cleanupErr)
		}
		if createErr != nil {
			fmt.Fprintf(config.stderr, "postgres acceptance: create database %s: %v\n", databaseName, createErr)
			return 1
		}
		if testErr != nil {
			fmt.Fprintf(config.stderr, "postgres acceptance: test database-backed file %s %s: %v\n", group.packageName, group.fileName, testErr)
			return exitCodeFor(testErr)
		}
		if cleanupErr != nil {
			return 1
		}
	}

	return 0
}

func discoverDatabaseTestFileGroups(output []byte) ([]listedPackage, []databaseTestFileGroup, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	var packages []listedPackage
	for {
		var listed listedPackage
		if err := decoder.Decode(&listed); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("decode go list JSON: %w", err)
		}
		if listed.ImportPath == "" {
			return nil, nil, errors.New("go list package has no import path")
		}
		if listed.Dir == "" {
			return nil, nil, fmt.Errorf("package %s has no directory", listed.ImportPath)
		}
		packages = append(packages, listed)
	}
	sort.Slice(packages, func(left, right int) bool {
		return packages[left].ImportPath < packages[right].ImportPath
	})

	var groups []databaseTestFileGroup
	for _, listed := range packages {
		files := append([]string(nil), listed.TestGoFiles...)
		files = append(files, listed.XTestGoFiles...)
		sort.Strings(files)
		selectableNameFiles := make(map[string]string)
		for _, fileName := range files {
			path := filepath.Join(listed.Dir, fileName)
			source, err := os.ReadFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("read %s %s: %w", listed.ImportPath, fileName, err)
			}
			analysis, err := analyzeTestFile(path, source)
			if err != nil {
				return nil, nil, fmt.Errorf("parse %s %s: %w", listed.ImportPath, fileName, err)
			}
			for _, testName := range analysis.testNames {
				if previousFile, exists := selectableNameFiles[testName]; exists && previousFile != fileName {
					return nil, nil, fmt.Errorf("%s selectable test name collision %s in %s and %s", listed.ImportPath, testName, previousFile, fileName)
				}
				selectableNameFiles[testName] = fileName
			}
			if !analysis.usesDatabaseURL {
				continue
			}
			if len(analysis.testNames) == 0 {
				return nil, nil, fmt.Errorf("%s %s uses %s but has no runnable top-level Test, Example, or Fuzz function", listed.ImportPath, fileName, databaseURLVariable)
			}
			groups = append(groups, databaseTestFileGroup{
				packageName: listed.ImportPath,
				fileName:    fileName,
				testNames:   analysis.testNames,
			})
		}
	}
	return packages, groups, nil
}

func analyzeTestFile(path string, source []byte) (testFileAnalysis, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments)
	if err != nil {
		return testFileAnalysis{}, err
	}
	return testFileAnalysis{
		usesDatabaseURL: usesDatabaseURL(parsed),
		testNames:       topLevelTestNamesFromSyntax(parsed),
	}, nil
}

func topLevelTestNames(path string, source []byte) ([]string, error) {
	analysis, err := analyzeTestFile(path, source)
	if err != nil {
		return nil, err
	}
	return analysis.testNames, nil
}

func topLevelTestNamesFromSyntax(parsed *ast.File) []string {
	var names []string
	genericFunctions := make(map[string]bool)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil {
			continue
		}
		name := function.Name.Name
		if function.Type.TypeParams != nil && len(function.Type.TypeParams.List) > 0 {
			genericFunctions[name] = true
		}
		switch {
		case name == "TestMain":
			if isGoTestFunction(function, "T") {
				names = append(names, name)
			}
		case isGoTestName(name, "Test") && isGoTestFunction(function, "T"):
			names = append(names, name)
		case isGoTestName(name, "Fuzz") && isGoTestFunction(function, "F"):
			names = append(names, name)
		}
	}
	for _, example := range doc.Examples(parsed) {
		name := "Example" + example.Name
		if !genericFunctions[name] && (example.Output != "" || example.EmptyOutput) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func usesDatabaseURL(parsed *ast.File) bool {
	osNames, dotImported := osImportNames(parsed)
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !isOSEnvironmentLookup(call.Fun, osNames, dotImported) {
			return true
		}
		value, ok := evaluateStringConstant(call.Args[0], make(map[*ast.Object]bool))
		if ok && value == databaseURLVariable {
			found = true
			return false
		}
		return true
	})
	return found
}

func osImportNames(parsed *ast.File) (map[string]bool, bool) {
	names := make(map[string]bool)
	dotImported := false
	for _, imported := range parsed.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || path != "os" {
			continue
		}
		if imported.Name == nil {
			names["os"] = true
			continue
		}
		switch imported.Name.Name {
		case ".":
			dotImported = true
		case "_":
		default:
			names[imported.Name.Name] = true
		}
	}
	return names, dotImported
}

func isOSEnvironmentLookup(function ast.Expr, osNames map[string]bool, dotImported bool) bool {
	lookupName := func(name string) bool {
		return name == "Getenv" || name == "LookupEnv"
	}
	switch function := function.(type) {
	case *ast.SelectorExpr:
		packageName, ok := function.X.(*ast.Ident)
		return ok && osNames[packageName.Name] &&
			(packageName.Obj == nil || packageName.Obj.Kind == ast.Pkg) &&
			lookupName(function.Sel.Name)
	case *ast.Ident:
		return dotImported && function.Obj == nil && lookupName(function.Name)
	default:
		return false
	}
}

// evaluateStringConstant intentionally supports only string literals,
// parentheses, concatenation, and identifiers bound to explicit const value
// expressions in the same file. It does not guess through variables, helper
// calls, imported constants, or omitted expressions in const groups.
func evaluateStringConstant(expression ast.Expr, visiting map[*ast.Object]bool) (string, bool) {
	switch expression := expression.(type) {
	case *ast.BasicLit:
		if expression.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(expression.Value)
		return value, err == nil
	case *ast.ParenExpr:
		return evaluateStringConstant(expression.X, visiting)
	case *ast.BinaryExpr:
		if expression.Op != token.ADD {
			return "", false
		}
		left, leftOK := evaluateStringConstant(expression.X, visiting)
		right, rightOK := evaluateStringConstant(expression.Y, visiting)
		return left + right, leftOK && rightOK
	case *ast.Ident:
		object := expression.Obj
		if object == nil || object.Kind != ast.Con || visiting[object] {
			return "", false
		}
		valueSpec, ok := object.Decl.(*ast.ValueSpec)
		if !ok || len(valueSpec.Values) == 0 {
			return "", false
		}
		valueIndex := -1
		for index, name := range valueSpec.Names {
			if name.Obj == object {
				valueIndex = index
				break
			}
		}
		if valueIndex < 0 || valueIndex >= len(valueSpec.Values) {
			return "", false
		}
		visiting[object] = true
		value, ok := evaluateStringConstant(valueSpec.Values[valueIndex], visiting)
		delete(visiting, object)
		return value, ok
	default:
		return "", false
	}
}

func isGoTestFunction(function *ast.FuncDecl, argumentName string) bool {
	if function.Type.TypeParams != nil && len(function.Type.TypeParams.List) > 0 {
		return false
	}
	if function.Type.Results != nil && len(function.Type.Results.List) > 0 ||
		function.Type.Params == nil ||
		len(function.Type.Params.List) != 1 ||
		len(function.Type.Params.List[0].Names) > 1 {
		return false
	}
	pointer, ok := function.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch argument := pointer.X.(type) {
	case *ast.Ident:
		return argument.Name == argumentName
	case *ast.SelectorExpr:
		return argument.Sel.Name == argumentName
	default:
		return false
	}
}

func isGoTestName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	firstRune, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(firstRune)
}

func exactTestRunPattern(names []string) string {
	quoted := make([]string, len(names))
	for index, name := range names {
		quoted[index] = regexp.QuoteMeta(name)
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}

func detachedCleanupContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), cleanupTimeout)
}

func validateAndDeriveURLs(rawURL string) (acceptanceURLs, error) {
	if rawURL == "" {
		return acceptanceURLs{}, errors.New("TEST_DATABASE_URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return acceptanceURLs{}, fmt.Errorf("TEST_DATABASE_URL must be a valid PostgreSQL URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return acceptanceURLs{}, errors.New("TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	if !isLoopbackHost(parsed.Hostname()) {
		return acceptanceURLs{}, errors.New("TEST_DATABASE_URL must use a loopback host")
	}
	if parsed.Path != "/rarity_test" {
		return acceptanceURLs{}, errors.New("TEST_DATABASE_URL must select the rarity_test database")
	}
	connectionConfig, err := pgx.ParseConfig(rawURL)
	if err != nil {
		return acceptanceURLs{}, fmt.Errorf("TEST_DATABASE_URL must be a valid PostgreSQL URL: %w", err)
	}
	if !configUsesOnlyLoopbackHosts(connectionConfig) {
		return acceptanceURLs{}, errors.New("TEST_DATABASE_URL must use a loopback host")
	}
	if connectionConfig.Database != "rarity_test" {
		return acceptanceURLs{}, errors.New("TEST_DATABASE_URL must select the rarity_test database")
	}

	maintenanceURL := *parsed
	maintenanceURL.Path = "/postgres"
	maintenanceURL.RawPath = ""
	maintenanceConfig, err := pgx.ParseConfig(maintenanceURL.String())
	if err != nil || maintenanceConfig.Database != "postgres" {
		return acceptanceURLs{}, errors.New("TEST_DATABASE_URL must not override database selection")
	}
	return acceptanceURLs{parsed: parsed, maintenance: maintenanceURL.String()}, nil
}

func (urls acceptanceURLs) forDatabase(databaseName string) string {
	testURL := *urls.parsed
	testURL.Path = "/" + databaseName
	testURL.RawPath = ""
	return testURL.String()
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func configUsesOnlyLoopbackHosts(config *pgx.ConnConfig) bool {
	if !isLoopbackHost(config.Host) {
		return false
	}
	for _, fallback := range config.Fallbacks {
		if !isLoopbackHost(fallback.Host) {
			return false
		}
	}
	return true
}

func acceptanceDatabaseName(index int) string {
	return fmt.Sprintf("rarity_test_acceptance_%03d", index)
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func lookupEnvironment(environment []string, name string) string {
	prefix := name + "="
	for _, variable := range environment {
		if strings.HasPrefix(variable, prefix) {
			return strings.TrimPrefix(variable, prefix)
		}
	}
	return ""
}

func replaceEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	replaced := false
	result := make([]string, 0, len(environment)+1)
	for _, variable := range environment {
		if strings.HasPrefix(variable, prefix) {
			if !replaced {
				result = append(result, prefix+value)
				replaced = true
			}
			continue
		}
		result = append(result, variable)
	}
	if !replaced {
		result = append(result, prefix+value)
	}
	return result
}

func removeEnvironment(environment []string, name string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment))
	for _, variable := range environment {
		if !strings.HasPrefix(variable, prefix) {
			result = append(result, variable)
		}
	}
	return result
}

func exitCodeFor(err error) int {
	type exitCoder interface {
		ExitCode() int
	}
	var processError exitCoder
	if errors.As(err, &processError) && processError.ExitCode() > 0 {
		return processError.ExitCode()
	}
	return 1
}

type operatingSystemCommands struct{}

func (operatingSystemCommands) list(ctx context.Context, patterns []string, stderr io.Writer) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", append([]string{"list"}, patterns...)...)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = stderr
	if err := runCommandInProcessGroup(ctx, command); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (operatingSystemCommands) test(ctx context.Context, arguments, environment []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, "go", append([]string{"test"}, arguments...)...)
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	return runCommandInProcessGroup(ctx, command)
}

func runCommandInProcessGroup(ctx context.Context, command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}

	err := command.Run()
	if ctx.Err() != nil && command.Process != nil {
		waitForProcessGroup(command.Process.Pid)
	}
	return err
}

func waitForProcessGroup(processGroupID int) {
	for {
		err := syscall.Kill(-processGroupID, 0)
		if errors.Is(err, syscall.ESRCH) || err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type postgresMaintenanceDatabase struct {
	connection *pgx.Conn
}

func connectPostgres(ctx context.Context, databaseURL string) (maintenanceDatabase, error) {
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return postgresMaintenanceDatabase{connection: connection}, nil
}

func (database postgresMaintenanceDatabase) exec(ctx context.Context, statement string) error {
	_, err := database.connection.Exec(ctx, statement)
	return err
}

func (database postgresMaintenanceDatabase) close(ctx context.Context) error {
	return database.connection.Close(ctx)
}

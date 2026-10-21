// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package localworkflow

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestPlannerComposerDiscoveryRequiresPropagatingTests(t *testing.T) {
	// A test-named script must not grant implementation evidence merely because
	// it exists. Composer arrays propagate failure between their entries, unlike
	// shell semicolons within a single entry.
	for _, tt := range []struct {
		name    string
		script  any
		aliases map[string]any
		test    bool
	}{
		{name: "placeholder", script: "echo TODO"},
		{name: "noop", script: "true"},
		{name: "quoted test text", script: "echo 'vendor/bin/phpunit'"},
		{name: "noop array", script: []string{"echo TODO", ":"}},
		{name: "static", script: "vendor/bin/phpstan analyse"},
		{name: "static array", script: []string{"vendor/bin/phpstan analyse", "vendor/bin/psalm"}},
		{name: "empty string", script: ""},
		{name: "empty array", script: []string{}},
		{name: "null", script: nil},
		{name: "invalid body", script: map[string]string{"test": "vendor/bin/phpunit"}},
		{name: "invalid array entry", script: []any{"vendor/bin/phpunit", true}},
		{name: "unknown wrapper", script: "sh scripts/test.sh"},
		{name: "unknown before test", script: "sh scripts/prepare.sh && vendor/bin/phpunit"},
		{name: "PHP callback", script: `Project\Tests::run`},
		{name: "PHP callback array", script: []string{`Project\Tests::prepare`, "vendor/bin/phpunit"}},
		{name: "masked failure", script: "vendor/bin/phpunit || true"},
		{name: "semicolon masks failure", script: "vendor/bin/phpunit; true"},
		{name: "newline masks failure", script: "vendor/bin/pest\necho passed"},
		{name: "skipped test", script: "false && vendor/bin/phpunit; true"},
		{name: "masked entry in array", script: []string{"vendor/bin/phpunit; true", "echo passed"}},
		{name: "pipeline", script: "vendor/bin/phpunit | cat"},
		{name: "missing alias", script: "@test:unit"},
		{name: "noop alias", script: "@test:unit", aliases: map[string]any{"test:unit": "echo TODO"}},
		{name: "static alias", script: "@test:unit", aliases: map[string]any{"test:unit": "vendor/bin/phpstan analyse"}},
		{name: "cyclic alias", script: "@test:unit", aliases: map[string]any{"test:unit": "@test"}},
		{name: "alias with shell operators", script: "@test:unit && true", aliases: map[string]any{"test:unit": "vendor/bin/phpunit"}},
		{name: "PHP wrapper with shell operators", script: "@php vendor/bin/phpunit && true"},
		{name: "PHP wrapper with unrelated runner", script: "php go test ./..."},
		{name: "Composer PHP wrapper with unrelated runner", script: "@php vitest run"},
		{name: "foreign Python introspection", script: "pytest --version"},
		{name: "foreign JavaScript introspection", script: "vitest --version"},
		{name: "foreign Go introspection", script: "go test -list ."},
		{name: "foreign Python runner", script: "python3 -m pytest"},
		{name: "foreign JavaScript runner", script: "vitest run"},
		{name: "foreign Go runner", script: "go test ./..."},
		{name: "foreign alias", script: "@test:unit", aliases: map[string]any{"test:unit": "pytest --version"}},
		{name: "foreign array", script: []string{"echo running", "vitest --version"}},
		{name: "PHPUnit", script: "phpunit", test: true},
		{name: "vendor PHPUnit", script: "vendor/bin/phpunit --configuration phpunit.xml", test: true},
		{name: "relative Pest", script: "./vendor/bin/pest", test: true},
		{name: "PHP wrapper", script: "php vendor/bin/phpunit", test: true},
		{name: "Composer PHP wrapper", script: "@php vendor/bin/pest", test: true},
		{name: "shell AND list", script: "echo running && vendor/bin/phpunit && echo passed", test: true},
		{name: "final shell test", script: "echo running; vendor/bin/pest", test: true},
		{name: "test array", script: []string{"echo running", "vendor/bin/phpunit", "echo passed"}, test: true},
		{name: "static and test array", script: []string{"vendor/bin/phpstan analyse", "@php vendor/bin/phpunit"}, test: true},
		{name: "test alias", script: "@test:unit", aliases: map[string]any{"test:unit": "vendor/bin/phpunit"}, test: true},
		{name: "nested alias array", script: []string{"@test:unit", "echo passed"}, aliases: map[string]any{"test:unit": []string{"@unit", "@unit"}, "unit": "@php vendor/bin/pest"}, test: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			scripts := map[string]any{"test": tt.script}
			for name, script := range tt.aliases {
				scripts[name] = script
			}
			writePlannerManifest(t, root, "composer.json", map[string]any{"scripts": scripts})
			checks, err := discoverChecks(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(checks, verificationCandidate{"composer run test", "test"}); got != tt.test {
				t.Errorf("discovered Composer test evidence = %v, want %v: %+v", got, tt.test, checks)
			}
			_, err = configuredOnlyPlanner([]string{"composer run test"}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root})
			if (err == nil) != tt.test {
				t.Errorf("implementation plan accepted = %v, want %v (error: %v)", err == nil, tt.test, err)
			}
		})
	}
}

func TestPlannerComposerDiscoveryRespectsScopeAndConfiguredContract(t *testing.T) {
	for _, tt := range []struct {
		name       string
		script     string
		configured []string
	}{
		{name: "nested discovery", script: "vendor/bin/phpunit"},
		{name: "configured Composer wrapper", script: "echo TODO", configured: []string{"cd php-app && composer run test"}},
		{name: "configured Composer introspection", script: "vendor/bin/phpunit --version", configured: []string{"cd php-app && composer run test"}},
		{name: "configured foreign Composer wrapper", script: "pytest --version", configured: []string{"cd php-app && composer run test"}},
		{name: "configured custom wrapper", script: "echo TODO", configured: []string{"cd php-app && sh bin/test-local"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			project := filepath.Join(root, "php-app")
			if err := os.Mkdir(project, 0700); err != nil {
				t.Fatal(err)
			}
			writePlannerManifest(t, project, "composer.json", map[string]any{"scripts": map[string]string{"test": tt.script, "phpstan": "vendor/bin/phpstan analyse"}})
			selected := tt.configured
			if len(selected) == 0 {
				selected = []string{"cd php-app && composer run test"}
			}
			plan, err := configuredOnlyPlanner(selected).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root, TestCommands: tt.configured})
			if err != nil {
				t.Fatalf("real discovered test or explicitly configured runner rejected: %v", err)
			}
			if !slices.Contains(plan.Commands, "cd php-app && composer run phpstan") {
				t.Fatalf("Composer test lost static checks for the same scope: %+v", plan)
			}
		})
	}
}

func TestPlannerComposerRunnerIntrospectionDoesNotReplaceTests(t *testing.T) {
	for _, tt := range []struct {
		name    string
		script  any
		aliases map[string]any
		test    bool
	}{
		{name: "PHPUnit version", script: "vendor/bin/phpunit --version"},
		{name: "Pest version", script: "vendor/bin/pest --version"},
		{name: "PHPUnit help", script: "vendor/bin/phpunit --help"},
		{name: "Pest help", script: "vendor/bin/pest --help"},
		{name: "PHPUnit short help", script: "phpunit -h"},
		{name: "Pest short help", script: "pest -h"},
		{name: "PHPUnit tests", script: "phpunit --list-tests"},
		{name: "Pest tests", script: "pest --list-tests"},
		{name: "PHPUnit test files", script: "phpunit --list-test-files"},
		{name: "Pest test files", script: "pest --list-test-files"},
		{name: "PHPUnit groups", script: "phpunit --list-groups"},
		{name: "Pest groups", script: "pest --list-groups"},
		{name: "PHPUnit suites", script: "phpunit --list-suites"},
		{name: "Pest suites", script: "pest --list-suites"},
		{name: "PHPUnit XML tests", script: "phpunit --list-tests-xml tests.xml"},
		{name: "Pest XML tests", script: "pest --list-tests-xml=tests.xml"},
		{name: "PHPUnit test IDs", script: "phpunit --list-test-ids"},
		{name: "PHPUnit validate configuration", script: "phpunit --validate-configuration"},
		{name: "PHPUnit warm coverage cache", script: "phpunit --warm-coverage-cache"},
		{name: "PHPUnit migrate configuration", script: "phpunit --migrate-configuration"},
		{name: "PHPUnit generate configuration", script: "phpunit --generate-configuration"},
		{name: "PHPUnit check minimum version", script: "phpunit --atleast-version=12.0"},
		{name: "PHPUnit check version", script: "phpunit --check-version"},
		{name: "PHPUnit check PHP configuration", script: "phpunit --check-php-configuration"},
		{name: "Pest initialize", script: "pest --init"},
		{name: "Pest todo selection", script: "pest --todos", test: true},
		{name: "Pest notes selection", script: "pest --notes", test: true},
		{name: "Pest flaky selection", script: "pest --flaky", test: true},
		{name: "Pest issue selection", script: "pest --issue=123", test: true},
		{name: "Pest PR selection", script: "pest --pr=123", test: true},
		{name: "Pest pull request selection", script: "pest --pull-request=123", test: true},
		{name: "Composer PHP test IDs", script: "@php vendor/bin/phpunit --list-test-ids"},
		{name: "PHP configuration validation", script: "php vendor/bin/phpunit --validate-configuration"},
		{name: "alias initialization", script: "@test:unit", aliases: map[string]any{"test:unit": "@php vendor/bin/pest --init"}},
		{name: "configuration array", script: []string{"@php vendor/bin/phpunit --warm-coverage-cache", "echo passed"}},
		{name: "Composer PHP version", script: "@php vendor/bin/phpunit --version"},
		{name: "PHP help", script: "php vendor/bin/pest --help"},
		{name: "alias listing", script: "@test:unit", aliases: map[string]any{"test:unit": "@php vendor/bin/phpunit --list-groups"}},
		{name: "array listing", script: []string{"@php vendor/bin/pest --list-suites", "echo passed"}},
		{name: "alias array listing", script: []string{"@test:unit", "echo passed"}, aliases: map[string]any{"test:unit": []string{"phpunit --list-tests", "pest --version"}}},
		{name: "listing before real array test", script: []string{"phpunit --list-tests", "@php vendor/bin/phpunit"}, test: true},
		{name: "version after real array test", script: []string{"vendor/bin/phpunit", "pest --version"}, test: true},
		{name: "listing before real shell test", script: "phpunit --list-groups && phpunit", test: true},
		{name: "help after real shell test", script: "phpunit && pest --help", test: true},
		{name: "suite selection still tests", script: "@php vendor/bin/phpunit --testsuite unit", test: true},
		{name: "group selection still tests", script: "pest --group=unit", test: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			scripts := map[string]any{"test": tt.script}
			for name, script := range tt.aliases {
				scripts[name] = script
			}
			writePlannerManifest(t, root, "composer.json", map[string]any{"scripts": scripts})
			checks, err := discoverChecks(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(checks, verificationCandidate{"composer run test", "test"}); got != tt.test {
				t.Errorf("introspection granted test evidence = %v, want %v: %+v", got, tt.test, checks)
			}
			_, err = configuredOnlyPlanner([]string{"composer run test"}).Plan(t.Context(), domain.LocalWorkflowRun{WorkspacePath: root})
			if (err == nil) != tt.test {
				t.Errorf("implementation plan accepted = %v, want %v (error: %v)", err == nil, tt.test, err)
			}
		})
	}
}

package testplan

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Closure audit for US3/SC-004 (specs/047-dsh-chat-demo/tasks.md T024):
// the dsh dependency baseline third_party/dsh/core must be reusable and
// contain only the framework core, and every dsh package shipped in the
// agent artifact must be traceable to a declaration. The three assertions
// are defined in specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §4.
const (
	// agentPkgJSONRel and agentTarRel feed assertion ②: the expected set is
	// generated from the agent package.json direct dependencies plus their
	// recursive peer closure, checked against the shipped server_pkg tar.
	agentPkgJSONRel = "experimental/dsh/demo/agent/package.json"
	agentTarRel     = "experimental/dsh/demo/agent/server_pkg.tar"
	// coreBuildRel and corePkgJSONRel feed assertion ①: the two declaration
	// surfaces of the core baseline audited for plugin-free enumeration.
	coreBuildRel   = "third_party/dsh/core/BUILD.bazel"
	corePkgJSONRel = "third_party/dsh/core/package.json"

	// The tar layout produced by artifact_pkg_js (tools/release/defs.bzl):
	// everything ships under dominion/{app}/{service}, and node_modules is
	// the flattened pnpm store closure — one directory per package, package
	// manifests included (survey/deepseek-harness-b1-bazel-packaging.md §3.2).
	tarNodeModulesPrefix = "dominion/dsh-demo/agent/node_modules/"
	tarCordisYML         = "dominion/dsh-demo/agent/cordis.yml"

	// coreNPMDepsMarker locates the js_runtime_library npm_deps enumeration
	// inside the core BUILD file.
	coreNPMDepsMarker = "npm_deps = ["
	// linkTargetPrefix is the per-package link-target label form generated
	// by npm_link_all_packages (survey/deepseek-harness-b1-bazel-packaging.md
	// §3.3); the package name is the suffix.
	linkTargetPrefix = ":node_modules/"

	// nodeAddonPkg is the native-addon prerequisite of bare-name dsh
	// resolution — the one audited package outside the @deepseek-ai scope
	// (specs/047-dsh-chat-demo/research.md D6).
	nodeAddonPkg  = "node-addon-require-builtin"
	deepseekScope = "@deepseek-ai/"
)

// d6CorePackages is the framework-core baseline reference
// (specs/047-dsh-chat-demo/research.md D6): an @deepseek-ai/* package
// outside this list is a plugin, and plugins must not appear in the core
// baseline declarations.
var d6CorePackages = map[string]struct{}{
	"@deepseek-ai/cordis":                 {},
	"@deepseek-ai/cordis-plugin-group":    {},
	"@deepseek-ai/cordis-plugin-include":  {},
	"@deepseek-ai/cordis-plugin-loader":   {},
	"@deepseek-ai/cordis-plugin-timer":    {},
	"@deepseek-ai/dsh-app-boot":           {},
	"@deepseek-ai/dsh-home-paths":         {},
	"@deepseek-ai/dsh-invariants":         {},
	"@deepseek-ai/dsh-launch-environment": {},
	"@deepseek-ai/dsh-system-prompt":      {},
	"node-addon-require-builtin":          {},
}

// npmPackageMeta is the subset of a package.json manifest the audit parses:
// identity plus the three dependency edge kinds followed when expanding the
// declared closure.
type npmPackageMeta struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Dependencies     map[string]string `json:"dependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	OptionalDeps     map[string]string `json:"optionalDependencies"`
}

// agentTar is the audited content of the agent server_pkg tar.
type agentTar struct {
	// depEdges maps every top-level node_modules package to the union of its
	// dependency, peerDependency, and optionalDependency names — the edge set
	// the declared closure expands over.
	depEdges map[string]map[string]struct{}
	// versions maps package names to version → tar path of the package.json
	// the version was read from, across every node_modules depth, so version
	// conflicts carry provenance (survey/deepseek-harness-b1-bazel-packaging.md
	// §5.6.2). Collecting nested depths too is deliberate and conservative:
	// the uniqueness invariant exists to catch same-name version mixes before
	// the flatten step's silent last-write-wins overwrite decides what ships
	// (§5.6.2), so a multi-version occurrence at any depth — including a
	// pnpm-nested layout or one bundled inside a published package (§3.2) —
	// is reported rather than trusted. Workspace packages without a version
	// field record the empty version, which cannot conflict by itself.
	versions map[string]map[string]string
	// cordisRows holds the package names enabled by the shipped cordis.yml.
	cordisRows []string
}

// TestCoreBaselinePluginFree is audit assertion ①: the framework-core
// baseline third_party/dsh/core must stay plugin-free — every @deepseek-ai/*
// package it declares must be on the D6 core list, the D6 core list must be
// fully declared (an emptied or shrunk baseline must not pass trivially),
// and both declaration surfaces must enumerate the same set so neither can
// hide a declaration from the other
// (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §4-1).
func TestCoreBaselinePluginFree(t *testing.T) {
	// given: both declaration surfaces of the core baseline.
	buildDeps := coreBuildNPMDeps(t)
	pkgDeps := packageJSONDependencyNames(t, corePkgJSONRel)

	// when/then: no plugin on either surface — any @deepseek-ai/* package
	// off the D6 core list is one (the cordis family carries no dsh- prefix,
	// so scoping the check to the whole @deepseek-ai scope is both simpler
	// and stricter than matching dsh-* names only).
	for _, surface := range []map[string]struct{}{buildDeps, pkgDeps} {
		for _, name := range sortedNames(surface) {
			if !strings.HasPrefix(name, deepseekScope) {
				continue
			}
			if _, core := d6CorePackages[name]; !core {
				t.Errorf("plugin %s declared in the core baseline (not on the D6 core list, specs/047-dsh-chat-demo/research.md D6)", name)
			}
		}
	}

	// when/then: the full D6 core list is declared on both surfaces, so an
	// emptied declaration cannot pass the plugin checks above trivially.
	for _, core := range sortedNames(d6CorePackages) {
		if _, enumerated := buildDeps[core]; !enumerated {
			t.Errorf("D6 core package %s missing from BUILD npm_deps", core)
		}
		if _, declared := pkgDeps[core]; !declared {
			t.Errorf("D6 core package %s missing from core package.json dependencies", core)
		}
	}

	// when/then: both surfaces enumerate the same package set. Link targets
	// only exist for package.json dependencies
	// (survey/deepseek-harness-b1-bazel-packaging.md §3.3), so drift means one
	// surface is stale and the plugin audit above no longer covers the real
	// baseline.
	for _, name := range sortedNames(pkgDeps) {
		if _, enumerated := buildDeps[name]; !enumerated {
			t.Errorf("core package.json dependency %s missing from BUILD npm_deps", name)
		}
	}
	for _, name := range sortedNames(buildDeps) {
		if _, declared := pkgDeps[name]; !declared {
			t.Errorf("BUILD npm_deps entry %s missing from core package.json dependencies", name)
		}
	}
}

// TestTarDshClosureIsDeclared is audit assertion ②: every @deepseek-ai/*
// package and node-addon-require-builtin in the shipped agent tar must be
// traceable to a declaration — the closure of {core baseline dependencies ∪
// agent package.json dependencies} expanded over dependency/peer edges to a
// fixed point. The check is bidirectional: the tar must contain what the
// enabled cordis.yml rows and the declared dsh-family roots need (⊇), and
// nothing outside the declared closure (⊆). A ⊆ violation means the closure
// has a third source beyond the core baseline and the service declarations
// (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §4-2).
func TestTarDshClosureIsDeclared(t *testing.T) {
	// given: the declared roots and the shipped artifact's package edges.
	content := readAgentTar(t)
	rootSet := packageJSONDependencyNames(t, corePkgJSONRel)
	for name := range packageJSONDependencyNames(t, agentPkgJSONRel) {
		rootSet[name] = struct{}{}
	}
	roots := sortedNames(rootSet)
	closure := declaredClosure(roots, content.depEdges)

	audited := map[string]struct{}{}
	for name := range content.depEdges {
		if isDshFamily(name) {
			audited[name] = struct{}{}
		}
	}
	if len(audited) == 0 {
		t.Fatalf("no @deepseek-ai/* packages found in %s — the tar layout changed?", agentTarRel)
	}

	// then (⊇): the enabled composition rows and every declared dsh-family
	// root must be physically present — 启用行 ⊆ 物化 node_modules
	// (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §2).
	for _, row := range content.cordisRows {
		if _, present := audited[row]; !present {
			t.Errorf("cordis.yml row %s not materialized in the tar", row)
		}
	}
	for _, root := range roots {
		if !isDshFamily(root) {
			continue
		}
		if _, present := audited[root]; !present {
			t.Errorf("declared dsh root %s not materialized in the tar", root)
		}
	}

	// then (⊆): every audited package must be reachable from the declared
	// roots through materialized manifests.
	for _, name := range sortedNames(audited) {
		if _, reachable := closure[name]; !reachable {
			t.Errorf("tar package %s is outside the declared closure (third source: check agent/core npm_deps and runtime_deps)", name)
		}
	}
}

// TestTarPackageVersionsUnique is audit assertion ③: a package name may
// resolve to exactly one version across the whole materialized node_modules
// tree. The packaging flatten step (tools/release/defs.bzl Phase 3, cp -aL)
// lets a later copy silently win when two versions collide, so a name
// resolving to two versions means the closure mixed versions and which one
// ships is undefined (survey/deepseek-harness-b1-bazel-packaging.md §5.6.2).
func TestTarPackageVersionsUnique(t *testing.T) {
	// given: every package.json in the tar's node_modules tree.
	content := readAgentTar(t)

	// when/then: one version per package name.
	for _, name := range sortedNames(content.versions) {
		versions := content.versions[name]
		if len(versions) <= 1 {
			continue
		}
		var occurrences []string
		for _, version := range sortedNames(versions) {
			occurrences = append(occurrences, fmt.Sprintf("%s (%s)", version, versions[version]))
		}
		t.Errorf("package %s resolves to multiple versions: %s", name, strings.Join(occurrences, ", "))
	}
}

// readAgentTar streams the server_pkg tar and collects the audit inputs:
// dependency edges of every top-level node_modules package, package versions
// at any node_modules depth, and the enabled composition rows from the
// shipped cordis.yml. Only package.json and cordis.yml entries are parsed;
// the rest of the tree is skipped by header.
func readAgentTar(t *testing.T) *agentTar {
	t.Helper()

	f, err := os.Open(mustRunfile(t, agentTarRel))
	if err != nil {
		t.Fatalf("open %s: %v", agentTarRel, err)
	}
	defer f.Close()

	content := &agentTar{
		depEdges: map[string]map[string]struct{}{},
		versions: map[string]map[string]string{},
	}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read %s: %v", agentTarRel, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		switch {
		case hdr.Name == tarCordisYML:
			body, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read %s in %s: %v", hdr.Name, agentTarRel, err)
			}
			content.cordisRows = cordisRowNames(string(body))
		case strings.HasPrefix(hdr.Name, tarNodeModulesPrefix):
			rest := strings.TrimPrefix(hdr.Name, tarNodeModulesPrefix)
			if path.Base(rest) != "package.json" {
				continue
			}
			var meta npmPackageMeta
			if err := json.NewDecoder(tr).Decode(&meta); err != nil {
				t.Fatalf("decode %s in %s: %v", hdr.Name, agentTarRel, err)
			}
			// Name-less package.json files inside a package subtree (zod's
			// v4/, yaml's browser/, …) are subpath export-routing metadata,
			// not installable packages — they identify nothing to audit.
			if meta.Name == "" {
				continue
			}
			if content.versions[meta.Name] == nil {
				content.versions[meta.Name] = map[string]string{}
			}
			content.versions[meta.Name][meta.Version] = hdr.Name

			// Top-level packages — node_modules/<pkg> or
			// node_modules/@scope/<pkg> — define the materialized set and
			// the closure edges.
			pkgDir := path.Dir(rest)
			topLevel := !strings.Contains(pkgDir, "/") ||
				(strings.HasPrefix(pkgDir, "@") && strings.Count(pkgDir, "/") == 1)
			if !topLevel {
				continue
			}
			if content.depEdges[meta.Name] == nil {
				content.depEdges[meta.Name] = map[string]struct{}{}
			}
			for _, edges := range []map[string]string{meta.Dependencies, meta.PeerDependencies, meta.OptionalDeps} {
				for dep := range edges {
					content.depEdges[meta.Name][dep] = struct{}{}
				}
			}
		}
	}
	if len(content.depEdges) == 0 {
		t.Fatalf("no node_modules packages found in %s — the tar layout changed?", agentTarRel)
	}
	if len(content.cordisRows) == 0 {
		t.Fatalf("no composition rows found in %s within %s", tarCordisYML, agentTarRel)
	}
	return content
}

// cordisRowNames extracts the package names of the enabled composition rows
// from a cordis.yml manifest — a top-level list of maps whose row keys sit
// one indent level under the `- ` item markers
// (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §2). Within each
// item the shallowest `name:` key is the row package; deeper `name:` keys
// belong to row config and are ignored. Comment lines and trailing comments
// are tolerated, and a manifest whose structure yields no rows fails the
// caller's zero-row fail-loud check.
func cordisRowNames(manifest string) []string {
	itemMarker := regexp.MustCompile(`^-\s`)
	nameKey := regexp.MustCompile(`^(\s+)name:\s*'([^']+)'(?:\s*#.*)?$`)

	type candidate struct {
		indent int
		name   string
	}
	var (
		rows       []string
		candidates []candidate
	)
	shallowest := func() {
		if len(candidates) == 0 {
			return
		}
		row := candidates[0]
		for _, c := range candidates[1:] {
			if c.indent < row.indent {
				row = c
			}
		}
		rows = append(rows, row.name)
		candidates = nil
	}
	for _, line := range strings.Split(manifest, "\n") {
		if itemMarker.MatchString(line) {
			shallowest()
			continue
		}
		if m := nameKey.FindStringSubmatch(line); m != nil {
			candidates = append(candidates, candidate{indent: len(m[1]), name: m[2]})
		}
	}
	shallowest()
	return rows
}

// declaredClosure expands roots to a fixed point over the dependency edges
// of the materialized manifests, following only packages present in the tar
// (edges to absent packages cannot be walked; the presence direction of
// assertion ② is checked separately against the roots and cordis rows).
func declaredClosure(roots []string, depEdges map[string]map[string]struct{}) map[string]struct{} {
	closure := map[string]struct{}{}
	var queue []string
	for _, root := range roots {
		if _, present := depEdges[root]; present {
			queue = append(queue, root)
		}
	}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if _, reached := closure[pkg]; reached {
			continue
		}
		closure[pkg] = struct{}{}
		for dep := range depEdges[pkg] {
			if _, present := depEdges[dep]; present {
				queue = append(queue, dep)
			}
		}
	}
	return closure
}

// isDshFamily reports whether name is in the audited dsh closure scope:
// every @deepseek-ai/* package plus the native-addon prerequisite
// node-addon-require-builtin.
func isDshFamily(name string) bool {
	return strings.HasPrefix(name, deepseekScope) || name == nodeAddonPkg
}

// mustRunfile resolves a workspace-relative data dependency to its runfiles
// path. Bazel exposes a test's runfiles tree through the mandatory
// TEST_SRCDIR and TEST_WORKSPACE variables
// (https://bazel.build/reference/test-encyclopedia).
func mustRunfile(t *testing.T, rel string) string {
	t.Helper()
	srcDir := os.Getenv("TEST_SRCDIR")
	workspace := os.Getenv("TEST_WORKSPACE")
	if srcDir == "" || workspace == "" {
		t.Fatalf("missing bazel test env (TEST_SRCDIR=%q TEST_WORKSPACE=%q) — run via bazel test", srcDir, workspace)
	}
	resolved := filepath.Join(srcDir, workspace, rel)
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("locate runfile %s: %v", rel, err)
	}
	return resolved
}

// coreBuildNPMDeps returns the package names enumerated in the
// js_runtime_library runtime_pkg npm_deps attribute of the core BUILD file —
// the bazel-side baseline declaration audited by assertion ①.
func coreBuildNPMDeps(t *testing.T) map[string]struct{} {
	t.Helper()
	raw, err := os.ReadFile(mustRunfile(t, coreBuildRel))
	if err != nil {
		t.Fatalf("read %s: %v", coreBuildRel, err)
	}
	content := string(raw)

	// Exactly one npm_deps list may exist: a second js_runtime_library in
	// this BUILD would make "the" baseline enumeration ambiguous, and
	// silently auditing only the first list would leave the other unaudited.
	if got := strings.Count(content, coreNPMDepsMarker); got != 1 {
		t.Fatalf("%s: expected exactly one %q list, found %d — the audit needs a per-target way to locate the baseline enumeration", coreBuildRel, coreNPMDepsMarker, got)
	}
	start := strings.Index(content, coreNPMDepsMarker)
	block := content[start+len(coreNPMDepsMarker):]
	end := strings.Index(block, "]")
	if end < 0 {
		t.Fatalf("%s: unterminated npm_deps list", coreBuildRel)
	}

	deps := map[string]struct{}{}
	for _, field := range strings.FieldsFunc(block[:end], func(r rune) bool {
		return r == '"' || r == ',' || r == ' ' || r == '\n' || r == '\t'
	}) {
		name, ok := strings.CutPrefix(field, linkTargetPrefix)
		if !ok {
			t.Fatalf("%s: npm_deps entry %q is not a %s link target", coreBuildRel, field, linkTargetPrefix)
		}
		deps[name] = struct{}{}
	}
	return deps
}

// packageJSONDependencyNames returns the dependency names declared in the
// dependencies section of the package.json at rel — the pnpm-side
// declaration surface that link targets are generated from
// (survey/deepseek-harness-b1-bazel-packaging.md §3.3).
func packageJSONDependencyNames(t *testing.T, rel string) map[string]struct{} {
	t.Helper()
	raw, err := os.ReadFile(mustRunfile(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("decode %s: %v", rel, err)
	}
	deps := map[string]struct{}{}
	for name := range pkg.Dependencies {
		deps[name] = struct{}{}
	}
	return deps
}

// sortedNames returns the map keys in sorted order so audit failures list
// offenders deterministically.
func sortedNames[V any](m map[string]V) []string {
	var names []string
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

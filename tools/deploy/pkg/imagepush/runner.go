package imagepush

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"dominion/tools/deploy/pkg/workspace"
)

const (
	bazelOutputFilter = "--ui_event_filters=-info"
	bazelNoProgress   = "--noshow_progress"
	bazelBinary       = "bazel"
	bazelRun          = "run"
	bazelQuery        = "cquery"

	imageDirPrefix       = "readonly IMAGE_DIR=\"$(rlocation \""
	repositoryFilePrefix = "readonly REPOSITORY_FILE=\"$(rlocation \""
	fixedArgsPrefix      = "readonly FIXED_ARGS=("
	rlocationSuffix      = "\")\""
	pushScriptSuffix     = ".sh"
	runfilesManifestName = ".runfiles_manifest"
	runfilesDirName      = ".runfiles"
	imageIndexFileName   = "index.json"
)

var fixedArgPattern = regexp.MustCompile(`"([^"]*)"`)

// commandExecutor executes external commands for the production runner.
type commandExecutor interface {
	CombinedOutput(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// osCommandExecutor runs commands with os/exec.
type osCommandExecutor struct{}

func (osCommandExecutor) CombinedOutput(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// bazelRunner executes bazel push targets and parses the stable push contract.
type bazelRunner struct {
	workspaceRoot string
	exec          commandExecutor
}

// pushScriptMetadata captures the fields we read from the generated push script.
type pushScriptMetadata struct {
	imageRunfile      string
	repositoryRunfile string
	fixedArgs         []string
}

// NewRunner creates the production Runner backed by bazel.
func NewRunner() (Runner, error) {
	return &bazelRunner{
		workspaceRoot: workspace.MustRoot(),
		exec:          osCommandExecutor{},
	}, nil
}

func (r *bazelRunner) Run(ctx context.Context, pushTarget string) (*PushOutput, error) {
	if r == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	if strings.TrimSpace(pushTarget) == "" {
		return nil, fmt.Errorf("push target is empty")
	}
	if strings.TrimSpace(r.workspaceRoot) == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}
	if r.exec == nil {
		return nil, fmt.Errorf("command executor is nil")
	}

	if output, err := r.exec.CombinedOutput(ctx, r.workspaceRoot, bazelBinary, bazelRun, bazelNoProgress, bazelOutputFilter, pushTarget); err != nil {
		return nil, bazelCommandError(bazelRun, pushTarget, output, err)
	}

	scriptPath, err := r.pushScriptPath(ctx, pushTarget)
	if err != nil {
		return nil, err
	}

	output, err := parsePushScriptContract(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("parse push contract for %s: %w", pushTarget, err)
	}
	return output, nil
}

func (r *bazelRunner) pushScriptPath(ctx context.Context, pushTarget string) (string, error) {
	output, err := r.exec.CombinedOutput(
		ctx,
		r.workspaceRoot,
		bazelBinary,
		bazelQuery,
		bazelNoProgress,
		bazelOutputFilter,
		pushTarget,
		"--output=starlark",
		`--starlark:expr="\n".join([f.path for f in target.files.to_list()])`,
	)
	if err != nil {
		return "", bazelCommandError(bazelQuery, pushTarget, output, err)
	}

	lines := splitNonEmptyLines(string(output))
	if len(lines) == 0 {
		return "", fmt.Errorf("push target %s did not expose an output file", pushTarget)
	}
	if len(lines) != 1 {
		return "", fmt.Errorf("push target %s exposed %d output files, want 1", pushTarget, len(lines))
	}

	scriptPath := lines[0]
	if !filepath.IsAbs(scriptPath) {
		scriptPath = filepath.Join(r.workspaceRoot, filepath.FromSlash(scriptPath))
	}
	if filepath.Ext(scriptPath) != pushScriptSuffix {
		return "", fmt.Errorf("push target %s output is not a shell script: %s", pushTarget, scriptPath)
	}
	return scriptPath, nil
}

func parsePushScriptContract(scriptPath string) (*PushOutput, error) {
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("read push script %s: %w", scriptPath, err)
	}

	metadata, err := parsePushScriptMetadata(string(raw))
	if err != nil {
		return nil, err
	}
	imageDir, err := resolveRunfile(scriptPath, metadata.imageRunfile)
	if err != nil {
		return nil, fmt.Errorf("resolve image directory %q: %w", metadata.imageRunfile, err)
	}

	output := &PushOutput{}
	output.Repository = parseRepositoryFromFixedArgs(metadata.fixedArgs)
	if output.Repository == "" {
		if metadata.repositoryRunfile != "" {
			repositoryFile, err := resolveRunfile(scriptPath, metadata.repositoryRunfile)
			if err != nil {
				return nil, fmt.Errorf("resolve repository file %q: %w", metadata.repositoryRunfile, err)
			}
			repositoryRaw, err := os.ReadFile(repositoryFile)
			if err != nil {
				return nil, fmt.Errorf("read repository file %s: %w", repositoryFile, err)
			}
			output.RepositoryFile = strings.TrimSpace(string(repositoryRaw))
		}
	}

	digest, err := parseDigest(filepath.Join(imageDir, imageIndexFileName))
	if err != nil {
		return nil, err
	}
	output.Digest = digest

	if _, err := resultFromOutput(output); err != nil {
		return nil, err
	}
	return output, nil
}

func parsePushScriptMetadata(script string) (*pushScriptMetadata, error) {
	metadata := &pushScriptMetadata{}

	scanner := bufio.NewScanner(strings.NewReader(script))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, imageDirPrefix):
			value, err := parseRequiredRlocationValue(line, imageDirPrefix)
			if err != nil {
				return nil, err
			}
			metadata.imageRunfile = value
		case strings.HasPrefix(line, repositoryFilePrefix):
			value, err := parseOptionalRlocationValue(line, repositoryFilePrefix)
			if err != nil {
				return nil, err
			}
			metadata.repositoryRunfile = value
		case strings.HasPrefix(line, fixedArgsPrefix):
			metadata.fixedArgs = parseFixedArgsLine(line)
		}
	}

	if metadata.imageRunfile == "" {
		return nil, fmt.Errorf("missing script entry with prefix %q", imageDirPrefix)
	}

	return metadata, nil
}

func parseRepositoryFromFixedArgs(fixedArgs []string) string {
	for idx, fixedArg := range fixedArgs {
		if fixedArg == "--repository" && idx+1 < len(fixedArgs) {
			return strings.TrimSpace(fixedArgs[idx+1])
		}
		if value, ok := strings.CutPrefix(fixedArg, "--repository="); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseFixedArgsLine(line string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(line, fixedArgsPrefix), ")")
	matches := fixedArgPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	args := make([]string, 0, len(matches))
	for _, match := range matches {
		args = append(args, match[1])
	}
	return args
}

func parseRequiredRlocationValue(line string, prefix string) (string, error) {
	value, err := parseOptionalRlocationValue(line, prefix)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("missing script entry with prefix %q", prefix)
	}
	return value, nil
}

func parseOptionalRlocationValue(line string, prefix string) (string, error) {
	if !strings.HasSuffix(line, rlocationSuffix) {
		return "", fmt.Errorf("malformed script entry: %s", line)
	}
	value := strings.TrimSuffix(strings.TrimPrefix(line, prefix), rlocationSuffix)
	if strings.Contains(value, "{{") {
		return "", nil
	}
	return value, nil
}

func resolveRunfile(scriptPath string, runfilePath string) (string, error) {
	runfilePath = strings.TrimSpace(runfilePath)
	if runfilePath == "" {
		return "", fmt.Errorf("runfile path is empty")
	}

	manifestPath := scriptPath + runfilesManifestName
	if raw, err := os.ReadFile(manifestPath); err == nil {
		actualPath, ok := lookupManifestEntry(string(raw), runfilePath)
		if ok {
			return actualPath, nil
		}
	}

	runfilesPath := filepath.Join(scriptPath+runfilesDirName, filepath.FromSlash(runfilePath))
	if _, err := os.Stat(runfilesPath); err == nil {
		return runfilesPath, nil
	}

	return "", fmt.Errorf("runfile %q not found for %s", runfilePath, scriptPath)
}

func lookupManifestEntry(manifest string, runfilePath string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " ", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == runfilePath {
			return parts[1], true
		}
	}
	return "", false
}

func parseDigest(indexPath string) (string, error) {
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return "", fmt.Errorf("read image index %s: %w", indexPath, err)
	}

	index := new(struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	})
	if err := json.Unmarshal(raw, index); err != nil {
		return "", fmt.Errorf("parse image index %s: %w", indexPath, err)
	}
	if len(index.Manifests) == 0 {
		return "", fmt.Errorf("digest is empty")
	}
	return strings.TrimSpace(index.Manifests[0].Digest), nil
}

func splitNonEmptyLines(raw string) []string {
	parts := strings.Split(raw, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		line := strings.TrimSpace(part)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func bazelCommandError(subcommand string, pushTarget string, output []byte, err error) error {
	message := fmt.Sprintf("bazel %s %s failed", subcommand, pushTarget)
	if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
		return fmt.Errorf("%s: %s: %w", message, trimmed, err)
	}
	return fmt.Errorf("%s: %w", message, err)
}

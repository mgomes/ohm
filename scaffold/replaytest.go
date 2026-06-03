package scaffold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"github.com/mgomes/ohm/replay"
)

const defaultReplayTestsDir = "internal/replaytests"

// ReplayTest describes a generated replay regression test.
type ReplayTest struct {
	SnapshotPath string
	Dir          string
}

// ReplayTestResult describes the file created by GenerateReplayTest.
type ReplayTestResult struct {
	CreatedFile string
}

// GenerateReplayTest writes a Go test that replays a snapshot through the app.
func GenerateReplayTest(cfg ReplayTest) (ReplayTestResult, error) {
	data, err := newReplayTestData(cfg)
	if err != nil {
		return ReplayTestResult{}, err
	}

	body, err := renderReplayTest(data)
	if err != nil {
		return ReplayTestResult{}, err
	}

	if err := os.MkdirAll(data.Dir, 0o755); err != nil {
		return ReplayTestResult{}, fmt.Errorf("create replay tests directory %q: %w", data.Dir, err)
	}
	if err := writeNewFile(data.Path, body); err != nil {
		return ReplayTestResult{}, err
	}

	return ReplayTestResult{CreatedFile: data.Path}, nil
}

type replayTestData struct {
	Dir          string
	Path         string
	Module       string
	TestName     string
	SnapshotJSON string
}

func newReplayTestData(cfg ReplayTest) (replayTestData, error) {
	if cfg.SnapshotPath == "" {
		return replayTestData{}, fmt.Errorf("replay snapshot path is required")
	}

	snapshot, err := readReplaySnapshot(cfg.SnapshotPath)
	if err != nil {
		return replayTestData{}, err
	}
	if _, err := replay.NewRequest(snapshot); err != nil {
		return replayTestData{}, err
	}
	if snapshot.ExpectedResponse == nil {
		return replayTestData{}, fmt.Errorf("replay snapshot %q is missing expected_response; run the generated app replay command with --write-expected first", cfg.SnapshotPath)
	}
	if snapshot.ExpectedResponse.Status < 100 || snapshot.ExpectedResponse.Status > 999 {
		return replayTestData{}, fmt.Errorf("replay snapshot %q expected_response.status is invalid", cfg.SnapshotPath)
	}
	if err := replay.RequireDeterministic(snapshot); err != nil {
		return replayTestData{}, fmt.Errorf("replay snapshot %q is not deterministic: %w", cfg.SnapshotPath, err)
	}

	module, err := readModulePath("go.mod")
	if err != nil {
		return replayTestData{}, err
	}

	fileBase, testName, err := replayTestNames(cfg.SnapshotPath)
	if err != nil {
		return replayTestData{}, err
	}

	snapshotJSON, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return replayTestData{}, fmt.Errorf("encode replay snapshot %q: %w", cfg.SnapshotPath, err)
	}

	dir := cfg.Dir
	if dir == "" {
		dir = defaultReplayTestsDir
	}
	return replayTestData{
		Dir:          dir,
		Path:         filepath.Join(dir, fileBase+"_test.go"),
		Module:       module,
		TestName:     testName,
		SnapshotJSON: strconv.Quote(string(snapshotJSON)),
	}, nil
}

func readReplaySnapshot(path string) (replay.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return replay.Snapshot{}, fmt.Errorf("read replay snapshot %q: %w", path, err)
	}

	var snapshot replay.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return replay.Snapshot{}, fmt.Errorf("decode replay snapshot %q: %w", path, err)
	}
	return snapshot, nil
}

func readModulePath(path string) (string, error) {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve module path from %s: %w\n%s", path, err, output)
	}
	module := strings.TrimSpace(string(output))
	if module == "" {
		return "", fmt.Errorf("%s does not declare a module path", path)
	}
	return module, nil
}

func replayTestNames(snapshotPath string) (fileBase string, testName string, err error) {
	base := filepath.Base(snapshotPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	parts := identifierParts(base)
	if len(parts) == 0 {
		return "", "", fmt.Errorf("replay snapshot name %q must contain a letter or digit", filepath.Base(snapshotPath))
	}

	fileBase = strings.ToLower(strings.Join(parts, "_")) + "_replay"
	var name strings.Builder
	name.WriteString("Test")
	for _, part := range parts {
		name.WriteString(titleIdentifierPart(part))
	}
	name.WriteString("Replay")
	return fileBase, name.String(), nil
}

func titleIdentifierPart(part string) string {
	runes := []rune(strings.ToLower(part))
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func identifierParts(value string) []string {
	var parts []string
	var current strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func renderReplayTest(data replayTestData) ([]byte, error) {
	tmpl, err := template.New("replay-test").Parse(replayTestTemplate)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, err
	}
	if len(formatted) == 0 || formatted[len(formatted)-1] != '\n' {
		formatted = append(formatted, '\n')
	}
	return formatted, nil
}

const replayTestTemplate = `package replaytests

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/mgomes/ohm/replay"

	"{{.Module}}/internal/app"
)

func {{.TestName}}(t *testing.T) {
	var snapshot replay.Snapshot
	if err := json.Unmarshal([]byte({{.SnapshotJSON}}), &snapshot); err != nil {
		t.Fatalf("json.Unmarshal(snapshot) error = %v, want nil", err)
	}
	if snapshot.ExpectedResponse == nil {
		t.Fatalf("snapshot.ExpectedResponse = nil, want response expectation")
	}
	if err := replay.RequireDeterministic(snapshot); err != nil {
		t.Fatalf("replay.RequireDeterministic(snapshot) error = %v, want nil", err)
	}

	response, err := replay.Run(app.New().HTTPHandler(), snapshot)
	if err != nil {
		t.Fatalf("replay.Run(app.New().HTTPHandler(), snapshot) error = %v, want nil", err)
	}

	expected := *snapshot.ExpectedResponse
	result := response.Result()
	defer result.Body.Close()

	if result.StatusCode != expected.Status {
		t.Errorf("replay response status = %d, want %d", result.StatusCode, expected.Status)
	}
	for header, values := range expected.Headers {
		if got := result.Header.Values(header); !slices.Equal(got, values) {
			t.Errorf("replay response header %s = %v, want %v", header, got, values)
		}
	}
	if !expected.BodyOmitted && !bytes.Equal(response.Body.Bytes(), expected.Body) {
		t.Errorf("replay response body = %q, want %q", response.Body.Bytes(), expected.Body)
	}
}
`

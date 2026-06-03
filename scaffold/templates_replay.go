package scaffold

var replayTemplates = map[string]string{
	"tmp/replays/README.md": `# Replay Snapshots

Store local replay snapshot JSON files here while debugging requests.

Replay snapshots are local debugging artifacts. Review them before committing
because they may include scrubbed request and response details.
`,
}

package scaffold

var appTemplates = mergeAppTemplates(
	projectTemplates,
	applicationTemplates,
	databaseTemplates,
	viewTemplates,
	replayTemplates,
)

func mergeAppTemplates(groups ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, group := range groups {
		for path, body := range group {
			if _, ok := merged[path]; ok {
				panic("duplicate app template path: " + path)
			}
			merged[path] = body
		}
	}
	return merged
}

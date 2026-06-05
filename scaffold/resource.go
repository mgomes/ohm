package scaffold

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v2"
)

const defaultQueriesDir = "queries"

var resourceFieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Resource describes generated files for an application resource.
type Resource struct {
	Name   string
	Fields []ResourceField
	Dir    string
	Now    func() time.Time
}

// ResourceField describes a generated resource database field.
type ResourceField struct {
	Name string
	Type string
}

// ResourceResult describes the files changed by GenerateResource.
type ResourceResult struct {
	CreatedFiles    []string
	RegisterFile    string
	RegisterUpdated bool
	RoutePath       string
}

// GenerateResource writes a routed handler skeleton plus database migration and sqlc query files.
func GenerateResource(cfg Resource) (ResourceResult, error) {
	root := cfg.Dir
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	database, err := readResourceDatabase(root)
	if err != nil {
		return ResourceResult{}, err
	}

	data, err := newResourceData(cfg, database)
	if err != nil {
		return ResourceResult{}, err
	}

	handler, err := newHandlerData(Handler{
		Name: cfg.Name,
		Dir:  filepath.Join(root, defaultHandlersDir),
	})
	if err != nil {
		return ResourceResult{}, err
	}

	handlerFiles, err := renderHandlerFiles(handler)
	if err != nil {
		return ResourceResult{}, err
	}

	update, err := prepareHandlerRoute(handler)
	if err != nil {
		return ResourceResult{}, err
	}

	migration, err := prepareMigrationFile(Migration{
		Name: data.MigrationName,
		Dir:  filepath.Join(root, defaultMigrationsDir),
		Now:  cfg.Now,
	}, renderResourceMigration(data))
	if err != nil {
		return ResourceResult{}, err
	}

	files := []resourceFile{
		{path: migration.path, body: migration.body},
		{
			path: filepath.Join(root, defaultQueriesDir, data.TableName+".sql"),
			body: renderResourceQueries(data),
		},
	}
	for _, file := range handlerFiles {
		files = append(files, resourceFile{path: file.path, body: file.body})
	}

	if err := ensureResourceFilesAvailable(files); err != nil {
		return ResourceResult{}, err
	}

	result := ResourceResult{
		CreatedFiles: make([]string, 0, len(files)),
		RegisterFile: update.path,
		RoutePath:    handler.RoutePath,
	}
	for _, file := range files {
		if err := writeNewFile(file.path, file.body); err != nil {
			return ResourceResult{}, err
		}
		result.CreatedFiles = append(result.CreatedFiles, file.path)
	}

	if update.changed {
		if err := writeExistingFile(update.path, update.body); err != nil {
			return ResourceResult{}, err
		}
		result.RegisterUpdated = true
	}

	return result, nil
}

type resourceFile struct {
	path string
	body []byte
}

type resourceData struct {
	Name          string
	TableName     string
	MigrationName string
	Fields        []resourceFieldData
	Database      Database
}

type resourceFieldData struct {
	Name string
	Type string
}

func newResourceData(cfg Resource, database Database) (resourceData, error) {
	if cfg.Name == "" {
		return resourceData{}, fmt.Errorf("resource name is required")
	}
	if !handlerNamePattern.MatchString(cfg.Name) {
		return resourceData{}, fmt.Errorf("resource name %q must start with a letter and contain only letters or digits", cfg.Name)
	}
	if len(cfg.Fields) == 0 {
		return resourceData{}, fmt.Errorf("resource %q requires at least one field", cfg.Name)
	}

	name := upperCamel(cfg.Name)
	tableName := snakeName(name)
	data := resourceData{
		Name:          name,
		TableName:     tableName,
		MigrationName: "create_" + tableName,
		Database:      database,
	}

	seen := map[string]struct{}{"id": {}}
	for _, field := range cfg.Fields {
		normalized, err := normalizeResourceField(field)
		if err != nil {
			return resourceData{}, err
		}
		if _, ok := seen[normalized.Name]; ok {
			return resourceData{}, fmt.Errorf("resource field %q is duplicated or reserved", normalized.Name)
		}
		seen[normalized.Name] = struct{}{}
		data.Fields = append(data.Fields, normalized)
	}

	return data, nil
}

func normalizeResourceField(field ResourceField) (resourceFieldData, error) {
	name := strings.TrimSpace(field.Name)
	if !resourceFieldNamePattern.MatchString(name) {
		return resourceFieldData{}, fmt.Errorf("resource field name %q must start with a lowercase letter and contain only lowercase letters, digits, or underscores", field.Name)
	}

	fieldType := strings.ToLower(strings.TrimSpace(field.Type))
	switch fieldType {
	case "string", "text":
		return resourceFieldData{Name: name, Type: fieldType}, nil
	default:
		return resourceFieldData{}, fmt.Errorf("resource field %q has unsupported type %q; supported types are string and text", name, field.Type)
	}
}

func readResourceDatabase(root string) (Database, error) {
	path := filepath.Join(root, "sqlc.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read sqlc config %q: %w", path, err)
	}

	var cfg sqlcConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse sqlc config %q: %w", path, err)
	}

	for _, engine := range cfg.engines() {
		database, ok := databaseFromSQLCEngine(engine)
		if ok {
			return database, nil
		}
		return "", fmt.Errorf("unsupported sqlc engine %q in %s", engine, path)
	}
	return "", fmt.Errorf("sqlc engine was not found in %s", path)
}

type sqlcConfig struct {
	SQL []struct {
		Engine string `yaml:"engine"`
	} `yaml:"sql"`
	Packages []struct {
		Engine string `yaml:"engine"`
	} `yaml:"packages"`
}

func (cfg sqlcConfig) engines() []string {
	engines := make([]string, 0, len(cfg.SQL)+len(cfg.Packages))
	for _, sql := range cfg.SQL {
		if engine := strings.TrimSpace(sql.Engine); engine != "" {
			engines = append(engines, engine)
		}
	}
	for _, pkg := range cfg.Packages {
		if engine := strings.TrimSpace(pkg.Engine); engine != "" {
			engines = append(engines, engine)
		}
	}
	return engines
}

func databaseFromSQLCEngine(engine string) (Database, bool) {
	switch engine {
	case "postgresql":
		return DatabasePostgres, true
	case "sqlite":
		return DatabaseSQLite, true
	default:
		return "", false
	}
}

func ensureResourceFilesAvailable(files []resourceFile) error {
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if _, ok := seen[file.path]; ok {
			return fmt.Errorf("resource generated duplicate path %q", file.path)
		}
		seen[file.path] = struct{}{}

		dir := filepath.Dir(file.path)
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("inspect directory %q: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path %q is not a directory", dir)
		}

		_, err = os.Stat(file.path)
		if err == nil {
			return fmt.Errorf("file %q already exists", file.path)
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", file.path, err)
		}
	}
	return nil
}

func renderResourceMigration(data resourceData) []byte {
	var buf bytes.Buffer
	buf.WriteString("-- +goose Up\n")
	buf.WriteString("CREATE TABLE ")
	writeSQLIdentifier(&buf, data.TableName)
	buf.WriteString(" (\n")
	buf.WriteString("\t")
	writeSQLIdentifier(&buf, "id")
	switch data.Database {
	case DatabaseSQLite:
		buf.WriteString(" INTEGER PRIMARY KEY AUTOINCREMENT")
	default:
		buf.WriteString(" BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY")
	}
	for _, field := range data.Fields {
		buf.WriteString(",\n\t")
		writeSQLIdentifier(&buf, field.Name)
		buf.WriteString(" TEXT NOT NULL")
	}
	buf.WriteString("\n);\n\n")
	buf.WriteString("-- +goose Down\n")
	buf.WriteString("DROP TABLE ")
	writeSQLIdentifier(&buf, data.TableName)
	buf.WriteString(";\n")
	return buf.Bytes()
}

func renderResourceQueries(data resourceData) []byte {
	var buf bytes.Buffer
	columns := resourceColumns(data)
	fieldColumns := resourceFieldColumns(data)

	fmt.Fprintf(&buf, "-- name: List%s :many\n", data.Name)
	writeSelectColumns(&buf, columns)
	buf.WriteString("\nFROM ")
	writeSQLIdentifier(&buf, data.TableName)
	buf.WriteString("\nORDER BY ")
	writeSQLIdentifier(&buf, "id")
	buf.WriteString(";\n\n")

	fmt.Fprintf(&buf, "-- name: Get%s :one\n", data.Name)
	writeSelectColumns(&buf, columns)
	buf.WriteString("\nFROM ")
	writeSQLIdentifier(&buf, data.TableName)
	buf.WriteString("\nWHERE ")
	writeSQLIdentifier(&buf, "id")
	buf.WriteString(" = ")
	buf.WriteString(resourcePlaceholder(data.Database, 1))
	buf.WriteString(";\n\n")

	fmt.Fprintf(&buf, "-- name: Create%s :one\n", data.Name)
	buf.WriteString("INSERT INTO ")
	writeSQLIdentifier(&buf, data.TableName)
	buf.WriteString(" (")
	writeInlineIdentifiers(&buf, fieldColumns)
	buf.WriteString(")\nVALUES (")
	for i := range fieldColumns {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(resourcePlaceholder(data.Database, i+1))
	}
	buf.WriteString(")\nRETURNING ")
	writeInlineIdentifiers(&buf, columns)
	buf.WriteString(";\n\n")

	fmt.Fprintf(&buf, "-- name: Update%s :one\n", data.Name)
	buf.WriteString("UPDATE ")
	writeSQLIdentifier(&buf, data.TableName)
	buf.WriteString("\nSET ")
	for i, field := range data.Fields {
		if i > 0 {
			buf.WriteString(",\n\t")
		}
		writeSQLIdentifier(&buf, field.Name)
		buf.WriteString(" = ")
		buf.WriteString(resourcePlaceholder(data.Database, i+2))
	}
	buf.WriteString("\nWHERE ")
	writeSQLIdentifier(&buf, "id")
	buf.WriteString(" = ")
	buf.WriteString(resourcePlaceholder(data.Database, 1))
	buf.WriteString("\nRETURNING ")
	writeInlineIdentifiers(&buf, columns)
	buf.WriteString(";\n\n")

	fmt.Fprintf(&buf, "-- name: Delete%s :exec\n", data.Name)
	buf.WriteString("DELETE FROM ")
	writeSQLIdentifier(&buf, data.TableName)
	buf.WriteString("\nWHERE ")
	writeSQLIdentifier(&buf, "id")
	buf.WriteString(" = ")
	buf.WriteString(resourcePlaceholder(data.Database, 1))
	buf.WriteString(";\n")
	return buf.Bytes()
}

func resourceColumns(data resourceData) []string {
	columns := make([]string, 0, len(data.Fields)+1)
	columns = append(columns, "id")
	columns = append(columns, resourceFieldColumns(data)...)
	return columns
}

func resourceFieldColumns(data resourceData) []string {
	columns := make([]string, 0, len(data.Fields))
	for _, field := range data.Fields {
		columns = append(columns, field.Name)
	}
	return columns
}

func writeSelectColumns(buf *bytes.Buffer, columns []string) {
	buf.WriteString("SELECT ")
	writeInlineIdentifiers(buf, columns)
}

func writeInlineIdentifiers(buf *bytes.Buffer, identifiers []string) {
	for i, identifier := range identifiers {
		if i > 0 {
			buf.WriteString(", ")
		}
		writeSQLIdentifier(buf, identifier)
	}
}

func writeSQLIdentifier(buf *bytes.Buffer, identifier string) {
	buf.WriteByte('"')
	buf.WriteString(identifier)
	buf.WriteByte('"')
}

func resourcePlaceholder(database Database, position int) string {
	if database == DatabaseSQLite {
		return fmt.Sprintf("?%d", position)
	}
	return fmt.Sprintf("$%d", position)
}

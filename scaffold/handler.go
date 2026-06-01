package scaffold

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

const defaultHandlersDir = "internal/handlers"

var handlerNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// Handler describes generated handler files.
type Handler struct {
	Name string
	Dir  string
}

// HandlerResult describes the files changed by GenerateHandler.
type HandlerResult struct {
	CreatedFiles    []string
	RegisterFile    string
	RegisterUpdated bool
	RoutePath       string
}

// GenerateHandler writes a handler and wires its route into handlers.Register.
func GenerateHandler(cfg Handler) (HandlerResult, error) {
	data, err := newHandlerData(cfg)
	if err != nil {
		return HandlerResult{}, err
	}

	files, err := renderHandlerFiles(data)
	if err != nil {
		return HandlerResult{}, err
	}
	if err := ensureHandlerFilesAvailable(files); err != nil {
		return HandlerResult{}, err
	}

	update, err := prepareHandlerRoute(data)
	if err != nil {
		return HandlerResult{}, err
	}

	result := HandlerResult{
		CreatedFiles: make([]string, 0, len(files)),
		RegisterFile: update.path,
		RoutePath:    data.RoutePath,
	}
	for _, file := range files {
		if err := writeNewFile(file.path, file.body); err != nil {
			return HandlerResult{}, err
		}
		result.CreatedFiles = append(result.CreatedFiles, file.path)
	}

	if update.changed {
		if err := writeExistingFile(update.path, update.body); err != nil {
			return HandlerResult{}, err
		}
		result.RegisterUpdated = true
	}
	return result, nil
}

type handlerData struct {
	Name         string
	Dir          string
	FileName     string
	TestFileName string
	FunctionName string
	RoutePath    string
	DisplayName  string
}

func newHandlerData(cfg Handler) (handlerData, error) {
	if cfg.Name == "" {
		return handlerData{}, fmt.Errorf("handler name is required")
	}
	if !handlerNamePattern.MatchString(cfg.Name) {
		return handlerData{}, fmt.Errorf("handler name %q must start with a letter and contain only letters or digits", cfg.Name)
	}

	dir := cfg.Dir
	if dir == "" {
		dir = defaultHandlersDir
	}
	name := upperCamel(cfg.Name)
	routeName := routeName(name)
	fileBase := snakeName(name)
	return handlerData{
		Name:         name,
		Dir:          dir,
		FileName:     fileBase + ".go",
		TestFileName: fileBase + "_test.go",
		FunctionName: name + "Index",
		RoutePath:    "/" + routeName,
		DisplayName:  name + "#index",
	}, nil
}

type handlerFile struct {
	path string
	body []byte
}

func renderHandlerFiles(data handlerData) ([]handlerFile, error) {
	templates := []struct {
		path string
		body string
	}{
		{path: data.FileName, body: handlerTemplate},
		{path: data.TestFileName, body: handlerTestTemplate},
	}

	files := make([]handlerFile, 0, len(templates))
	for _, tmpl := range templates {
		body, err := renderHandlerFile(tmpl.path, tmpl.body, data)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", tmpl.path, err)
		}
		files = append(files, handlerFile{
			path: filepath.Join(data.Dir, tmpl.path),
			body: body,
		})
	}
	return files, nil
}

func renderHandlerFile(path string, raw string, data handlerData) ([]byte, error) {
	tmpl, err := template.New(path).Parse(raw)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	body := buf.Bytes()
	if strings.HasSuffix(path, ".go") {
		formatted, err := format.Source(body)
		if err != nil {
			return nil, err
		}
		body = formatted
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	return body, nil
}

func ensureHandlerFilesAvailable(files []handlerFile) error {
	for _, file := range files {
		_, err := os.Stat(file.path)
		if err == nil {
			return fmt.Errorf("file %q already exists", file.path)
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", file.path, err)
		}
	}
	return nil
}

func writeNewFile(path string, body []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("file %q already exists", path)
		}
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, closeErr)
		}
	}()

	if _, err := file.Write(body); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeExistingFile(path string, body []byte) (err error) {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tempPath := temp.Name()
	removeTemp := true
	closed := false
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	defer func() {
		if closed {
			return
		}
		if closeErr := temp.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close temporary file for %s: %w", path, closeErr)
		}
	}()

	if _, err := temp.Write(body); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	closed = true
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	removeTemp = false
	return nil
}

type registerUpdate struct {
	path    string
	body    []byte
	changed bool
}

func prepareHandlerRoute(data handlerData) (registerUpdate, error) {
	path, err := findRegisterFile(data.Dir)
	if err != nil {
		return registerUpdate{}, err
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return registerUpdate{}, fmt.Errorf("parse %s: %w", path, err)
	}

	register := findRegisterFunc(file)
	if register == nil {
		return registerUpdate{}, fmt.Errorf("handlers.Register was not found in %s", path)
	}
	appName, err := registerParamName(register)
	if err != nil {
		return registerUpdate{}, fmt.Errorf("inspect handlers.Register in %s: %w", path, err)
	}
	status := routeRegistrationStatus(register, appName, data)
	switch status {
	case routeRegistered:
		return registerUpdate{path: path}, nil
	case routeConflict:
		return registerUpdate{}, fmt.Errorf("route %q is already registered in %s", data.RoutePath, path)
	}

	register.Body.List = append(register.Body.List, routeRegistration(appName, data))

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, file); err != nil {
		return registerUpdate{}, fmt.Errorf("format %s: %w", path, err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return registerUpdate{}, fmt.Errorf("format %s: %w", path, err)
	}
	return registerUpdate{path: path, body: formatted, changed: true}, nil
}

func findRegisterFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read handlers directory %q: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
		if findRegisterFunc(file) != nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("handlers.Register was not found in %s", dir)
}

func findRegisterFunc(file *ast.File) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "Register" && fn.Body != nil {
			return fn
		}
	}
	return nil
}

type routeStatus int

const (
	routeMissing routeStatus = iota
	routeRegistered
	routeConflict
)

func routeRegistrationStatus(register *ast.FuncDecl, appName string, data handlerData) routeStatus {
	for _, stmt := range register.Body.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Get" {
			continue
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || receiver.Name != appName {
			continue
		}
		route, ok := call.Args[0].(*ast.BasicLit)
		if !ok || route.Kind != token.STRING {
			continue
		}
		routePath, err := strconv.Unquote(route.Value)
		if err != nil || routePath != data.RoutePath {
			continue
		}
		handler, ok := call.Args[1].(*ast.Ident)
		if ok && handler.Name == data.FunctionName {
			return routeRegistered
		}
		return routeConflict
	}
	return routeMissing
}

func registerParamName(register *ast.FuncDecl) (string, error) {
	if register.Type == nil || register.Type.Params == nil || len(register.Type.Params.List) == 0 {
		return "", fmt.Errorf("first parameter is required")
	}
	param := register.Type.Params.List[0]
	if len(param.Names) == 0 || param.Names[0].Name == "" {
		return "", fmt.Errorf("first parameter must be named")
	}
	if param.Names[0].Name == "_" {
		return "", fmt.Errorf("first parameter cannot be blank")
	}
	return param.Names[0].Name, nil
}

func routeRegistration(appName string, data handlerData) ast.Stmt {
	return &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   ast.NewIdent(appName),
				Sel: ast.NewIdent("Get"),
			},
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", data.RoutePath)},
				ast.NewIdent(data.FunctionName),
			},
		},
	}
}

func upperCamel(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func snakeName(name string) string {
	var builder strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			builder.WriteByte('_')
		}
		builder.WriteRune(unicode.ToLower(r))
	}
	return builder.String()
}

func routeName(name string) string {
	return strings.ReplaceAll(snakeName(name), "_", "-")
}

const handlerTemplate = `package handlers

import (
	"net/http"

	"github.com/mgomes/ohm"
)

func {{.FunctionName}}(req *ohm.Request) error {
	req.PlainText(http.StatusOK, "{{.DisplayName}}")
	return nil
}
`

const handlerTestTemplate = `package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgomes/ohm"
)

func Test{{.FunctionName}}(t *testing.T) {
	application := ohm.New()
	application.Get("{{.RoutePath}}", {{.FunctionName}})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "{{.RoutePath}}", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("{{.FunctionName}}(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if response.Body.String() != "{{.DisplayName}}" {
		t.Errorf("{{.FunctionName}}(%s %s) body = %q, want %q", request.Method, request.URL.Path, response.Body.String(), "{{.DisplayName}}")
	}
}
`

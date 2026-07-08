package ohm

import (
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/felixge/httpsnoop"
)

const gzipEncoding = "gzip"

var defaultCompressibleContentTypes = []string{
	"text/html",
	"text/css",
	"text/plain",
	"text/javascript",
	"text/xml",
	"application/javascript",
	"application/x-javascript",
	"application/json",
	"application/xml",
	"application/atom+xml",
	"application/rss+xml",
	"image/svg+xml",
}

// Compress returns middleware that gzip-compresses compressible responses when
// the request accepts gzip encoding.
//
// By default, Compress handles common text, JSON, XML, JavaScript, and SVG
// response types. Passing content types replaces that default set. A content
// type ending in /* matches every subtype below that top-level type.
func Compress(level int, types ...string) Middleware {
	config := newCompressConfig(level, types)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := newCompressResponseState(w, r, config)
			wrapped := state.wrap(w)
			defer state.finish()

			next.ServeHTTP(wrapped, r)
		})
	}
}

type compressConfig struct {
	level     int
	types     map[string]struct{}
	wildcards map[string]struct{}
}

func newCompressConfig(level int, types []string) compressConfig {
	writer, err := gzip.NewWriterLevel(io.Discard, level)
	if err != nil {
		panic(fmt.Sprintf("ohm: invalid gzip compression level %d", level))
	}
	_ = writer.Close()

	if len(types) == 0 {
		types = defaultCompressibleContentTypes
	}

	config := compressConfig{
		level:     level,
		types:     make(map[string]struct{}),
		wildcards: make(map[string]struct{}),
	}
	for _, contentType := range types {
		contentType = strings.ToLower(strings.TrimSpace(contentType))
		if contentType == "" {
			continue
		}
		if strings.Contains(strings.TrimSuffix(contentType, "/*"), "*") {
			panic(fmt.Sprintf("ohm: unsupported compress content type wildcard %q", contentType))
		}
		if wildcard, ok := strings.CutSuffix(contentType, "/*"); ok {
			config.wildcards[wildcard] = struct{}{}
			continue
		}
		contentType = normalizeContentType(contentType)
		if contentType == "" {
			continue
		}
		config.types[contentType] = struct{}{}
	}
	return config
}

func (c compressConfig) allows(contentType string) bool {
	contentType = normalizeContentType(contentType)
	if contentType == "" {
		return false
	}
	if _, ok := c.types[contentType]; ok {
		return true
	}
	prefix, _, ok := strings.Cut(contentType, "/")
	if !ok {
		return false
	}
	_, ok = c.wildcards[prefix]
	return ok
}

type compressResponseState struct {
	response http.ResponseWriter
	request  *http.Request
	config   compressConfig

	writeHeaderFunc httpsnoop.WriteHeaderFunc
	writeFunc       httpsnoop.WriteFunc

	status         int
	wroteHeader    bool
	explicitHeader bool
	headerSnapshot http.Header
	committed      bool
	compressing    bool
	gzipWriter     *gzip.Writer
	gzipClosed     bool
}

func newCompressResponseState(w http.ResponseWriter, r *http.Request, config compressConfig) *compressResponseState {
	return &compressResponseState{
		response: w,
		request:  r,
		config:   config,
		status:   http.StatusOK,
	}
}

func (s *compressResponseState) wrap(w http.ResponseWriter) http.ResponseWriter {
	return httpsnoop.Wrap(w, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			s.writeHeaderFunc = next
			return s.WriteHeader
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			s.writeFunc = next
			return s.Write
		},
		WriteString: func(httpsnoop.WriteStringFunc) httpsnoop.WriteStringFunc {
			return s.WriteString
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(src io.Reader) (int64, error) {
				return s.ReadFrom(next, src)
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() {
				s.Flush(next)
			}
		},
		FlushError: func(next httpsnoop.FlushErrorFunc) httpsnoop.FlushErrorFunc {
			return func() error {
				return s.FlushError(next)
			}
		},
	})
}

func (s *compressResponseState) WriteHeader(status int) {
	if !finalStatus(status) {
		s.writeHeaderFunc(status)
		return
	}
	if s.wroteHeader {
		return
	}
	s.status = status
	s.wroteHeader = true
	s.explicitHeader = true
	s.freezeHeader()
}

func (s *compressResponseState) Write(body []byte) (int, error) {
	if len(body) > 0 {
		s.sniffContentType(body)
	}
	if !s.wroteHeader {
		s.wroteHeader = true
	}
	if len(body) == 0 {
		s.commitUncompressed(s.explicitHeader || s.status != http.StatusOK)
		return s.writeFunc(body)
	}
	if !statusAllowsResponseBody(s.status) {
		s.commitUncompressed(true)
		return 0, http.ErrBodyNotAllowed
	}
	if s.shouldCompress() {
		if err := s.commitCompressed(); err != nil {
			return 0, err
		}
		return s.gzipWriter.Write(body)
	}

	s.commitUncompressed(s.explicitHeader || s.status != http.StatusOK)
	return s.writeFunc(body)
}

func (s *compressResponseState) WriteString(body string) (int, error) {
	return s.Write([]byte(body))
}

func (s *compressResponseState) ReadFrom(next httpsnoop.ReadFromFunc, src io.Reader) (int64, error) {
	if next != nil && s.canDelegateReadFrom() {
		s.commitUncompressed(s.explicitHeader || s.status != http.StatusOK)
		return next(src)
	}
	return io.Copy(compressBodyWriter{state: s}, src)
}

func (s *compressResponseState) Flush(next httpsnoop.FlushFunc) {
	if !s.wroteHeader {
		s.wroteHeader = true
	}
	if !s.committed {
		if s.shouldCompress() {
			if err := s.commitCompressed(); err != nil {
				return
			}
		} else {
			s.commitUncompressed(true)
		}
	}
	if s.gzipWriter != nil {
		_ = s.gzipWriter.Flush()
	}
	next()
}

func (s *compressResponseState) FlushError(next httpsnoop.FlushErrorFunc) error {
	if !s.wroteHeader {
		s.wroteHeader = true
	}
	if !s.committed {
		if s.shouldCompress() {
			if err := s.commitCompressed(); err != nil {
				return err
			}
		} else {
			s.commitUncompressed(true)
		}
	}
	if s.gzipWriter != nil {
		if err := s.gzipWriter.Flush(); err != nil {
			return err
		}
	}
	return next()
}

func (s *compressResponseState) finish() {
	if s.gzipWriter != nil && !s.gzipClosed {
		_ = s.gzipWriter.Close()
		s.gzipClosed = true
	}
	if s.wroteHeader && !s.committed {
		s.commitUncompressed(true)
	}
}

func (s *compressResponseState) commitCompressed() error {
	if s.committed {
		return nil
	}

	header := s.Header()
	addVary(header, "Accept-Encoding")
	header.Set("Content-Encoding", gzipEncoding)
	header.Del("Content-Length")
	header.Del("Accept-Ranges")

	writer, err := gzip.NewWriterLevel(compressBodyWriter{state: s, raw: true}, s.config.level)
	if err != nil {
		return err
	}
	s.gzipWriter = writer
	s.compressing = true
	s.committed = true
	s.restoreHeader(header)
	s.writeHeaderFunc(s.status)
	return nil
}

func (s *compressResponseState) commitUncompressed(forceHeader bool) {
	if s.committed {
		return
	}
	if s.shouldVary() {
		addVary(s.Header(), "Accept-Encoding")
	}
	s.restoreHeader(s.Header())
	s.committed = true
	if forceHeader {
		s.writeHeaderFunc(s.status)
	}
}

func (s *compressResponseState) shouldCompress() bool {
	if s.committed {
		return s.compressing
	}
	if !s.shouldVary() {
		return false
	}
	return acceptsGzip(s.request.Header.Get("Accept-Encoding"))
}

func (s *compressResponseState) canDelegateReadFrom() bool {
	if s.committed {
		return !s.compressing && statusAllowsResponseBody(s.status)
	}
	if !statusAllowsResponseBody(s.status) {
		return false
	}
	if s.request == nil {
		return true
	}
	header := s.Header()
	if s.request.Header.Get("Range") != "" {
		return true
	}
	if partialResponse(s.status, header) {
		return true
	}

	if header.Get("Content-Encoding") != "" || header.Get("Transfer-Encoding") != "" {
		return true
	}
	if _, ok := header["Content-Type"]; !ok {
		return false
	}
	if !s.config.allows(header.Get("Content-Type")) {
		return true
	}
	return !acceptsGzip(s.request.Header.Get("Accept-Encoding"))
}

func (s *compressResponseState) shouldVary() bool {
	if s.committed {
		return false
	}
	if s.request == nil {
		return false
	}
	header := s.Header()
	if s.request.Header.Get("Range") != "" {
		return false
	}
	if partialResponse(s.status, header) {
		return false
	}
	if !statusAllowsResponseBody(s.status) {
		return false
	}
	if header.Get("Content-Encoding") != "" {
		return false
	}
	return s.config.allows(header.Get("Content-Type"))
}

func partialResponse(status int, header http.Header) bool {
	return status == http.StatusPartialContent || header.Get("Content-Range") != ""
}

func (s *compressResponseState) sniffContentType(body []byte) {
	header := s.Header()
	if _, ok := header["Content-Type"]; ok {
		return
	}
	if header.Get("Content-Encoding") != "" || header.Get("Transfer-Encoding") != "" {
		return
	}
	header.Set("Content-Type", http.DetectContentType(body))
}

func (s *compressResponseState) Header() http.Header {
	if s.headerSnapshot != nil {
		return s.headerSnapshot
	}
	return s.response.Header()
}

func (s *compressResponseState) freezeHeader() {
	if s.headerSnapshot != nil {
		return
	}
	s.headerSnapshot = s.response.Header().Clone()
}

func (s *compressResponseState) restoreHeader(header http.Header) {
	if s.headerSnapshot == nil {
		return
	}
	live := s.response.Header()
	clear(live)
	for name, values := range header {
		live[name] = slices.Clone(values)
	}
}

type compressBodyWriter struct {
	state *compressResponseState
	raw   bool
}

func (w compressBodyWriter) Write(body []byte) (int, error) {
	if w.raw {
		return w.state.writeFunc(body)
	}
	return w.state.Write(body)
}

func acceptsGzip(header string) bool {
	if header == "" {
		return false
	}

	var wildcardAccepted bool
	var gzipSeen bool
	var gzipAccepted bool
	for _, value := range strings.Split(header, ",") {
		encoding, q := parseAcceptEncoding(value)
		switch encoding {
		case gzipEncoding:
			gzipSeen = true
			if q > 0 {
				gzipAccepted = true
			}
		case "*":
			if q > 0 {
				wildcardAccepted = true
			}
		}
	}
	if gzipSeen {
		return gzipAccepted
	}
	return wildcardAccepted
}

func parseAcceptEncoding(value string) (string, float64) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0
	}

	encoding, params, err := mime.ParseMediaType(value)
	if err != nil {
		parts := strings.Split(value, ";")
		encoding = parts[0]
		params = parseAcceptEncodingParams(parts[1:])
	}

	q := 1.0
	if rawQ := params["q"]; rawQ != "" {
		parsed, err := strconv.ParseFloat(rawQ, 64)
		if err != nil {
			return strings.ToLower(strings.TrimSpace(encoding)), 0
		}
		q = parsed
	}
	if q < 0 {
		q = 0
	}
	return strings.ToLower(strings.TrimSpace(encoding)), q
}

func parseAcceptEncodingParams(parts []string) map[string]string {
	params := make(map[string]string)
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"`)
		params[key] = value
	}
	return params
}

func normalizeContentType(contentType string) string {
	contentType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		contentType, _, _ = strings.Cut(contentType, ";")
	}
	return strings.ToLower(strings.TrimSpace(contentType))
}

func addVary(header http.Header, name string) {
	name = http.CanonicalHeaderKey(name)
	if name == "" {
		return
	}
	existing := varyHeaderSet(header.Values("Vary"))
	if existing["*"] || existing[name] {
		return
	}
	header.Add("Vary", name)
}

func varyHeaderSet(values []string) map[string]bool {
	headers := make(map[string]bool)
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			header := http.CanonicalHeaderKey(strings.TrimSpace(part))
			if header == "" {
				continue
			}
			headers[header] = true
		}
	}
	return headers
}

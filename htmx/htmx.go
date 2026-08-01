package htmx

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/mgomes/ohm"
)

const (
	// HeaderBoosted identifies requests made through hx-boost.
	HeaderBoosted = "HX-Boosted"
	// HeaderCurrentURL carries the browser's current URL.
	HeaderCurrentURL = "HX-Current-URL"
	// HeaderHistoryRestoreRequest identifies a history cache miss restore.
	HeaderHistoryRestoreRequest = "HX-History-Restore-Request"
	// HeaderPrompt carries the user response to hx-prompt.
	HeaderPrompt = "HX-Prompt"
	// HeaderRequest identifies htmx requests.
	HeaderRequest = "HX-Request"
	// HeaderTarget identifies the target element. htmx 2 sends the element id;
	// htmx 4 sends a CSS identifier built from the tag name and id, such as
	// "section#posts". Request.TargetCandidates lists both readings for
	// matching; Request.TargetID reads the id out of either form.
	HeaderTarget = "HX-Target"
	// HeaderTrigger carries the triggering id on requests and events on responses.
	HeaderTrigger = "HX-Trigger"
	// HeaderTriggerName carries the name of the triggering element when one exists.
	HeaderTriggerName = "HX-Trigger-Name"

	// HeaderLocation performs a client-side redirect without a full reload.
	HeaderLocation = "HX-Location"
	// HeaderPushURL pushes a URL into the browser history stack.
	HeaderPushURL = "HX-Push-Url"
	// HeaderRedirect redirects the browser to a new location.
	HeaderRedirect = "HX-Redirect"
	// HeaderRefresh asks the browser to perform a full refresh.
	HeaderRefresh = "HX-Refresh"
	// HeaderReplaceURL replaces the current URL in the browser history stack.
	HeaderReplaceURL = "HX-Replace-Url"
	// HeaderReselect selects part of the response for swapping.
	HeaderReselect = "HX-Reselect"
	// HeaderReswap overrides the swap behavior.
	HeaderReswap = "HX-Reswap"
	// HeaderRetarget overrides the response target.
	HeaderRetarget = "HX-Retarget"
	// HeaderTriggerAfterSettle triggers client events after settle.
	HeaderTriggerAfterSettle = "HX-Trigger-After-Settle"
	// HeaderTriggerAfterSwap triggers client events after swap.
	HeaderTriggerAfterSwap = "HX-Trigger-After-Swap"
)

// ErrUnknownTarget identifies an htmx request target with no matching fragment.
var ErrUnknownTarget = errors.New("unknown htmx target")

// UnknownTargetError describes an htmx target that does not match a fragment.
type UnknownTargetError struct {
	// Target is the requested htmx target.
	Target string
	// KnownTargets is the set of fragment targets declared by the view.
	KnownTargets []string
}

// Error returns a human-readable unknown target error.
func (e *UnknownTargetError) Error() string {
	if e == nil {
		return ErrUnknownTarget.Error()
	}
	if len(e.KnownTargets) == 0 {
		return fmt.Sprintf("%s %q", ErrUnknownTarget, e.Target)
	}
	return fmt.Sprintf("%s %q; known targets: %s", ErrUnknownTarget, e.Target, strings.Join(e.KnownTargets, ", "))
}

// Is reports whether target matches ErrUnknownTarget.
func (e *UnknownTargetError) Is(target error) bool {
	return target == ErrUnknownTarget
}

// Request describes the htmx-specific request headers Ohm uses.
type Request struct {
	isRequest        bool
	isBoosted        bool
	isHistoryRestore bool
	currentURL       string
	prompt           string
	target           string
	targetID         string
	trigger          string
	triggerName      string
}

// ParseRequest extracts htmx request information from r.
func ParseRequest(r *http.Request) Request {
	if r == nil {
		return Request{}
	}
	header := r.Header
	target := header.Get(HeaderTarget)
	return Request{
		isRequest:        isTrue(header.Get(HeaderRequest)),
		isBoosted:        isTrue(header.Get(HeaderBoosted)),
		isHistoryRestore: isTrue(header.Get(HeaderHistoryRestoreRequest)),
		currentURL:       header.Get(HeaderCurrentURL),
		prompt:           header.Get(HeaderPrompt),
		target:           target,
		targetID:         targetElementID(target),
		trigger:          header.Get(HeaderTrigger),
		triggerName:      header.Get(HeaderTriggerName),
	}
}

// IsRequest reports whether the request came from htmx.
func (r Request) IsRequest() bool {
	return r.isRequest
}

// IsBoosted reports whether the request came from hx-boost.
func (r Request) IsBoosted() bool {
	return r.isBoosted
}

// IsHistoryRestore reports whether htmx is restoring a history cache miss.
func (r Request) IsHistoryRestore() bool {
	return r.isHistoryRestore
}

// CurrentURL returns the browser's current URL, when htmx sent one.
func (r Request) CurrentURL() string {
	return r.currentURL
}

// Prompt returns the user response to hx-prompt, when present.
func (r Request) Prompt() string {
	return r.prompt
}

// Target returns the HX-Target header exactly as htmx sent it. The format
// differs by major version; use TargetCandidates to match against fragment
// targets.
func (r Request) Target() string {
	return r.target
}

// TargetCandidates returns the fragment targets to try for the request, most
// exact first: the HX-Target header verbatim, then the id parsed out of it
// when they differ. It returns nil when htmx sent no target.
//
// A value containing "#" is ambiguous — it may be a whole htmx 2 id or an
// htmx 4 tag#id pair — so match candidates in order and treat two distinct
// matches as a conflict, as Select does.
func (r Request) TargetCandidates() []string {
	if r.target == "" {
		return nil
	}
	if r.targetID != r.target && r.targetID != "" {
		return []string{r.target, r.targetID}
	}
	return []string{r.target}
}

// TargetID returns the id of the target element, when htmx sent one.
//
// htmx 2 sends the bare id in HX-Target ("posts"). htmx 4 sends a CSS
// identifier it builds as
//
//	`${elt.tagName.toLowerCase()}${elt.id ? '#' + encodeURI(elt.id) : ''}`
//
// so "section#posts" for an element with an id, and "section" for one without.
// TargetID returns "posts" for all of "posts", "#posts", and "section#posts".
//
// TargetID reads any value containing "#" as the htmx 4 form, so an htmx 2 id
// that itself contains "#" ("invoice#draft") is reported as just "draft". Use
// TargetCandidates when matching fragment targets; it carries both readings.
//
// A value carrying no "#" is returned unchanged: a bare value may be an htmx 2
// id or the tag name htmx 4 sends for an element with no id, and the two
// cannot be told apart. A fragment named like a bare tag name therefore
// matches an id-less htmx 4 element as well — keep fragment targets element
// ids to stay unambiguous.
func (r Request) TargetID() string {
	return r.targetID
}

// Trigger returns the triggering element id, when htmx sent one.
func (r Request) Trigger() string {
	return r.trigger
}

// TriggerName returns the triggering element name, when htmx sent one.
func (r Request) TriggerName() string {
	return r.triggerName
}

// Option configures htmx rendering.
type Option func(*renderOptions)

// WithSingleFragmentFallback renders the only fragment for targetless htmx requests.
func WithSingleFragmentFallback() Option {
	return func(opts *renderOptions) {
		opts.singleFragmentFallback = true
	}
}

// Render writes either the full HTML view or a matching htmx fragment.
func Render(req *ohm.Request, status int, view ohm.HTMLView, opts ...Option) error {
	if req == nil {
		return fmt.Errorf("htmx render request is required")
	}
	addVary(req.ResponseWriter(), HeaderRequest, HeaderBoosted, HeaderTarget, HeaderHistoryRestoreRequest)
	html, err := Select(req.HTTPRequest(), view, opts...)
	if err != nil {
		return err
	}
	return req.HTML(status, html)
}

// Select returns the HTML that should satisfy r.
func Select(r *http.Request, view ohm.HTMLView, opts ...Option) (ohm.HTML, error) {
	if r == nil {
		return nil, fmt.Errorf("htmx select request is required")
	}

	options := applyOptions(opts)
	request := ParseRequest(r)
	if !request.IsRequest() || request.IsBoosted() || request.IsHistoryRestore() {
		return view.Full(), nil
	}

	if target := request.Target(); target != "" {
		// Try the header verbatim and the id parsed out of it. An id may
		// legally contain "#", which htmx 2 sends raw and encodeURI does not
		// escape, so "invoice#draft" is a whole htmx 2 id and also parses to
		// the htmx 4 id "draft". When both readings resolve, only the htmx
		// version that sent the header could break the tie, and the server
		// cannot see it — so a double match is reported as a fragment-naming
		// conflict rather than silently misrouting one version.
		var match ohm.HTMLFragment
		var matched string
		var found bool
		for _, candidate := range request.TargetCandidates() {
			fragment, ok, err := targetFragment(view, candidate)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if found {
				return nil, fmt.Errorf("htmx target %q is ambiguous: fragments %q and %q both match", target, matched, candidate)
			}
			match, matched, found = fragment, candidate, true
		}
		if !found {
			return nil, unknownTargetError(target, view.Targets())
		}
		return match.HTML(), nil
	}

	if options.singleFragmentFallback {
		if fragment, ok := view.SingleFragment(); ok {
			return fragment.HTML(), nil
		}
	}

	return view.Full(), nil
}

// SetLocation sets HX-Location.
func SetLocation(w http.ResponseWriter, path string) {
	setHeader(w, HeaderLocation, path)
}

// SetPushURL sets HX-Push-Url.
func SetPushURL(w http.ResponseWriter, url string) {
	setHeader(w, HeaderPushURL, url)
}

// SetRedirect sets HX-Redirect.
func SetRedirect(w http.ResponseWriter, url string) {
	setHeader(w, HeaderRedirect, url)
}

// SetRefresh sets HX-Refresh to true.
func SetRefresh(w http.ResponseWriter) {
	setHeader(w, HeaderRefresh, "true")
}

// SetReplaceURL sets HX-Replace-Url.
func SetReplaceURL(w http.ResponseWriter, url string) {
	setHeader(w, HeaderReplaceURL, url)
}

// SetReselect sets HX-Reselect.
func SetReselect(w http.ResponseWriter, selector string) {
	setHeader(w, HeaderReselect, selector)
}

// SetReswap sets HX-Reswap.
func SetReswap(w http.ResponseWriter, swap string) {
	setHeader(w, HeaderReswap, swap)
}

// SetRetarget sets HX-Retarget.
func SetRetarget(w http.ResponseWriter, selector string) {
	setHeader(w, HeaderRetarget, selector)
}

// SetTrigger sets HX-Trigger.
func SetTrigger(w http.ResponseWriter, event string) {
	setHeader(w, HeaderTrigger, event)
}

// SetTriggerAfterSettle sets HX-Trigger-After-Settle.
func SetTriggerAfterSettle(w http.ResponseWriter, event string) {
	setHeader(w, HeaderTriggerAfterSettle, event)
}

// SetTriggerAfterSwap sets HX-Trigger-After-Swap.
func SetTriggerAfterSwap(w http.ResponseWriter, event string) {
	setHeader(w, HeaderTriggerAfterSwap, event)
}

type renderOptions struct {
	singleFragmentFallback bool
}

func applyOptions(opts []Option) renderOptions {
	var options renderOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return options
}

func targetFragment(view ohm.HTMLView, target string) (ohm.HTMLFragment, bool, error) {
	var match ohm.HTMLFragment
	var found bool
	for _, fragment := range view.Fragments() {
		if fragment.Target() != target {
			continue
		}
		if found {
			return ohm.HTMLFragment{}, false, fmt.Errorf("htmx target %q matches multiple fragments", target)
		}
		match = fragment
		found = true
	}
	return match, found, nil
}

func unknownTargetError(target string, knownTargets []string) error {
	knownTargets = slices.Clone(knownTargets)
	return ohm.NewHTTPError(http.StatusBadRequest, "unknown htmx target", &UnknownTargetError{
		Target:       target,
		KnownTargets: knownTargets,
	})
}

// targetElementID extracts the element id from an HX-Target value. See
// Request.TargetID for the formats htmx sends.
func targetElementID(target string) string {
	hash := strings.IndexByte(target, '#')
	if hash < 0 {
		return target
	}
	id := target[hash+1:]
	// htmx 4 passes the id through encodeURI, which escapes spaces and percent
	// signs but leaves "#" alone — so taking everything after the first "#"
	// also handles the pathological case of an id that contains one.
	if decoded, err := url.PathUnescape(id); err == nil {
		return decoded
	}
	return id
}

func isTrue(value string) bool {
	return strings.EqualFold(value, "true")
}

func setHeader(w http.ResponseWriter, key string, value string) {
	if w == nil {
		return
	}
	w.Header().Set(key, value)
}

func addVary(w http.ResponseWriter, headers ...string) {
	if w == nil {
		return
	}

	existing := varyHeaderSet(w.Header().Values("Vary"))
	for _, header := range headers {
		canonical := http.CanonicalHeaderKey(header)
		if canonical == "" || existing[canonical] {
			continue
		}
		w.Header().Add("Vary", canonical)
		existing[canonical] = true
	}
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

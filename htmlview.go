package ohm

import "slices"

// HTMLView describes a server-rendered HTML response with optional fragments.
type HTMLView struct {
	full      HTML
	fragments []HTMLFragment
}

// View creates an HTMLView from a full response and optional named fragments.
func View(full HTML, fragments ...HTMLFragment) HTMLView {
	return HTMLView{
		full:      full,
		fragments: slices.Clone(fragments),
	}
}

// Full returns the HTML rendered for normal full-page responses.
func (v HTMLView) Full() HTML {
	return v.full
}

// Fragments returns the fragments declared for this view.
func (v HTMLView) Fragments() []HTMLFragment {
	return slices.Clone(v.fragments)
}

// Fragment returns the fragment with target when one exists.
func (v HTMLView) Fragment(target string) (HTMLFragment, bool) {
	for _, fragment := range v.fragments {
		if fragment.Target() == target {
			return fragment, true
		}
	}
	return HTMLFragment{}, false
}

// SingleFragment returns the only fragment when the view has exactly one.
func (v HTMLView) SingleFragment() (HTMLFragment, bool) {
	if len(v.fragments) != 1 {
		return HTMLFragment{}, false
	}
	return v.fragments[0], true
}

// Targets returns fragment target names in declaration order.
func (v HTMLView) Targets() []string {
	targets := make([]string, 0, len(v.fragments))
	for _, fragment := range v.fragments {
		targets = append(targets, fragment.Target())
	}
	return targets
}

// HTMLFragment describes a named HTML fragment for a page region.
type HTMLFragment struct {
	target string
	html   HTML
}

// Fragment creates an HTML fragment for target.
func Fragment(target string, html HTML) HTMLFragment {
	return HTMLFragment{
		target: target,
		html:   html,
	}
}

// Target returns the page-region target name for this fragment.
func (f HTMLFragment) Target() string {
	return f.target
}

// HTML returns the HTML rendered for this fragment.
func (f HTMLFragment) HTML() HTML {
	return f.html
}

package cli

import _ "embed"

// The art contains backticks, so it cannot live in a raw string literal.
//
//go:embed banner.txt
var banner string

// Banner returns the ASCII wordmark shown above the help text.
func Banner() string {
	return banner
}

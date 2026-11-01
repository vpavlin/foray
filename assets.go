// Package foray holds what the binary carries with it: the files a list's
// assistant needs in its folder, so the service can set one up on a machine
// that has no checkout (internal/assist).
package foray

import _ "embed"

// AgentsMD is the assistant's instructions (session/AGENTS.md).
//
//go:embed session/AGENTS.md
var AgentsMD []byte

// BrowserPy and BrowserSh are the assistant's browser tools (browser/).
//
//go:embed browser/foray_browser.py
var BrowserPy []byte

//go:embed browser/foray-browser
var BrowserSh []byte

package provider

import (
	"fmt"
	"net/url"
)

// RefuseCredentialedEndpoint rejects a base URL that carries userinfo --
// https://token@host or https://user:password@host. net/http would send it as
// Basic auth on every request to whatever host the URL names; it sits in shell
// history, in `ps`, and in config in clear; and it is not how kolk holds a
// credential. The error names the host and never the credential, and says
// where a key belongs. Anything else about the URL is left to the caller.
func RefuseCredentialedEndpoint(baseURL string) error {
	// A URL that does not parse is someone else's error to report; only a
	// parsed URL can carry userinfo.
	if u, err := url.Parse(baseURL); err == nil && u.User != nil {
		return fmt.Errorf("the base URL for %s carries a username or password; kolk refuses it: "+
			"net/http would send it as Basic auth on every request, and it would sit in shell history and config in clear. "+
			"Give the URL without credentials. A key for kolk belongs in /key; an endpoint's own token belongs in its own login", u.Host)
	}
	return nil
}

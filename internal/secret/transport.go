package secret

import (
	"fmt"
	"net/http"
)

// AuthTransport attaches a credential to outgoing requests without the caller
// ever holding one.
//
// This exists because of a specific, verified leak that defeats every other
// precaution in this package. A Secret that redacts perfectly under %v, %+v,
// %#v and json.Marshal still leaks the moment it becomes an HTTP header:
//
//	req.Header.Set("Authorization", "Bearer "+key.Reveal())
//	log.Printf("request failed: %+v", req)     // prints the whole key
//
// http.Header is a plain map[string][]string. Nothing about it knows a secret
// went in, so nothing about it can redact one coming out — and *http.Request is
// exactly the kind of value that ends up in an error message, a retry log, or a
// debug dump when a call fails, which is precisely when someone shares it.
//
// The fix is structural rather than disciplinary: the header is set on a CLONE,
// inside RoundTrip, on a request object that never escapes this function. The
// request the caller built and can print never contains the token, so there is
// no way for a caller to leak it by accident, and no rule anyone has to
// remember.
type AuthTransport struct {
	// Token is attached to every request. It is a Secret rather than a string
	// so that AuthTransport itself is safe to print.
	Token Secret

	// Scheme defaults to "Bearer".
	Scheme string

	// Header defaults to "Authorization".
	Header string

	// Extra is added to every request — for provider headers like
	// HTTP-Referer and X-Title that are not secret but must not be forgotten.
	Extra map[string]string

	// Base defaults to http.DefaultTransport.
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone, always. The contract of RoundTripper is that it must not modify
	// the request it is given, and here that contract is also the security
	// boundary: the caller's request stays free of the token.
	r := req.Clone(req.Context())

	if !t.Token.IsZero() {
		scheme := t.Scheme
		if scheme == "" {
			scheme = "Bearer"
		}
		header := t.Header
		if header == "" {
			header = "Authorization"
		}
		r.Header.Set(header, scheme+" "+t.Token.Reveal())
	}
	for k, v := range t.Extra {
		if r.Header.Get(k) == "" {
			r.Header.Set(k, v)
		}
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	resp, err := base.RoundTrip(r)
	if err != nil {
		// A transport error quotes the URL, and a misconfigured endpoint can
		// carry credentials in a query string. Scrub on the way out.
		return resp, ScrubError(err)
	}
	return resp, nil
}

// String keeps AuthTransport itself printable. Without it, %+v on a client that
// holds one would print the struct field by field and defeat the point.
func (t *AuthTransport) String() string {
	return fmt.Sprintf("secret.AuthTransport{Token: %s}", t.Token)
}

// GoString covers %#v for the same reason.
func (t *AuthTransport) GoString() string { return t.String() }

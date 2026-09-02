package secret

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// ErrCredentialOrigin is returned before network I/O when a transport would
// otherwise attach a credential to an origin it was not constructed to trust.
var ErrCredentialOrigin = errors.New("secret: credential origin is not allowed")

type credentialOrigin struct {
	scheme string
	host   string
	port   string
}

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
	// token is private and synchronized so RoundTrip can validate and attach
	// one immutable snapshot while SetToken runs concurrently.
	tokenMu sync.RWMutex
	token   Secret

	// Scheme defaults to "Bearer".
	Scheme string

	// Header defaults to "Authorization".
	Header string

	// Extra is added to every request — for provider headers like
	// HTTP-Referer and X-Title that are not secret but must not be forgotten.
	Extra map[string]string

	// Base defaults to http.DefaultTransport.
	Base http.RoundTripper

	// allowedOrigin is private so changing a request URL cannot silently move
	// the credential's trust boundary. Credential-bearing transports must be
	// created with NewAuthTransport.
	allowedOrigin credentialOrigin
}

// NewAuthTransport constructs a transport whose credential is bound to the
// origin of allowedURL. Paths do not participate in an HTTP origin; scheme,
// host, and effective port do. The normalized binding is private and cannot be
// changed after construction.
func NewAuthTransport(token Secret, allowedURL string, base http.RoundTripper) (*AuthTransport, error) {
	u, err := url.Parse(allowedURL)
	if err != nil {
		return nil, fmt.Errorf("secret: invalid credential origin: %w", err)
	}
	origin, err := normalizeCredentialOrigin(u)
	if err != nil {
		return nil, err
	}
	return &AuthTransport{
		token:         token,
		Base:          base,
		allowedOrigin: origin,
	}, nil
}

// Token returns a synchronized credential snapshot. The Secret remains safe
// to print; revealing it still requires an explicit Reveal call.
func (t *AuthTransport) Token() Secret {
	if t == nil {
		return Secret{}
	}
	t.tokenMu.RLock()
	defer t.tokenMu.RUnlock()
	return t.token
}

// SetToken atomically replaces the credential used by future requests.
func (t *AuthTransport) SetToken(token Secret) {
	if t == nil {
		return
	}
	t.tokenMu.Lock()
	t.token = token
	t.tokenMu.Unlock()
}

// RoundTrip implements http.RoundTripper.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token := t.Token()
	if !token.IsZero() {
		origin, err := normalizeCredentialOrigin(req.URL)
		if err != nil || origin != t.allowedOrigin || t.allowedOrigin == (credentialOrigin{}) {
			return nil, ErrCredentialOrigin
		}
	}

	// Clone, always. The contract of RoundTripper is that it must not modify
	// the request it is given, and here that contract is also the security
	// boundary: the caller's request stays free of the token.
	r := req.Clone(req.Context())

	if !token.IsZero() {
		scheme := t.Scheme
		if scheme == "" {
			scheme = "Bearer"
		}
		header := t.Header
		if header == "" {
			header = "Authorization"
		}
		r.Header.Set(header, scheme+" "+token.Reveal())
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

func normalizeCredentialOrigin(u *url.URL) (credentialOrigin, error) {
	if u == nil || !u.IsAbs() || u.Host == "" || u.User != nil {
		return credentialOrigin{}, ErrCredentialOrigin
	}

	scheme := strings.ToLower(u.Scheme)
	// Host names are compared ASCII-only. strings.ToLower folds U+0130 (İ) to
	// the ASCII letter i, so "openrouter.aİ" would compare equal to the
	// canonical host here while net/http applies IDNA and dials
	// openrouter.xn--ai-sub. Found by the V34.1a independent reviewer. A
	// non-ASCII host is never an origin this transport binds; the user's
	// endpoint still works, without the credential.
	host := u.Hostname()
	if !isASCII(host) {
		return credentialOrigin{}, ErrCredentialOrigin
	}
	host = strings.ToLower(host)
	if host == "" {
		return credentialOrigin{}, ErrCredentialOrigin
	}
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return credentialOrigin{}, ErrCredentialOrigin
		}
	}
	return credentialOrigin{scheme: scheme, host: host, port: port}, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// SameOrigin reports whether two absolute HTTP URLs share the normalized
// scheme, host, and effective port used by AuthTransport. Invalid or
// userinfo-bearing URLs never match.
func SameOrigin(first, second string) bool {
	leftURL, err := url.Parse(first)
	if err != nil {
		return false
	}
	rightURL, err := url.Parse(second)
	if err != nil {
		return false
	}
	left, err := normalizeCredentialOrigin(leftURL)
	if err != nil {
		return false
	}
	right, err := normalizeCredentialOrigin(rightURL)
	return err == nil && left == right
}

// String keeps AuthTransport itself printable. Without it, %+v on a client that
// holds one would print the struct field by field and defeat the point.
func (t *AuthTransport) String() string {
	return fmt.Sprintf("secret.AuthTransport{Token: %s}", t.Token())
}

// GoString covers %#v for the same reason.
func (t *AuthTransport) GoString() string { return t.String() }

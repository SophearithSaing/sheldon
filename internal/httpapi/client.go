package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"
)

// Client contacts a numeric loopback instance without proxies or redirects.
// Verify must succeed before Open or BrowserURL can expose its credential.
// A Client is used sequentially, not concurrently.
type Client struct {
	origin   string
	token    string
	http     *http.Client
	verified bool
}

// NewClient validates a discovery record without sending any requests.
func NewClient(origin, token string) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("invalid local instance address")
	}
	ip, err := netip.ParseAddr(u.Hostname())
	port, portErr := strconv.Atoi(u.Port())
	if err != nil || !ip.IsLoopback() || ip.Is4In6() || ip.Zone() != "" || portErr != nil || port < 1 || port > 65535 || origin != "http://"+net.JoinHostPort(ip.String(), strconv.Itoa(port)) {
		return nil, errors.New("invalid local instance address")
	}
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != token {
		return nil, errors.New("invalid local instance credential")
	}
	return &Client{origin: origin, token: token, http: &http.Client{
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// instanceProof binds a fresh challenge and origin to the API token without disclosing it.
func instanceProof(token, origin, challenge string) string {
	mac := hmac.New(sha256.New, []byte(token))
	io.WriteString(mac, "sheldon-instance-v1\n"+origin+"\n"+challenge)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// identify proves possession of this instance's API token without returning it.
func (s *Server) identify(w http.ResponseWriter, r *http.Request) {
	values := r.Header.Values("X-Sheldon-Challenge")
	if len(values) != 1 {
		writeError(w, http.StatusBadRequest, "challenge required")
		return
	}
	challenge, err := base64.RawURLEncoding.DecodeString(values[0])
	if err != nil || len(challenge) != 32 || base64.RawURLEncoding.EncodeToString(challenge) != values[0] {
		writeError(w, http.StatusBadRequest, "invalid challenge")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"proof": instanceProof(s.apiToken, s.origin, values[0])})
}

// Verify checks a fresh server proof before any bearer credential is transmitted.
// An unrelated listener reached through a stale record receives no bearer token
// during verification; this is not protection against a later port takeover.
func (c *Client) Verify(ctx context.Context) error {
	c.verified = false
	challenge, err := newSecret()
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, "GET", c.origin+"/instance-proof", nil)
	if err != nil {
		return errors.New("cannot prepare instance verification")
	}
	r.Header.Set("X-Sheldon-Challenge", challenge)
	response, err := c.http.Do(r)
	if err != nil {
		return errors.New("local instance is unavailable")
	}
	defer response.Body.Close()
	var body struct {
		Proof string `json:"proof"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	if response.StatusCode != http.StatusOK || err != nil || len(data) > 1024 || json.Unmarshal(data, &body) != nil || !equalSecret(body.Proof, instanceProof(c.token, c.origin, challenge)) {
		return errors.New("local instance verification failed")
	}
	c.verified = true
	return nil
}

// Open forwards a PR-open intent to a verified instance without starting agent work.
func (c *Client) Open(ctx context.Context, prURL string) error {
	if !c.verified {
		return errors.New("local instance has not been verified")
	}
	data, _ := json.Marshal(map[string]string{"url": prURL})
	r, err := http.NewRequestWithContext(ctx, "POST", c.origin+"/api/open", bytes.NewReader(data))
	if err != nil {
		return errors.New("cannot prepare open request")
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(r)
	if err != nil {
		return errors.New("open request failed; delivery may be unknown")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return errors.New("local instance rejected open request")
	}
	return nil
}

// BrowserURL returns a verified launch URL with a transient, client-cleared fragment.
// Never print or log this URL. The fragment is not sent in HTTP requests.
func (c *Client) BrowserURL() (string, error) {
	if !c.verified {
		return "", errors.New("local instance has not been verified")
	}
	return c.origin + "/#token=" + c.token, nil
}

// String returns a credential-safe client description.
func (c *Client) String() string { return "Sheldon client at " + c.origin + " (credential withheld)" }

// GoString returns the same credential-safe representation for %#v formatting.
func (c *Client) GoString() string { return c.String() }

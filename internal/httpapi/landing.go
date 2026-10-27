package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
)

// landingScript clears the launch fragment before making authenticated requests.
// This temporary runtime page is not the embedded application or a PR resolver.
const landingScript = `const token = new URLSearchParams(location.hash.slice(1)).get('token');
history.replaceState(null, '', '/');
const status = document.getElementById('status');
async function connect(value) {
  try {
    const response = await fetch('/api/open', {headers: {Authorization: 'Bearer ' + value}, cache: 'no-store', credentials: 'omit'});
    if (!response.ok) throw new Error();
    const state = await response.json();
    status.textContent = state.url ? 'Requested PR: ' + state.url + '. PR loading is not implemented yet.' : 'Runtime connected. No PR requested.';
  } catch { status.textContent = 'Cannot connect. Run sheldon again to reopen this page.'; }
}
document.getElementById('connect').addEventListener('submit', event => {
  event.preventDefault();
  const input = document.getElementById('token');
  const value = input.value;
  input.value = '';
  connect(value);
});
if (token) connect(token);
`

// landing serves an honest, minimal runtime page until the embedded UI is built.
func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	hash := sha256.Sum256([]byte(landingScript))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Sheldon runtime</title><h1>Sheldon</h1><p>Local runtime only. The application UI, PR loading and agent integration are not implemented yet.</p><p id="status" role="status">Run sheldon to connect, or enter the API token from its private instance record.</p><form id="connect"><label for="token">API token</label> <input id="token" type="password" autocomplete="off" required> <button>Connect</button></form><script>`+landingScript+`</script></html>`)
}

package githubpr

import "testing"

func TestNormalizeURL(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/owner/repo/pull/1",
		"https://GITHUB.com/owner/repo/pull/1/?tab=files#discussion",
	} {
		u, err := NormalizeURL(raw)
		if err != nil || u != "https://github.com/owner/repo/pull/1" {
			t.Fatalf("%q: %q, %v", raw, u, err)
		}
	}
	for _, raw := range []string{
		"", "http://github.com/owner/repo/pull/1", "https://github.com.attacker.example/owner/repo/pull/1",
		"https://github.com@attacker.example/owner/repo/pull/1", "https://user@github.com/owner/repo/pull/1",
		"https://github.com:443/owner/repo/pull/1", "https://github.com/owner/repo/issues/1",
		"https://github.com/owner/repo/pull/0", "https://github.com/owner/repo/pull/-1",
		"https://github.com/owner/repo/pull/+1", "https://github.com/owner/repo/pull/01",
		"https://github.com/owner/repo/pull/18446744073709551616", "https://github.com/owner/repo/pull/1/files",
		"https://github.com/owner/repo/pull/1;touch", "https://github.com/owner/repo/pull/1\n",
		"https://github.com/owner/repo/pull/$(id)", "https://github.com/owner/repo/pull/1 && id",
		"https://github.com/owner%2Frepo/repo/pull/1", "https://github.com/owner/repo/pull/%31",
		"https://github.com/owner/../pull/1", "https://github.com/-owner/repo/pull/1",
		"https://github.com/owner-/repo/pull/1", "https://github.com/owner/repo\\other/pull/1",
		"file:///owner/repo/pull/1", "javascript:alert(1)",
	} {
		if u, err := NormalizeURL(raw); err == nil {
			t.Errorf("accepted %q as %q", raw, u)
		}
	}
}

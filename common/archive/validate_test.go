package archive

import "testing"

func TestIsValidGitHubDownloadURL(t *testing.T) {
	cases := []struct {
		u    string
		want bool
	}{
		{"https://github.com/AsikaProject/asika/releases/download/v1/asika-linux-amd64.tar.gz", true},
		{"https://objects.githubusercontent.com/github/...", true},
		{"http://github.com/evil/asika-linux-amd64.tar.gz", false},
		{"https://evil.com/asika-linux-amd64.tar.gz", false},
		{"https://github.com.evil.com/", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsValidGitHubDownloadURL(c.u); got != c.want {
			t.Errorf("IsValidGitHubDownloadURL(%q) = %v, want %v", c.u, got, c.want)
		}
	}
}

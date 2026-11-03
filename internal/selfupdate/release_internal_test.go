package selfupdate

import (
	"net/http"
	"testing"
	"time"
)

func TestNewGitHubSource_ProductionClients(t *testing.T) {
	t.Parallel()

	src := NewGitHubSource(nil, DefaultAPIBase, "")
	if src.api.Timeout != 15*time.Second {
		t.Errorf("metadata timeout = %s, want 15s", src.api.Timeout)
	}
	if src.assets.Timeout != 0 {
		t.Errorf("asset total timeout = %s, want none", src.assets.Timeout)
	}
	tr, ok := src.assets.Transport.(*http.Transport)
	if !ok || tr.ResponseHeaderTimeout != 30*time.Second || tr.Proxy == nil {
		t.Errorf("asset transport = %+v, want a 30s ResponseHeaderTimeout and proxy from environment", src.assets.Transport)
	}
}

package remote

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
)

// TestNetErr comprueba que el error de red pierde la URL (con sus
// credenciales) y conserva la causa.
func TestNetErr(t *testing.T) {
	raw := &url.Error{Op: "Get", URL: "https://host/rest/ping?u=me&t=secret&s=salt", Err: io.EOF}
	err := netErr("subsonic: ping", raw)
	if got := err.Error(); got != "subsonic: ping: EOF" || strings.Contains(got, "secret") {
		t.Errorf("netErr = %q", got)
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("the cause was lost: %v", err)
	}
	dl := netErr("x", &url.Error{Op: "Get", URL: "u", Err: context.DeadlineExceeded})
	if !errors.Is(dl, context.DeadlineExceeded) {
		t.Errorf("the timeout was lost: %v", dl)
	}
	if other := errors.New("plain"); netErr("x", other) != other {
		t.Errorf("a non-network error changed")
	}
}

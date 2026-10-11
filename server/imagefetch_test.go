package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The remote fetch's guards, each proved by a destination that would accept
// the connection were the guard missing: a listener that counts what it
// accepts, and an assertion that the count stayed zero.

// recorder is an HTTP server that counts the connections it accepts.
type recorder struct {
	srv     *httptest.Server
	accepts atomic.Int64
	ap      netip.AddrPort
}

func newRecorder(t *testing.T, h http.Handler, tls bool) *recorder {
	t.Helper()
	r := &recorder{}
	r.srv = httptest.NewUnstartedServer(h)
	r.srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			r.accepts.Add(1)
		}
	}
	if tls {
		r.srv.StartTLS()
	} else {
		r.srv.Start()
	}
	t.Cleanup(r.srv.Close)
	r.ap = netip.MustParseAddrPort(r.srv.Listener.Addr().String())
	return r
}

// port is the recorder's port as text.
func (r *recorder) port() string { return fmt.Sprint(r.ap.Port()) }

// redirector answers /to?u=URL with a redirect to URL, /chain/N with a
// redirect to /chain/N-1 down to a picture at /chain/0, and anything else
// with the picture.
func redirector(pic []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := r.URL.Query().Get("u"); u != "" {
			http.Redirect(w, r, u, http.StatusFound)
			return
		}
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/chain/%d", &n); err == nil && n > 0 {
			http.Redirect(w, r, fmt.Sprintf("/chain/%d", n-1), http.StatusFound)
			return
		}
		w.Write(pic)
	})
}

func fetchPolicy(allow []string, ports []int, exempt ...netip.AddrPort) ImagePolicy {
	return ImagePolicy{FetchAllow: allow, FetchHTTP: true, FetchPorts: ports, exempt: exempt}.ready()
}

// TestImageFetchAllowListPath: an allow-listed host on an allowed port is
// fetched, through up to three redirects and not four.
func TestImageFetchAllowListPath(t *testing.T) {
	pic := testPNG(t, 6, 2, color.White)
	o := newRecorder(t, redirector(pic), false)
	p := fetchPolicy([]string{"127.0.0.1"}, []int{int(o.ap.Port())}, o.ap)
	img, err := p.imageURL(context.Background(), o.srv.URL+"/x.png")
	if err != nil || img.Bounds().Dx() != 6 {
		t.Fatalf("the allow-listed fetch: %v", err)
	}
	if _, err := p.imageURL(context.Background(), o.srv.URL+"/chain/3"); err != nil {
		t.Fatalf("three redirects: %v", err)
	}
	if _, err := p.imageURL(context.Background(), o.srv.URL+"/chain/4"); err == nil ||
		!strings.Contains(err.Error(), "more than 3 redirects") {
		t.Fatalf("four redirects: %v", err)
	}
}

// TestImageFetchForbiddenDestinationsAreNeverReached: each forbidden
// destination is a live listener that would accept the connection, and none
// does.
func TestImageFetchForbiddenDestinationsAreNeverReached(t *testing.T) {
	pic := testPNG(t, 2, 2, color.White)
	target := newRecorder(t, redirector(pic), false)
	origin := newRecorder(t, redirector(pic), false)
	tlsOrigin := newRecorder(t, redirector(pic), true)
	tp, op := int(target.ap.Port()), int(origin.ap.Port())
	via := func(o *recorder, to string) string { return o.srv.URL + "/to?u=" + to }
	cases := []struct {
		name, url, want string
		p               ImagePolicy
	}{
		// The special-use ranges: the target is on the allow-list and an
		// allowed port, and only the address policy stands between.
		{"a redirect to a loopback name", via(origin, "http://localhost:"+target.port()+"/"), "special-use",
			fetchPolicy([]string{"127.0.0.1", "localhost"}, []int{op, tp}, origin.ap)},
		{"a redirect to an IPv4-mapped address", via(origin, "http://[::ffff:127.0.0.1]:"+target.port()+"/"), "special-use",
			fetchPolicy([]string{"127.0.0.1", "::ffff:127.0.0.1"}, []int{op, tp}, origin.ap)},
		{"a redirect to NAT64", via(origin, "http://[64:ff9b::7f00:1]:"+target.port()+"/"), "special-use",
			fetchPolicy([]string{"127.0.0.1", "64:ff9b::7f00:1"}, []int{op, tp}, origin.ap)},
		{"a direct loopback URL", target.srv.URL, "special-use",
			fetchPolicy([]string{"127.0.0.1"}, []int{tp})},
		// The URL policy: the target is exempt from the address policy, so
		// only the rule under test stands between.
		{"a redirect to a host not on the list", via(origin, "http://localhost:"+target.port()+"/"), "not on this server's image fetch allow-list",
			fetchPolicy([]string{"127.0.0.1"}, []int{op, tp}, origin.ap, target.ap)},
		{"a redirect to a port not allowed", via(origin, target.srv.URL), "names port",
			fetchPolicy([]string{"127.0.0.1"}, []int{op}, origin.ap, target.ap)},
		{"an https to http downgrade", via(tlsOrigin, target.srv.URL), "redirects from https to http",
			fetchPolicy([]string{"127.0.0.1"}, []int{int(tlsOrigin.ap.Port()), tp}, tlsOrigin.ap, target.ap)},
		{"userinfo", "http://u:p@127.0.0.1:" + target.port() + "/", "userinfo",
			fetchPolicy([]string{"127.0.0.1"}, []int{tp}, target.ap)},
		{"a redirect carrying userinfo", via(origin, "http://u:p@127.0.0.1:"+target.port()+"/"), "userinfo",
			fetchPolicy([]string{"127.0.0.1"}, []int{op, tp}, origin.ap, target.ap)},
		{"an IPv6 zone", "http://[fe80::1%25lo]:" + target.port() + "/", "zone",
			fetchPolicy([]string{"fe80::1"}, []int{tp}, target.ap)},
		{"plain http with https only", target.srv.URL,
			"https only", ImagePolicy{FetchAllow: []string{"127.0.0.1"}, FetchPorts: []int{tp},
				exempt: []netip.AddrPort{target.ap}}.ready()},
		{"fetching off", target.srv.URL, "does not fetch remote images",
			ImagePolicy{FetchHTTP: true, FetchPorts: []int{tp}, exempt: []netip.AddrPort{target.ap}}.ready()},
	}
	for _, c := range cases {
		c.p.roots = tlsOrigin.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
		before := target.accepts.Load()
		_, err := c.p.imageURL(context.Background(), c.url)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want one containing %q", c.name, err, c.want)
		}
		if n := target.accepts.Load() - before; n != 0 {
			t.Errorf("%s: the forbidden destination accepted %d connection(s)", c.name, n)
		}
	}
}

// TestDeniedAddr is the address policy over a sample of every range, and the
// public addresses it lets through.
func TestDeniedAddr(t *testing.T) {
	v4 := func(a, b, c, d byte) netip.Addr { return netip.AddrFrom4([4]byte{a, b, c, d}) }
	v6 := netip.MustParseAddr
	denied := []netip.Addr{
		v4(0, 1, 2, 3), v4(10, 1, 2, 3), v4(100, 64, 0, 1), v4(127, 0, 0, 1), v4(169, 254, 169, 254),
		v4(172, 16, 0, 1), v4(192, 0, 0, 9), v4(192, 0, 2, 1), v4(192, 88, 99, 1), v4(192, 168, 1, 1),
		v4(198, 18, 0, 1), v4(198, 51, 100, 1), v4(203, 0, 113, 1), v4(224, 0, 0, 1), v4(240, 0, 0, 1),
		v4(255, 255, 255, 255),
		v6("::"), v6("::1"), v6("::ffff:8.8.8.8"), v6("64:ff9b::808:808"), v6("64:ff9b:1::1"), v6("100::1"),
		v6("2001::1"), v6("2001:1::1"), v6("2001:db8::1"), v6("2002::1"), v6("fd00::1"), v6("fe80::1"),
		v6("ff02::1"), v6("fe80::1%lo"),
	}
	for _, a := range denied {
		if !deniedAddr(a) {
			t.Errorf("%s is let through", a)
		}
	}
	for _, a := range []netip.Addr{v4(8, 8, 8, 8), v4(1, 1, 1, 1), v6("2606:4700::1111")} {
		if deniedAddr(a) {
			t.Errorf("%s, a public address, is denied", a)
		}
	}
}

// TestImageFetchAllowListMatching: case, IDNA and a trailing dot normalise;
// a wildcard is one label deep; nothing matches by suffix.
func TestImageFetchAllowListMatching(t *testing.T) {
	p, err := ImagePolicy{FetchAllow: []string{"Images.Example.COM.", "*.cdn.example.org", "bücher.example"}}.Validate()
	if err != nil {
		t.Fatal(err)
	}
	for h, want := range map[string]bool{
		"images.example.com": true, "IMAGES.example.com.": true, "a.cdn.example.org": true,
		"xn--bcher-kva.example": true, "bücher.example": true,
		"evilimages.example.com": false, "x.images.example.com": false, "cdn.example.org": false,
		"a.b.cdn.example.org": false, "example.com": false,
	} {
		n, err := normHost(h)
		if err != nil {
			t.Fatalf("%s: %v", h, err)
		}
		if got := p.hostAllowed(n); got != want {
			t.Errorf("%s: allowed=%v, want %v", h, got, want)
		}
	}
	if _, err := (ImagePolicy{FetchAllow: []string{"ex*ample.com"}}).Validate(); err == nil {
		t.Error("a wildcard inside a name was taken")
	}
}

// TestInlinePicturesNeedNoFetching: a data: URL and a base64 source are
// taken with fetching off and an empty allow-list, through the policy and
// through both APIs.
func TestInlinePicturesNeedNoFetching(t *testing.T) {
	pic := testPNG(t, 3, 3, color.White)
	p := ImagePolicy{}.ready()
	if len(p.FetchAllow) != 0 {
		t.Fatal("the default policy lists hosts")
	}
	if _, err := p.imageURL(context.Background(), dataURL("image/png", pic)); err != nil {
		t.Fatalf("a data: URL with fetching off: %v", err)
	}
	f := &fakeBackend{tokens: []string{"ok"}, reason: FinishEOS}
	s := serve(t, f)
	for _, x := range []struct{ path, body string }{
		{"/v1/chat/completions", fmt.Sprintf(`{"model":"m-1","messages":[{"role":"user","content":[
			{"type":"image_url","image_url":{"url":%q}}]}]}`, dataURL("image/png", pic))},
		{"/v1/messages", fmt.Sprintf(`{"model":"m-1","max_tokens":4,"messages":[{"role":"user","content":[
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":%q}}]}]}`, base64.StdEncoding.EncodeToString(pic))},
	} {
		resp := post(t, s, x.path, x.body)
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || len(f.last.Prompt.Chat.Images) != 1 {
			t.Errorf("%s with fetching off: %d %s", x.path, resp.StatusCode, raw)
		}
	}
}

// TestImageFetchLimits: the server-wide fetch bound, the request's byte
// total, and the decode deadline.
func TestImageFetchLimits(t *testing.T) {
	pic := testPNG(t, 2, 2, color.White)
	hold := make(chan struct{})
	slow := newRecorder(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-hold
		w.Write(pic)
	}), false)
	p := ImagePolicy{FetchAllow: []string{"127.0.0.1"}, FetchHTTP: true, FetchPorts: []int{int(slow.ap.Port())},
		FetchConcurrency: 1, FetchTimeout: 5 * time.Second, exempt: []netip.AddrPort{slow.ap}}.ready()
	first := make(chan error, 1)
	go func() {
		_, err := p.imageURL(context.Background(), slow.srv.URL)
		first <- err
	}()
	for slow.accepts.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err := p.imageURL(ctx, slow.srv.URL)
	cancel()
	close(hold)
	<-first
	if err == nil || !strings.Contains(err.Error(), "remote fetches are running") {
		t.Fatalf("a fetch past the server-wide bound: %v", err)
	}

	ps := newPictures(ImagePolicy{MaxRequestBytes: int64(len(pic)) + 1}.ready())
	load := func() error {
		return ps.add("x", func() (image.Image, error) { return ps.p.decodeDataURL(dataURL("image/png", pic)) })
	}
	if err := load(); err != nil {
		t.Fatal(err)
	}
	if err := load(); err == nil || !strings.Contains(err.Error(), "over the limit") {
		t.Fatalf("a request past its byte total: %v", err)
	}

	if _, err := (ImagePolicy{DecodeTimeout: time.Nanosecond}).ready().decodeImage(testPNG(t, 512, 512, color.White), ""); err == nil ||
		!strings.Contains(err.Error(), "did not decode within") {
		t.Fatalf("a decode past its deadline: %v", err)
	}
}

package slideshow

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConfiguration(t *testing.T) {
	h, err := New("")
	if err != nil || h != nil {
		t.Fatalf("empty source should be disabled: handler=%v err=%v", h, err)
	}
	for _, source := range []string{"/slideshow/demo", "ftp://localhost/player", "http://user:secret@localhost/player", "http://localhost/player#key", "http://localhost/a/../admin", "http://localhost/a%252fb"} {
		if _, err := New(source); err == nil {
			t.Errorf("accepted unsafe configuration %q", source)
		}
	}
}

func TestSelectedPlayerRewritingAndCredentialIsolation(t *testing.T) {
	var sourceURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/screen/player?show=reception" {
			t.Errorf("wrong source: %s", r.URL.RequestURI())
		}
		for _, name := range []string{"Authorization", "Cookie", "Origin", "Referer", "Range"} {
			if r.Header.Get(name) != "" {
				t.Errorf("forwarded private or inappropriate header %s", name)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Set-Cookie", "source_secret=private")
		fmt.Fprintf(w, `<html><head><base href="https://bad.invalid/"><link href="/static/player.css"><script src="player.js"></script></head><body><img src='/uploads/a.jpg'><video poster="%s/uploads/p.jpg"></video><script>fetch('/api/time');fetch('/api/slideshow/slug/' + slug + '/status');const media="/poster/uploads/v.mp4";</script></body></html>`, sourceURL)
	}))
	defer upstream.Close()
	sourceURL = upstream.URL
	h, err := New(upstream.URL + "/screen/player?show=reception")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/slideshow/?ignored=1", nil)
	req.Header.Set("Authorization", "Bearer tablet-secret")
	req.Header.Set("Cookie", "app_session=private")
	req.Header.Set("Origin", "https://private.invalid")
	req.Header.Set("Referer", "https://private.invalid/admin")
	req.Header.Set("Range", "bytes=0-4")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	for _, expected := range []string{`<base href="/slideshow/screen/">`, `href="/slideshow/static/player.css"`, `src="/slideshow/screen/player.js"`, `src='/slideshow/uploads/a.jpg'`, `poster="/slideshow/uploads/p.jpg"`, `fetch('/slideshow/api/time')`, `fetch('/slideshow/api/slideshow/slug/'`, `"/slideshow/poster/uploads/v.mp4"`, "event.source !== parent", "event.origin !== location.origin", "window.donRatingActive = true", "window.donRatingActive = false", "document.dispatchEvent(new Event('don:resume'))"} {
		if !strings.Contains(w.Body.String(), expected) {
			t.Errorf("missing player rewrite/bridge %q", expected)
		}
	}
	if strings.Contains(w.Body.String(), "https://bad.invalid/") {
		t.Error("source base tag survived")
	}
	if w.Header().Get("X-Frame-Options") != "" || w.Header().Get("Set-Cookie") != "" {
		t.Error("source framing or session headers escaped")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Error("same-origin framing policy missing")
	}
	if w.Header().Get("Permissions-Policy") != "fullscreen=()" || !strings.Contains(w.Body.String(), "Element.prototype.requestFullscreen = function () { return Promise.resolve(); }") {
		t.Error("embedded player can take fullscreen above the guest overlay")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("player should not retain stale bridge or source HTML")
	}
	if bridge, script := strings.Index(w.Body.String(), "data-don-bridge"), strings.Index(w.Body.String(), `src="/slideshow/screen/player.js"`); bridge < 0 || bridge > script {
		t.Error("bridge must load before source scripts")
	}
}

func TestSourceAssetURLsAndVideoClientTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `.slide{background:url(/uploads/a)}`
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()
	handler, err := New(upstream.URL + "/screen/player")
	if err != nil {
		t.Fatal(err)
	}
	p := handler.(*proxy)
	if p.client.Timeout != 0 {
		t.Fatal("full video body is subject to a short client timeout")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slideshow/static/player.css", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "url(/slideshow/uploads/a)") || w.Header().Get("Content-Length") != "" {
		t.Fatalf("asset rewrite: status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
}

func TestReadOnlyAllowlist(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer upstream.Close()
	h, err := New(upstream.URL + "/screen/player")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/slideshow/api/time", "/slideshow/api/slideshow/slug/reception-2/status", "/slideshow/uploads/video.mp4", "/slideshow/poster/uploads/image.png", "/slideshow/static/css/app.css", "/slideshow/assets/font.woff2", "/slideshow/screen/player.js"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusOK {
			t.Errorf("public resource %s: %d", target, w.Code)
		}
	}
	before := calls.Load()
	for _, target := range []string{"/slideshow/admin", "/slideshow/api/admin/state", "/slideshow/api/device/state", "/slideshow/api/slideshows", "/slideshow/api/slideshow/1/play", "/slideshow/screen/private", "/slideshow/static/../admin", "/slideshow/static/%2e%2e/admin", "/slideshow/static/%252e%252e/admin", "/slideshow//admin", "/slideshow/static/", "/other"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("restricted route %s returned %d", target, w.Code)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/slideshow/api/time", strings.NewReader(`{"rating":5}`)))
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("write method %s was not refused", method)
		}
	}
	if calls.Load() != before {
		t.Error("forbidden requests reached upstream")
	}
}

func TestVideoRangesAndHead(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/uploads/video.mp4" || r.URL.RawQuery != "v=2" {
			t.Errorf("wrong media URL %s", r.URL)
		}
		if r.Header.Get("Range") != "bytes=3-6" || r.Header.Get("If-Range") != `"video-version"` {
			t.Error("range request lost")
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Content-Range", "bytes 3-6/12")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"video-version"`)
		w.WriteHeader(http.StatusPartialContent)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, "3456")
		}
	}))
	defer upstream.Close()
	h, _ := New(upstream.URL + "/player")
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequest(method, "/slideshow/uploads/video.mp4?v=2", nil)
		r.Header.Set("Range", "bytes=3-6")
		r.Header.Set("If-Range", `"video-version"`)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusPartialContent || w.Header().Get("Content-Range") != "bytes 3-6/12" || w.Header().Get("Content-Length") != "4" || w.Header().Get("Accept-Ranges") != "bytes" {
			t.Errorf("%s range response lost: %d %v", method, w.Code, w.Header())
		}
		if (method == http.MethodGet && w.Body.String() != "3456") || (method == http.MethodHead && w.Body.Len() != 0) {
			t.Errorf("%s returned incorrect body", method)
		}
	}
}

func TestRedirectsNeverFollowed(t *testing.T) {
	var redirected atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
	}))
	defer destination.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", destination.URL+"/admin")
		w.Header().Set("Set-Cookie", "session=private")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	h, _ := New(upstream.URL + "/player")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slideshow/", nil))
	if w.Code != http.StatusBadGateway || redirected.Load() != 0 || w.Header().Get("Location") != "" || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("redirect was not contained: status=%d calls=%d headers=%v", w.Code, redirected.Load(), w.Header())
	}
}

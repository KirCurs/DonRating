// Package slideshow embeds an explicitly configured, existing slideshow player.
// It exposes a small read-only subset of the source application, never its editor.
package slideshow

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const prefix = "/slideshow/"
const maxPlayerBytes = 8 << 20

type proxy struct {
	player    *url.URL
	assetBase string
	client    *http.Client
}

// New returns nil when no source is configured. playerURL is the full URL of
// the standalone player, not the application's editor or administration page.
// The returned handler must be mounted at /slideshow/ without stripping it.
func New(playerURL string) (http.Handler, error) {
	if strings.TrimSpace(playerURL) == "" {
		return nil, nil
	}
	u, err := url.Parse(playerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || !safePath(u.Path) {
		return nil, fmt.Errorf("slideshow source must be a full HTTP(S) player URL without credentials or a fragment")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = 10 * time.Second
	return &proxy{
		player: u, assetBase: path.Dir(u.Path) + "/",
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "read-only slideshow", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.URL.Path, prefix) || !safePath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	upstreamPath := "/" + strings.TrimPrefix(r.URL.Path, prefix)
	playerRequest := upstreamPath == "/"
	if !playerRequest && !p.allowed(upstreamPath) {
		http.NotFound(w, r)
		return
	}
	target := *p.player
	if !playerRequest {
		target.Path = upstreamPath
		target.RawPath = ""
		target.RawQuery = r.URL.RawQuery
	}
	mediaRequest := strings.HasPrefix(upstreamPath, "/uploads/") || strings.HasPrefix(upstreamPath, "/poster/uploads/")
	ctx := r.Context()
	if mediaRequest {
		// Videos can legitimately stream for longer than an ordinary API response.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target.String(), nil)
	if err != nil {
		http.Error(w, "slideshow unavailable", http.StatusBadGateway)
		return
	}
	// Only media/conditional headers cross this boundary. In particular, neither
	// the Go app's tablet credential nor source cookies reach the other app.
	requestHeaders := []string{"Accept"}
	if !playerRequest {
		requestHeaders = append(requestHeaders, "Range", "If-Range", "If-None-Match", "If-Modified-Since")
	}
	for _, name := range requestHeaders {
		if value := r.Header.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "Don-Slideshow-ReadOnly/1.0")
	res, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "slideshow unavailable", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode < 400 && res.StatusCode != http.StatusNotModified {
		http.Error(w, "slideshow redirect refused", http.StatusBadGateway)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Cache-Control", "ETag", "Last-Modified", "Content-Encoding"} {
		if value := res.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	isHTML := strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "text/html")
	contentType := strings.ToLower(res.Header.Get("Content-Type"))
	isTextAsset := strings.HasPrefix(contentType, "text/css") || strings.Contains(contentType, "javascript")
	if isTextAsset && r.Method != http.MethodHead {
		body, err := io.ReadAll(io.LimitReader(res.Body, maxPlayerBytes+1))
		if err != nil || len(body) > maxPlayerBytes {
			http.Error(w, "slideshow asset is too large or incomplete", http.StatusBadGateway)
			return
		}
		w.Header().Del("Content-Length")
		w.Header().Del("ETag")
		w.Header().Del("Last-Modified")
		w.WriteHeader(res.StatusCode)
		_, _ = w.Write(p.rewritePublicPaths(body))
		return
	}
	if isHTML {
		// Replace source framing rules only for this explicitly selected player.
		// The parent remains the sole allowed frame ancestor.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://cdnjs.cloudflare.com; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; font-src 'self' https://fonts.gstatic.com https://cdnjs.cloudflare.com; frame-ancestors 'self'; object-src 'none'; base-uri 'self'; form-action 'none'")
		w.Header().Set("Permissions-Policy", "fullscreen=()")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Del("Content-Length")
		w.Header().Del("ETag")
		w.Header().Del("Last-Modified")
		if res.Header.Get("Content-Encoding") != "" && res.Header.Get("Content-Encoding") != "identity" {
			w.Header().Del("Content-Encoding")
			http.Error(w, "compressed slideshow HTML is unsupported", http.StatusBadGateway)
			return
		}
		if r.Method != http.MethodHead {
			body, err := io.ReadAll(io.LimitReader(res.Body, maxPlayerBytes+1))
			if err != nil || len(body) > maxPlayerBytes {
				http.Error(w, "slideshow response is too large or incomplete", http.StatusBadGateway)
				return
			}
			body = p.rewriteHTML(body)
			w.WriteHeader(res.StatusCode)
			_, _ = w.Write(body)
			return
		}
	}
	w.WriteHeader(res.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, res.Body)
	}
}

func safePath(value string) bool {
	if strings.ContainsAny(value, "%\\\x00\r\n") || strings.Contains(value, "//") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (p *proxy) allowed(value string) bool {
	if !safePath(value) {
		return false
	}
	if value == "/api/time" {
		return true
	}
	segments := strings.Split(strings.Trim(value, "/"), "/")
	if len(segments) == 5 && segments[0] == "api" && segments[1] == "slideshow" && segments[2] == "slug" && segments[3] != "" && segments[4] == "status" {
		return true
	}
	for _, base := range []string{"/uploads/", "/poster/uploads/", "/static/", "/assets/"} {
		if strings.HasPrefix(value, base) && len(value) > len(base) && !strings.HasSuffix(value, "/") {
			return true
		}
	}
	// Relative scripts, styles, and fonts can live beside a source player.
	// Limit that exception to asset extensions, not adjacent application routes.
	if p.assetBase != "//" && strings.HasPrefix(value, p.assetBase) {
		switch strings.ToLower(path.Ext(value)) {
		case ".css", ".js", ".mjs", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".otf", ".mp4", ".webm", ".ogg", ".mov":
			return true
		}
	}
	return false
}

var (
	attributePattern = regexp.MustCompile(`(?i)\b(?:src|href|poster)\s*=\s*(?:"[^"]*"|'[^']*')`)
	basePattern      = regexp.MustCompile(`(?i)<base\b[^>]*>`)
	headPattern      = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
)

func (p *proxy) rewriteHTML(body []byte) []byte {
	body = basePattern.ReplaceAll(body, nil)
	body = attributePattern.ReplaceAllFunc(body, func(attribute []byte) []byte {
		s := string(attribute)
		quote := strings.IndexAny(s, "\"'")
		if quote < 0 {
			return attribute
		}
		reference := html.UnescapeString(s[quote+1 : len(s)-1])
		u, err := url.Parse(reference)
		if err != nil || reference == "" || strings.HasPrefix(reference, "#") {
			return attribute
		}
		u = p.player.ResolveReference(u)
		if u.Scheme != p.player.Scheme || !strings.EqualFold(u.Host, p.player.Host) {
			return attribute
		}
		u.Scheme, u.Host, u.User = "", "", nil
		u.Path = strings.TrimSuffix(prefix, "/") + u.Path
		u.RawPath = ""
		return []byte(s[:quote+1] + html.EscapeString(u.String()) + s[len(s)-1:])
	})
	// The preserved player uses root-relative fetches and media strings inside
	// inline JavaScript. Rewrite only its known public resource families.
	body = p.rewritePublicPaths(body)
	assetBase := p.assetBase
	if assetBase == "//" {
		assetBase = "/"
	}
	baseURL := (&url.URL{Path: strings.TrimSuffix(prefix, "/") + assetBase}).String()
	addition := []byte(`<base href="` + html.EscapeString(baseURL) + `">` + pauseBridge)
	if location := headPattern.FindIndex(body); location != nil {
		result := make([]byte, 0, len(body)+len(addition))
		result = append(result, body[:location[1]]...)
		result = append(result, addition...)
		result = append(result, body[location[1]:]...)
		return result
	}
	return append(addition, body...)
}

func (p *proxy) rewritePublicPaths(body []byte) []byte {
	for _, root := range []string{"/api/", "/uploads/", "/poster/uploads/", "/static/", "/assets/"} {
		for _, quote := range []string{"\"", "'", "`"} {
			body = bytes.ReplaceAll(body, []byte(quote+root), []byte(quote+strings.TrimSuffix(prefix, "/")+root))
			body = bytes.ReplaceAll(body, []byte(quote+p.player.Scheme+"://"+p.player.Host+root), []byte(quote+strings.TrimSuffix(prefix, "/")+root))
		}
		body = bytes.ReplaceAll(body, []byte("url("+root), []byte("url("+strings.TrimSuffix(prefix, "/")+root))
	}
	return body
}

// Install before source scripts. Pausing animation callbacks also supports the
// original player, which predates the local donRatingActive flag.
const pauseBridge = `<script data-don-bridge>
(function () {
  'use strict';
  // Fullscreen belongs to the outer tablet page. A child fullscreen element
  // otherwise enters the browser's top layer and hides the rating overlay.
  if (window.parent !== window) {
    Element.prototype.requestFullscreen = function () { return Promise.resolve(); };
    Element.prototype.webkitRequestFullscreen = function () {};
  }
  var paused = false, nextId = 0, frames = new Map(), playingVideos = new Set();
  var requestFrame = window.requestAnimationFrame.bind(window);
  var cancelFrame = window.cancelAnimationFrame.bind(window);
  window.requestAnimationFrame = function (callback) {
    var id = ++nextId, entry = { callback: callback, nativeId: null };
    frames.set(id, entry);
    function run(time) {
      entry.nativeId = null;
      if (paused) return;
      frames.delete(id);
      callback(time);
    }
    entry.run = run;
    if (!paused) entry.nativeId = requestFrame(run);
    return id;
  };
  window.cancelAnimationFrame = function (id) {
    var entry = frames.get(id);
    if (!entry) return;
    if (entry.nativeId !== null) cancelFrame(entry.nativeId);
    frames.delete(id);
  };
  function pauseVideos() {
    document.querySelectorAll('video').forEach(function (video) {
      var slide = video.closest('.slide');
      if (!video.paused && (!slide || slide.classList.contains('active'))) playingVideos.add(video);
      video.pause();
    });
  }
  document.addEventListener('play', function (event) {
    if (paused && event.target instanceof HTMLMediaElement) event.target.pause();
  }, true);
  window.addEventListener('message', function (event) {
    if (event.source !== parent || event.origin !== location.origin || !event.data) return;
    if (event.data.type === 'don:pause') {
      paused = true;
      window.donRatingActive = true;
      pauseVideos();
    } else if (event.data.type === 'don:resume') {
      paused = false;
      window.donRatingActive = false;
      document.dispatchEvent(new Event('don:resume'));
      frames.forEach(function (entry) {
        if (entry.nativeId === null) entry.nativeId = requestFrame(entry.run);
      });
      document.querySelectorAll('.slide.active video').forEach(function (video) { playingVideos.add(video); });
      playingVideos.forEach(function (video) {
        if (!video.isConnected) return;
        var playing = video.play();
        if (playing && playing.catch) playing.catch(function () {});
      });
      playingVideos.clear();
    }
  });
})();
</script>`

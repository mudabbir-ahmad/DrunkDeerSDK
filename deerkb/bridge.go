package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
)

// bridgeShim runs before the page. The page calls fetch('/api/...') and expects an HTTP server, so the
// shim sends those calls to Go through the rpc binding instead. Export is a plain link, which a
// WebView2 page cannot download from a data-less origin, so its click is turned into a blob download.
const bridgeShim = `(function () {
  var apiRe = /^\/api\//;
  var exportRe = /^\/api\/profiles\/([^/]+)\/export$/;
  var origFetch = window.fetch;

  function response(status, text) {
    var isJSON = true;
    try { JSON.parse(text); } catch (e) { isJSON = false; }
    return {
      ok: status < 400,
      status: status,
      statusText: '',
      headers: { get: function (k) { return k.toLowerCase() === 'content-type' ? (isJSON ? 'application/json' : 'text/plain') : null; } },
      json: function () { return Promise.resolve().then(function () { return JSON.parse(text); }); },
      text: function () { return Promise.resolve(text); }
    };
  }

  window.fetch = function (input, init) {
    var url = typeof input === 'string' ? input : '';
    if (!apiRe.test(url)) return origFetch.apply(this, arguments);
    init = init || {};
    var method = (init.method || 'GET').toUpperCase();
    var body = typeof init.body === 'string' ? init.body : '';
    return window.rpc(method, url, body).then(function (raw) {
      var r = JSON.parse(raw);
      return response(r.status, r.body);
    });
  };

  function exportName(a) {
    var href = a && a.getAttribute ? a.getAttribute('href') : null;
    var m = href && exportRe.exec(href);
    return m ? m[1] : null;
  }

  function saveExport(name) {
    return window.rpc('GET', '/api/profiles/' + name + '/export', '').then(function (raw) {
      var r = JSON.parse(raw);
      if (r.status >= 400) throw new Error(r.body);
      var url = URL.createObjectURL(new Blob([r.body], { type: 'application/json' }));
      var a = document.createElement('a');
      a.href = url;
      a.download = decodeURIComponent(name) + '.json';
      document.body.appendChild(a);
      HTMLAnchorElement.prototype.click.call(a);
      a.remove();
      setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
    }).catch(function (err) { console.error(err); });
  }

  // A detached <a> that the page clicks never reaches a document listener, so the click method is wrapped too.
  var nativeClick = HTMLAnchorElement.prototype.click;
  HTMLAnchorElement.prototype.click = function () {
    var name = exportName(this);
    if (name !== null) { saveExport(name); return; }
    return nativeClick.apply(this, arguments);
  };
  document.addEventListener('click', function (e) {
    var a = e.target && e.target.closest ? e.target.closest('a') : null;
    var name = exportName(a);
    if (name === null) return;
    e.preventDefault();
    saveExport(name);
  }, true);
})();`

// memWriter is an http.ResponseWriter that keeps the reply in memory.
type memWriter struct {
	h      http.Header
	status int
	body   bytes.Buffer
}

func (m *memWriter) Header() http.Header { return m.h }

func (m *memWriter) WriteHeader(code int) {
	if m.status == 0 {
		m.status = code
	}
}

func (m *memWriter) Write(b []byte) (int, error) {
	if m.status == 0 {
		m.status = http.StatusOK
	}
	return m.body.Write(b)
}

// serveInProcess runs one request through h without a socket. Writes carry X-DeerKB, as the page sends them.
func serveInProcess(h http.Handler, method, target string, body []byte) (status int, respBody []byte) {
	if !strings.HasPrefix(target, "/") {
		return http.StatusBadRequest, []byte("target must start with /")
	}
	req, err := http.NewRequest(method, "http://app"+target, bytes.NewReader(body))
	if err != nil {
		return http.StatusBadRequest, []byte(err.Error())
	}
	if method != http.MethodGet {
		req.Header.Set("X-DeerKB", "1")
	}
	w := &memWriter{h: http.Header{}}
	h.ServeHTTP(w, req)
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.status, w.body.Bytes()
}

// rpcBinding is the Go side of the shim's window.rpc: it returns {"status": N, "body": "..."} as JSON text.
// It never returns a Go error: a failure, or a panic in a handler, is sent back as a 500 reply, so a bad
// request cannot end the window.
func rpcBinding(h http.Handler) func(method, target, body string) (string, error) {
	return func(method, target, body string) (reply string, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic in rpc %s %s: %v\n%s", method, target, r, debug.Stack())
				reply = encodeReply(http.StatusInternalServerError, fmt.Sprintf("internal error: %v", r))
				err = nil
			}
		}()
		status, respBody := serveInProcess(h, method, target, []byte(body))
		return encodeReply(status, string(respBody)), nil
	}
}

// encodeReply is the JSON text the shim parses.
func encodeReply(status int, body string) string {
	out, err := json.Marshal(struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}{status, body})
	if err != nil {
		return `{"status":500,"body":"reply could not be encoded"}`
	}
	return string(out)
}

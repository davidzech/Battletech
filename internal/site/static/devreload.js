// Development only (the server's -dev-reload flag): reload the page when the server restarts. The server sends its
// boot ID on GET /dev/reload; EventSource reconnects on its own after a restart, and a new ID means new code.
(function () {
  if (!window.EventSource) return;
  var boot = null;
  var es = new EventSource('/dev/reload');
  es.onmessage = function (e) {
    if (boot === null) boot = e.data;
    else if (e.data !== boot) location.reload();
  };
})();

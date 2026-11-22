package site

import (
	"net/http"
	"net/url"

	"github.com/davidzech/Battletech/internal/mekbayproxy"
	"github.com/davidzech/Battletech/internal/storage"
)

// The site's side of the MekBay proxy (package mekbayproxy): the links it hands out, and the options tests use to point it elsewhere.

const tableLabel = "table"

// mekbayLink is the link that opens MekBay acting as account (the link for this playthrough's campaign and label, made on first use and the
// same ever after), with the given query: ?instance=<force> loads a force and ?operation=<id> an operation.
// tableLabel is the label of the link the whole table shares.
func (s *Server) mekbayLink(r *http.Request, playthroughID, campaign, label, account string, query url.Values) (string, error) {
	id, err := s.store.EnsureMekBayLink(r.Context(), playthroughID, campaign, label, account, storage.LinkTTL, s.proxy.Now())
	if err != nil {
		return "", err
	}
	scheme := "http"
	if mekbayproxy.IsHTTPS(r) {
		scheme = "https"
	}
	link := scheme + "://" + storage.LinkPrefix + id + "." + mekbayproxy.SiteHost(r.Host) + "/"
	if len(query) > 0 {
		link += "?" + query.Encode()
	}
	return link, nil
}

// withMekBayUpstream points the proxy at another MekBay (tests use a fake).
func withMekBayUpstream(web, ws, origin string) Option {
	return func(s *Server) { s.upstream = &[3]string{web, ws, origin} }
}

// withMekBayProxyHook hands the proxy to a test once it is built.
func withMekBayProxyHook(f func(*mekbayproxy.Proxy)) Option { return func(s *Server) { s.onProxy = f } }

//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package httpclient

import "net/http"

// BearerAuth returns an AuthFunc that sets Authorization: Bearer.
func BearerAuth(apiKey string) AuthFunc {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// HeaderAuth returns an AuthFunc that sets a custom header.
func HeaderAuth(header, value string) AuthFunc {
	return func(req *http.Request) {
		req.Header.Set(header, value)
	}
}

// QueryParamAuth returns an AuthFunc that adds a query parameter.
func QueryParamAuth(param, value string) AuthFunc {
	return func(req *http.Request) {
		q := req.URL.Query()
		q.Set(param, value)
		req.URL.RawQuery = q.Encode()
	}
}

// NoAuth returns an AuthFunc that does nothing.
func NoAuth() AuthFunc {
	return func(_ *http.Request) {}
}

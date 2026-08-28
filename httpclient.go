package main

import (
	"net/http"
	"time"
)

// Transport compartilhado: reaproveita conexões TCP/TLS com o SIGAA em vez
// de abrir um socket novo a cada request (o código antigo criava um
// http.Client por chamada, sem timeout e sem pool).
var sharedTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 16,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
	ForceAttemptHTTP2:   true,
}

// sigaaHTTPClient segue redirects (comportamento padrão) e tem timeout,
// para que uma conexão pendurada com o SIGAA não trave o handler para sempre.
var sigaaHTTPClient = &http.Client{
	Transport: sharedTransport,
	Timeout:   45 * time.Second,
}

// sigaaNoRedirectClient é usado quando precisamos inspecionar o 302
// (ex.: download que o SIGAA tenta desviar para a tela de sessão expirada).
var sigaaNoRedirectClient = &http.Client{
	Transport: sharedTransport,
	Timeout:   45 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// externalHTTPClient é para serviços fora do SIGAA (ex.: calendário da PREG).
var externalHTTPClient = &http.Client{
	Transport: sharedTransport,
	Timeout:   30 * time.Second,
}

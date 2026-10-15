// hermetrix-tailnet serves an authenticated TLS gateway to a running loopback
// Hermetrix instance. It never opens a second store or runs background workers.
package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"hermetrix-harness/internal/web"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18811", "loopback TLS listener")
	upstream := flag.String("upstream", "http://127.0.0.1:18810", "running local Hermetrix")
	publicHost := flag.String("public-host", "", "exact tailnet DNS host and public port")
	cert := flag.String("tls-cert", "", "TLS certificate path")
	key := flag.String("tls-key", "", "TLS private key path")
	tokenEnv := flag.String("auth-token-env", "HERMETRIX_TAILNET_TOKEN", "environment variable with login token")
	principal := flag.String("auth-principal", "local-user", "owner represented by the shared login")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || *cert == "" || *key == "" {
		log.Fatal("gateway requires a literal loopback listener and TLS certificate/key")
	}
	handler, err := web.NewAuthenticatedLoopbackProxy(*upstream, *publicHost, os.Getenv(*tokenEnv), *principal)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 2 * time.Minute, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	log.Printf("authenticated tailnet gateway ready on %s for %s", *listen, *publicHost)
	log.Fatal(server.ListenAndServeTLS(*cert, *key))
}

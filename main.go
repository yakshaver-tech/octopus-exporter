package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr string
)

func main() {
	flag.StringVar(&addr, "listen-address", ":8080", "The address to listen on for HTTP requests.")
	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(addr, nil))
}

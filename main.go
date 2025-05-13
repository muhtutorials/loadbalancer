package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// Backend server
type Backend struct {
	URL          *url.URL
	Alive        bool
	ReverseProxy *httputil.ReverseProxy
	mu           sync.RWMutex
}

func (b *Backend) SetAlive(alive bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Alive = alive
}

func (b *Backend) IsAlive() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.Alive
}

type LoadBalancer struct {
	backends []*Backend
	current  int
	mu       sync.Mutex
}

// NextBackend returns the next available backend to handle the request
func (lb *LoadBalancer) NextBackend() *Backend {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	nBackends := len(lb.backends)
	next := (lb.current + 1) % nBackends
	for i := 0; i < nBackends; i++ {
		idx := (next + i) % nBackends
		if lb.backends[idx].IsAlive() {
			lb.current = idx
			return lb.backends[idx]
		}
	}
	return nil
}

func isBackendAlive(u *url.URL) bool {
	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		fmt.Printf("server is unreachable: %s\n", err)
		return false
	}
	defer conn.Close()
	return true
}

// HealthCheck pings the backends and updates their status
func (lb *LoadBalancer) HealthCheck() {
	for _, b := range lb.backends {
		status := isBackendAlive(b.URL)
		b.SetAlive(status)
		if status {
			fmt.Printf("server %s is alive\n", b.URL)
		} else {
			fmt.Printf("server %s is dead\n", b.URL)
		}
	}
}

// HealthCheckPeriodically runs a routine health check every interval
func (lb *LoadBalancer) HealthCheckPeriodically(interval time.Duration) {
	for range time.Tick(interval) {
		lb.HealthCheck()
	}
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	backend := lb.NextBackend()
	if backend == nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	// forward request
	backend.ReverseProxy.ServeHTTP(w, r)
}

func main() {
	port := 8000
	serverList := []string{
		"http://localhost:8001",
		"http://localhost:8002",
		"http://localhost:8003",
		"http://localhost:8004",
		"http://localhost:8005",
	}

	lb := new(LoadBalancer)

	for _, serverURL := range serverList {
		u, err := url.Parse(serverURL)
		if err != nil {
			log.Fatal(err)
		}

		proxy := httputil.NewSingleHostReverseProxy(u)
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			fmt.Println(err)
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		}

		lb.backends = append(lb.backends, &Backend{
			URL:          u,
			ReverseProxy: proxy,
		})
	}

	// initial health check
	lb.HealthCheck()

	// start periodic health check
	go lb.HealthCheckPeriodically(10 * time.Second)

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: lb,
	}
	fmt.Println("load balancer started on port:", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

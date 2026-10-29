package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"
)

// ready asks the running manager whether it works: whether it reaches the
// host agent and Proxmox. The upgrade on the host runs it before it keeps a
// new version, and before it trusts a restored one.
func ready() error {
	addr := os.Getenv("IGLO_HTTPS_ADDR")
	if addr == "" {
		addr = ":443"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1:"+port(addr)+"/api/ready", nil)
	if err != nil {
		return err
	}
	// The manager's own certificate is for its hostname, not for 127.0.0.1.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("the manager does not answer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the manager is not ready: %s", response.Status)
	}
	return nil
}

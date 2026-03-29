package consul

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

const defaultConsulAddr = "http://localhost:8500"

type serviceRegistration struct {
	ID      string       `json:"ID"`
	Name    string       `json:"Name"`
	Address string       `json:"Address"`
	Port    int          `json:"Port"`
	Check   serviceCheck `json:"Check"`
}

type serviceCheck struct {
	HTTP     string `json:"HTTP"`
	Interval string `json:"Interval"`
	Timeout  string `json:"Timeout"`
}

var (
	serviceID  string
	consulAddr string
)

// lanIP returns the first non-loopback IPv4 address.
func lanIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String(), nil
		}
	}
	return "", fmt.Errorf("no LAN IPv4 address found")
}

// Register registers the tp25 service with the local Consul agent.
// It auto-detects the LAN IP and uses localhost:8500 for the Consul agent.
func Register(port int) error {
	consulAddr = defaultConsulAddr

	ip, err := lanIP()
	if err != nil {
		return fmt.Errorf("consul: %w", err)
	}

	hostname, _ := os.Hostname()
	serviceID = fmt.Sprintf("tp25-%s", hostname)

	reg := serviceRegistration{
		ID:      serviceID,
		Name:    "tp25",
		Address: ip,
		Port:    port,
		Check: serviceCheck{
			HTTP:     fmt.Sprintf("http://%s:%d/healthz", ip, port),
			Interval: "10s",
			Timeout:  "2s",
		},
	}

	body, err := json.Marshal(reg)
	if err != nil {
		return fmt.Errorf("consul: marshal registration: %w", err)
	}

	url := consulAddr + "/v1/agent/service/register"
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("consul: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("consul: register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("consul: register returned %d", resp.StatusCode)
	}

	log.Printf("consul: registered as %q at %s:%d", serviceID, ip, port)
	return nil
}

// Deregister removes the service from the Consul agent.
func Deregister() {
	if serviceID == "" {
		return
	}

	url := fmt.Sprintf("%s/v1/agent/service/deregister/%s", consulAddr, serviceID)
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodPut, url, nil)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("consul: deregister failed: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("consul: deregistered %q", serviceID)
}

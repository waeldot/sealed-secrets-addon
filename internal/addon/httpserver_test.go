package addon

import (
	"bufio"
	"fmt"
	"net/http"
	"testing"
)

func TestProbing(t *testing.T) {
	RunHTTPServer()

	resp, err := http.Get("http://localhost:8081/healthz")
	if err != nil {
		t.Errorf("TEST FAILED: %v", err)
	}
	fmt.Println("Response status:", resp.Status)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Scan()
	respbody := scanner.Text()
	if respbody != "ok" {
		t.Errorf("TEST FAILED: %v", respbody)
	}
	fmt.Println(respbody)
}

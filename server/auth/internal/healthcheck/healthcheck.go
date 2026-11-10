// Package healthcheck implements the dependency-free HTTP probe command.
package healthcheck

import (
	"net/http"
	"time"
)

// Handle recognizes only the healthcheck subcommand. Call it before application
// configuration or startup. Exit codes match the former standalone probe:
// 0 for HTTP 200, 1 for HTTP/transport failure, 2 for invalid argument count.
func Handle(args []string) (handled bool, code int) {
	if len(args) == 0 || args[0] != "healthcheck" {
		return false, 0
	}
	if len(args) != 2 {
		return true, 2
	}
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Get(args[1])
	if err != nil {
		return true, 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return true, 1
	}
	return true, 0
}

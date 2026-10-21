package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	client := &http.Client{Timeout: 20 * time.Second}
	failed := false
	for _, host := range []string{"dv-g3b-emudhra.we-have-a-problem.com", "dv-g3b-aaa-emudhra.we-have-a-problem.com", "example.com"} {
		for attempt := 1; attempt <= 3; attempt++ {
			resp, err := client.Get("https://" + host)
			var certErr *tls.CertificateVerificationError
			pass := errors.As(err, &certErr)
			status := 0
			if resp != nil {
				status = resp.StatusCode
				resp.Body.Close()
			}
			if host == "example.com" {
				pass = err == nil && status == http.StatusOK
			}
			message := ""
			if err != nil {
				message = err.Error()
			}
			result, _ := json.Marshal(map[string]any{"attempt": attempt, "transport": "go/net/http", "host": host, "status": status, "error": message, "pass": pass})
			fmt.Println(string(result))
			if pass {
				break
			}
			if err == nil || certErr != nil || attempt == 3 {
				failed = true
				break
			}
		}
	}
	if failed {
		os.Exit(1)
	}
}

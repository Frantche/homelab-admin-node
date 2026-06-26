package validate

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type Status string

const (
	StatusOK      Status = "ok"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusSkipped Status = "skipped"
)

type CheckResult struct {
	Name     string        `json:"name"`
	Status   Status        `json:"status"`
	Message  string        `json:"message"`
	Duration time.Duration `json:"duration"`
}

func HasFailure(results []CheckResult) bool {
	for _, result := range results {
		if result.Status == StatusFail {
			return true
		}
	}
	return false
}

func WriteText(w io.Writer, results []CheckResult) {
	for _, result := range results {
		fmt.Fprintf(w, "%-9s %-7s %s\n", result.Name, strings.ToUpper(string(result.Status)), result.Message)
	}
}

func WriteJSON(w io.Writer, results []CheckResult) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func timed(name string, fn func() (Status, string)) CheckResult {
	start := time.Now()
	status, message := fn()
	return CheckResult{
		Name:     name,
		Status:   status,
		Message:  message,
		Duration: time.Since(start),
	}
}

package main

import (
	"bufio"
	"log"
	"os"
	"strings"

	"github.com/TimLai666/mcp-harness/internal/web"
)

func main() {
	loadDotEnv(".env")
	addr := os.Getenv("MCP_HARNESS_WEB_ADDR")
	if addr == "" {
		addr = ":8765"
	}
	if err := web.ListenAndServe(addr); err != nil {
		log.Fatal(err)
	}
}

// loadDotEnv loads KEY=VALUE lines from a .env file into the process
// environment for keys that are not already set. It is a convenience for local
// runs; in Docker the values come from compose's env_file. Missing file is fine.
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}

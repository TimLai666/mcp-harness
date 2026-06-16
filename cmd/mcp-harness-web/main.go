package main

import (
	"log"
	"os"

	"github.com/TimLai666/mcp-harness/internal/web"
)

func main() {
	addr := os.Getenv("MCP_HARNESS_WEB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8765"
	}
	if err := web.ListenAndServe(addr); err != nil {
		log.Fatal(err)
	}
}

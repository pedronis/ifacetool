package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("not enough arguments")
	}
	op := os.Args[1]
	param := json.RawMessage(os.Args[2])

	switch op {
	case "fetch-decls":
		return fetchDecls(&param)
	case "auto-connections":
		return autoConnections(&param)
	default:
		return fmt.Errorf("invalid engine op: %s", op)
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

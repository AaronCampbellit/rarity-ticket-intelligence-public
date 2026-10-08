package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/releasegate"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("rarity-release-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	evidencePath := flags.String("evidence", "", "release evidence JSON")
	revision := flags.String("revision", "", "expected immutable revision")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *evidencePath == "" || *revision == "" {
		fmt.Fprintln(stderr, "-evidence and -revision are required")
		return 2
	}
	file, err := os.Open(*evidencePath)
	if err != nil {
		fmt.Fprintf(stderr, "open evidence: %v\n", err)
		return 1
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var evidence releasegate.Evidence
	if err := decoder.Decode(&evidence); err != nil {
		fmt.Fprintf(stderr, "decode evidence: %v\n", err)
		return 1
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fmt.Fprintln(stderr, "decode evidence: expected exactly one JSON value")
		return 1
	}
	result, err := releasegate.Evaluate(evidence, *revision)
	if err != nil {
		fmt.Fprintf(stderr, "release gate failed: %v\n", err)
		return 1
	}
	if err := releasegate.ValidateFiles(evidence, *evidencePath); err != nil {
		fmt.Fprintf(stderr, "release evidence files failed: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	return 0
}

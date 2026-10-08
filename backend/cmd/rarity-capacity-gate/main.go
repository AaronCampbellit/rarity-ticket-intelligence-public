package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/capacity"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: rarity-capacity-gate <capacity-evidence.json>")
		os.Exit(2)
	}
	input, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "capacity evidence is unavailable")
		os.Exit(1)
	}
	defer input.Close()

	var evidence capacity.Evidence
	decoder := json.NewDecoder(io.LimitReader(input, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		fmt.Fprintln(os.Stderr, "capacity evidence is invalid")
		os.Exit(1)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, "capacity evidence must contain one JSON value")
		os.Exit(1)
	}

	report := capacity.Evaluate(evidence)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "capacity report could not be written")
		os.Exit(1)
	}
	if !report.Passed {
		os.Exit(1)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/setup"
)

type bootstrapTokenIssuer interface {
	IssueToken(context.Context) (string, time.Time, error)
}

func issueFirstRunSetup(
	ctx context.Context,
	completed bool,
	publicURL string,
	issuer bootstrapTokenIssuer,
	output io.Writer,
) error {
	if completed {
		return nil
	}
	if strings.TrimSpace(publicURL) == "" {
		return errors.New("RARITY_PUBLIC_URL is required before first-run setup")
	}
	if _, err := setup.BuildBootstrapURL(publicURL, "validation-token"); err != nil {
		return err
	}
	token, expiresAt, err := issuer.IssueToken(ctx)
	if err != nil {
		return err
	}
	setupURL, err := setup.BuildBootstrapURL(publicURL, token)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		output,
		"\nFIRST-RUN SETUP AUTHORITY (secret; expires %s)\n"+
			"Anyone holding this link can initialize this installation.\n%s\n\n",
		expiresAt.UTC().Format(time.RFC3339),
		setupURL,
	)
	return err
}

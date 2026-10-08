package setup

import (
	"errors"
	"net/url"
	"strings"
)

func BuildBootstrapURL(publicURL, token string) (string, error) {
	if strings.TrimSpace(token) == "" {
		return "", errors.New("bootstrap token is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid public URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
	parsed.RawPath = ""
	parsed.Fragment = "/setup?" + url.Values{
		"bootstrap_token": []string{token},
	}.Encode()
	return parsed.String(), nil
}

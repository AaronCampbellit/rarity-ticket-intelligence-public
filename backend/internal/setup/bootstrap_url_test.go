package setup

import "testing"

func TestBuildBootstrapURLUsesBrowserReachableOrigin(t *testing.T) {
	got, err := BuildBootstrapURL(
		"https://rarity.example",
		"bootstrap-secret",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://rarity.example/#/setup?bootstrap_token=bootstrap-secret"
	if got != want {
		t.Fatalf("URL=%q want=%q", got, want)
	}
}

func TestBuildBootstrapURLPreservesPublicPathPrefix(t *testing.T) {
	got, err := BuildBootstrapURL(
		"https://rarity.example/support/rti/",
		"bootstrap-secret",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://rarity.example/support/rti/#/setup?bootstrap_token=bootstrap-secret"
	if got != want {
		t.Fatalf("URL=%q want=%q", got, want)
	}
}

func TestBuildBootstrapURLRejectsUnsafeInputs(t *testing.T) {
	for name, values := range map[string][2]string{
		"missing token": {
			"https://rarity.example", "",
		},
		"relative URL": {
			"/rarity", "bootstrap-secret",
		},
		"user information": {
			"https://operator@rarity.example", "bootstrap-secret",
		},
		"query": {
			"https://rarity.example?source=console", "bootstrap-secret",
		},
		"fragment": {
			"https://rarity.example/#/other", "bootstrap-secret",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildBootstrapURL(values[0], values[1]); err == nil {
				t.Fatalf("BuildBootstrapURL(%q, %q) accepted unsafe input", values[0], values[1])
			}
		})
	}
}

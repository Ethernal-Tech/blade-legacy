package accounts

import "testing"

const (
	bladeURL           = "blade.org"
	bladeURLWithPrefix = "https://blade.org"
	bladeURLJSON       = "\"https://blade.org\""
)

func TestURLParsing(t *testing.T) {
	t.Parallel()

	url, err := parseURL(bladeURLWithPrefix)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if url.Scheme != "https" {
		t.Errorf("expected: %v, got: %v", "https", url.Scheme)
	}

	if url.Path != bladeURL {
		t.Errorf("expected: %v, got: %v", bladeURL, url.Path)
	}

	for _, u := range []string{bladeURL, ""} {
		if _, err = parseURL(u); err == nil {
			t.Errorf("input %v, expected err, got: nil", u)
		}
	}
}

func TestURLString(t *testing.T) {
	t.Parallel()

	url := URL{Scheme: "https", Path: bladeURL}

	if url.String() != bladeURLWithPrefix {
		t.Errorf("expected: %v, got: %v", bladeURLWithPrefix, url.String())
	}

	url = URL{Scheme: "", Path: bladeURL}

	if url.String() != bladeURL {
		t.Errorf("expected: %v, got: %v", bladeURL, url.String())
	}
}

func TestURLMarshalJSON(t *testing.T) {
	t.Parallel()

	url := URL{Scheme: "https", Path: bladeURL}

	json, err := url.MarshalJSON()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if string(json) != bladeURLJSON {
		t.Errorf("expected: %v, got: %v", bladeURLJSON, string(json))
	}
}

func TestURLUnmarshalJSON(t *testing.T) {
	t.Parallel()

	url := &URL{}

	err := url.UnmarshalJSON([]byte(bladeURLJSON))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if url.Scheme != "https" {
		t.Errorf("expected: %v, got: %v", "https", url.Scheme)
	}

	if url.Path != bladeURL {
		t.Errorf("expected: %v, got: %v", "https", url.Path)
	}
}

func TestURLComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		urlA   URL
		urlB   URL
		expect int
	}{
		{URL{"https", bladeURL}, URL{"https", bladeURL}, 0},
		{URL{"http", bladeURL}, URL{"https", bladeURL}, -1},
		{URL{"https", bladeURL + "/a"}, URL{"https", bladeURL}, 1},
		{URL{"https", "abc.org"}, URL{"https", bladeURL}, -1},
	}

	for i, tt := range tests {
		result := tt.urlA.Cmp(tt.urlB)

		if result != tt.expect {
			t.Errorf("test %d: cmp mismatch: expected: %d, got: %d", i, tt.expect, result)
		}
	}
}

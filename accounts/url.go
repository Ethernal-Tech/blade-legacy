package accounts

import (
	"encoding/json"
	"errors"
	"strings"
)

type URL struct {
	Scheme string
	Path   string
}

func parseURL(url string) (URL, error) {
	urlParts := strings.Split(url, "://")
	if len(urlParts) != 2 || urlParts[0] == "" {
		return URL{}, errors.New("protocol scheme missing")
	}

	return URL{
		Scheme: urlParts[0],
		Path:   urlParts[1],
	}, nil
}

func (u URL) String() string {
	if u.Scheme != "" {
		return u.Scheme + "://" + u.Path
	}

	return u.Path
}

func (u URL) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

func (u *URL) UnmarshalJSON(input []byte) error {
	var textURL string

	err := json.Unmarshal(input, &textURL)

	if err != nil {
		return err
	}

	url, err := parseURL(textURL)

	if err != nil {
		return err
	}

	u.Scheme = url.Scheme
	u.Path = url.Path

	return nil
}

func (u URL) Cmp(url URL) int {
	if u.Scheme == url.Scheme {
		return strings.Compare(u.Path, url.Path)
	}

	return strings.Compare(u.Scheme, url.Scheme)
}

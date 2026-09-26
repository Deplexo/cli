package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Deplexo/cli/internal/version"
)

const repository = "https://github.com/Deplexo/cli"

type Client struct {
	http *http.Client
}

func New(transport http.RoundTripper) *Client {
	return &Client{http: &http.Client{Transport: transport, Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("untrusted release redirect")
		}
		switch req.URL.Host {
		case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			return nil
		}
		return errors.New("untrusted release redirect")
	}}}
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "deplexo-cli")
	return c.http.Do(req)
}

func (c *Client) metadata(ctx context.Context, path string, value any) (int, error) {
	resp, err := c.get(ctx, "https://api.github.com/repos/Deplexo/cli/releases"+path)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, errors.New("could not check releases; try again later")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, errors.New("release information is unavailable; try again later")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil || len(data) > 2<<20 || json.Unmarshal(data, value) != nil {
		if ctx.Err() != nil {
			return resp.StatusCode, ctx.Err()
		}
		return resp.StatusCode, errors.New("release information has an unexpected format")
	}
	return resp.StatusCode, nil
}

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Latest prefers a stable release. Until one exists, it selects a published beta.
func (c *Client) Latest(ctx context.Context) (string, error) {
	var stable release
	status, err := c.metadata(ctx, "/latest", &stable)
	if err == nil {
		if !version.ValidTag(stable.Tag) || strings.Contains(stable.Tag, "-") || stable.Draft || stable.Prerelease {
			return "", errors.New("GitHub returned an invalid stable release")
		}
		return stable.Tag, nil
	}
	if status != http.StatusNotFound {
		return "", err
	}
	latest := ""
	for page := 1; ; page++ {
		if page > 100 {
			return "", errors.New("release history exceeds the lookup limit")
		}
		var releases []release
		if _, err := c.metadata(ctx, "?per_page=100&page="+strconv.Itoa(page), &releases); err != nil {
			return "", err
		}
		for _, item := range releases {
			if !item.Draft && item.Prerelease && version.BetaTag(item.Tag) && (latest == "" || version.Newer(item.Tag, latest)) {
				latest = item.Tag
			}
		}
		if len(releases) < 100 {
			break
		}
	}
	if latest == "" {
		return "", errors.New("no downloadable release is available")
	}
	return latest, nil
}

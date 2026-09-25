package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const ClientID = "deplexo-cli"

type Discovery struct {
	Issuer             string   `json:"issuer"`
	DeviceEndpoint     string   `json:"device_authorization_endpoint"`
	TokenEndpoint      string   `json:"token_endpoint"`
	RevocationEndpoint string   `json:"revocation_endpoint"`
	Scopes             []string `json:"scopes_supported"`
}

func (c *Client) Discover(ctx context.Context) (Discovery, error) {
	var d Discovery
	err := c.request(ctx, http.MethodGet, c.origin+"/.well-known/oauth-authorization-server", "", "", nil, &d)
	if err != nil {
		return d, err
	}
	if d.Issuer != c.origin {
		return d, errors.New("OAuth issuer does not match the selected API origin")
	}
	for _, endpoint := range []string{d.DeviceEndpoint, d.TokenEndpoint, d.RevocationEndpoint} {
		if err := c.ValidateURL(endpoint); err != nil {
			return d, err
		}
		u, _ := url.Parse(endpoint)
		if u.RawQuery != "" || u.ForceQuery {
			return d, errors.New("OAuth endpoint must not contain a query")
		}
	}
	return d, nil
}

type Device struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (c *Client) form(ctx context.Context, endpoint string, form url.Values, out any) error {
	form.Set("client_id", ClientID)
	return c.request(ctx, http.MethodPost, endpoint, "", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), out)
}

func (c *Client) Authorize(ctx context.Context, d Discovery, scopes string) (Device, error) {
	var device Device
	err := c.form(ctx, d.DeviceEndpoint, url.Values{"scope": {scopes}}, &device)
	if err != nil {
		return device, err
	}
	if !ValidToken(device.DeviceCode) || !ValidToken(device.UserCode) || device.ExpiresIn <= 0 || device.ExpiresIn > 86400 || device.Interval < 0 || device.Interval > 86400 {
		return device, errors.New("server returned an invalid pairing response")
	}
	if err := c.ValidateURL(device.VerificationURI); err != nil {
		return device, err
	}
	u, _ := url.Parse(device.VerificationURI)
	if u.Path != "/new-devices" || u.RawQuery != "" || u.ForceQuery {
		return device, errors.New("server returned an invalid verification URL")
	}
	if device.VerificationURIComplete != "" {
		if err := c.ValidateURL(device.VerificationURIComplete); err != nil {
			return device, err
		}
		u, _ := url.Parse(device.VerificationURIComplete)
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || u.Path != "/new-devices" || len(query) != 1 || len(query["user_code"]) != 1 || query.Get("user_code") != device.UserCode {
			return device, errors.New("server returned an invalid prefilled verification URL")
		}
	}
	return device, nil
}

func (c *Client) Exchange(ctx context.Context, d Discovery, grant, credential string) (Tokens, error) {
	form := url.Values{"grant_type": {grant}}
	if grant == "refresh_token" {
		form.Set("refresh_token", credential)
	} else {
		form.Set("device_code", credential)
	}
	var tokens Tokens
	err := c.form(ctx, d.TokenEndpoint, form, &tokens)
	return tokens, err
}

func (c *Client) Revoke(ctx context.Context, d Discovery, token string) error {
	return c.form(ctx, d.RevocationEndpoint, url.Values{"token": {token}, "token_type_hint": {"refresh_token"}}, nil)
}

type Profile struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (c *Client) Profile(ctx context.Context, token string) (Profile, error) {
	var profile Profile
	err := c.get(ctx, "/profile", token, &profile)
	if err == nil && (profile.ID == "" || profile.Email == "") {
		err = errors.New("API returned an incomplete account profile")
	}
	return profile, err
}

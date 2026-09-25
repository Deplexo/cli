package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/Deplexo/cli/internal/api"
	"github.com/Deplexo/cli/internal/credentials"
	"github.com/Deplexo/cli/internal/output"
)

type Store interface {
	WithLock(context.Context, func(credentials.Vault) error) error
}

type Manager struct {
	API         *api.Client
	Store       Store
	Profile     string
	TokenEnv    string
	TokenEnvSet bool
	wait        func(context.Context, time.Duration) error
}

func Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *Manager) sleep(ctx context.Context, duration time.Duration) error {
	if m.wait != nil {
		return m.wait(ctx, duration)
	}
	return Sleep(ctx, duration)
}

func Scopes(raw string, readOnly bool) ([]string, error) {
	if raw != "" && readOnly {
		return nil, output.Usage("--scopes and --read-only cannot be used together")
	}
	if raw == "" {
		raw = "profile:read app:read logs:read"
		if !readOnly {
			raw += " app:deploy"
		}
	}
	if len(raw) > 1024 {
		return nil, output.Usage("requested scopes are too long")
	}
	values := strings.Fields(strings.ReplaceAll(raw, ",", " "))
	slices.Sort(values)
	values = slices.Compact(values)
	if !slices.Contains(values, "profile:read") {
		return nil, output.Usage("stored sign-ins require profile:read in --scopes")
	}
	return values, nil
}

func tokenScopes(t api.Tokens, requested []string) ([]string, error) {
	if !api.ValidToken(t.AccessToken) || !api.ValidToken(t.RefreshToken) || !strings.EqualFold(t.TokenType, "Bearer") || t.ExpiresIn <= 0 || t.ExpiresIn > 86400*365 {
		return nil, errors.New("server returned an invalid token response; sign in again")
	}
	got := strings.Fields(t.Scope)
	slices.Sort(got)
	got = slices.Compact(got)
	want := slices.Clone(requested)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return nil, output.Permission("the granted scopes do not match the requested scopes; sign in again")
	}
	return got, nil
}

func (m *Manager) Token(ctx context.Context, required ...string) (string, error) {
	if m.TokenEnvSet {
		if !api.ValidToken(m.TokenEnv) {
			return "", output.SignIn("DEPLEXO_TOKEN is empty or invalid; set a valid token or unset it before signing in")
		}
		return m.TokenEnv, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var token string
	err := m.Store.WithLock(ctx, func(v credentials.Vault) error {
		s, err := v.Load()
		if errors.Is(err, credentials.ErrNotFound) {
			return output.SignIn("sign-in required; run `deplexo auth login`")
		}
		if err != nil {
			return err
		}
		if s.Origin != m.API.Origin() || s.Profile != m.Profile || s.AccountID == "" || s.Pending || !api.ValidToken(s.AccessToken) || !api.ValidToken(s.RefreshToken) || !slices.Contains(s.Scopes, "profile:read") {
			return output.SignIn("stored sign-in is unusable; run `deplexo auth login`")
		}
		for _, scope := range required {
			if !slices.Contains(s.Scopes, scope) {
				return output.Permission("this operation requires " + scope + "; sign in with that scope")
			}
		}
		if time.Until(s.ExpiresAt) < 30*time.Second {
			discovery, err := m.API.Discover(ctx)
			if err != nil {
				return err
			}
			refresh := s.RefreshToken
			// Erase the single-use credential before the request can reach the server.
			s.Pending, s.AccessToken, s.RefreshToken = true, "", ""
			if err := v.Save(s); err != nil {
				return err
			}
			t, err := m.API.Exchange(ctx, discovery, "refresh_token", refresh)
			if err != nil {
				return errors.Join(output.SignIn("could not refresh the sign-in; run `deplexo auth login`"), ctx.Err())
			}
			return m.accept(ctx, v, discovery, t, s.Scopes, s.AccountID, func(saved credentials.Session) { token = saved.AccessToken })
		}
		token = s.AccessToken
		return nil
	})
	return token, err
}

func (m *Manager) cleanup(d api.Discovery, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !api.ValidToken(token) {
		return errors.New("new credentials could not be revoked; remove the CLI device in account settings")
	}
	if err := m.API.Revoke(ctx, d, token); err != nil {
		return errors.New("new credentials could not be revoked; remove the CLI device in account settings")
	}
	return nil
}

func (m *Manager) accept(ctx context.Context, v credentials.Vault, d api.Discovery, t api.Tokens, requested []string, accountID string, saved func(credentials.Session)) (result error) {
	defer func() {
		if result != nil {
			result = errors.Join(result, m.cleanup(d, t.RefreshToken))
		}
	}()
	scopes, err := tokenScopes(t, requested)
	if err != nil {
		return err
	}
	expires := time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	p, err := m.API.Profile(ctx, t.AccessToken)
	if err != nil {
		return err
	}
	if accountID != "" && p.ID != accountID {
		return errors.New("refreshed sign-in belongs to a different account; sign in again")
	}
	s := credentials.Session{Version: 1, Origin: m.API.Origin(), Profile: m.Profile, AccountID: p.ID, Email: p.Email, AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, Scopes: scopes, ExpiresAt: expires}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := v.Save(s); err != nil {
		return fmt.Errorf("could not persist the sign-in: %w", err)
	}
	saved(s)
	return nil
}

// Login holds the same mutation lock as refresh and logout through approval and persistence.
func (m *Manager) Login(ctx context.Context, scopes []string, pairing func(api.Device) error) (api.Profile, error) {
	if m.TokenEnvSet {
		return api.Profile{}, output.Usage("unset DEPLEXO_TOKEN before signing in")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	var profile api.Profile
	err := m.Store.WithLock(ctx, func(v credentials.Vault) error {
		previous, err := v.Load()
		if err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return err
		}
		d, err := m.API.Discover(ctx)
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if !slices.Contains(d.Scopes, scope) {
				return output.Usage("the server does not support a requested scope")
			}
		}
		device, err := m.API.Authorize(ctx, d, strings.Join(scopes, " "))
		if err != nil {
			return err
		}
		pollCtx, pollCancel := context.WithTimeout(ctx, time.Duration(device.ExpiresIn)*time.Second)
		defer pollCancel()
		if err := pairing(device); err != nil {
			return err
		}
		t, err := m.poll(pollCtx, d, device)
		if err != nil {
			return err
		}
		if err := m.accept(ctx, v, d, t, scopes, "", func(s credentials.Session) { profile = api.Profile{ID: s.AccountID, Email: s.Email} }); err != nil {
			return err
		}
		if previous.RefreshToken != "" && previous.RefreshToken != t.RefreshToken {
			if previous.Origin != m.API.Origin() || previous.Profile != m.Profile {
				return errors.New("new sign-in saved; the previous record belonged to another origin or profile, so its server session was not revoked")
			}
			if err := m.cleanup(d, previous.RefreshToken); err != nil {
				return errors.New("new sign-in saved, but the previous session could not be revoked; remove the old CLI device in account settings")
			}
		}
		return nil
	})
	return profile, err
}

func (m *Manager) poll(ctx context.Context, d api.Discovery, device api.Device) (api.Tokens, error) {
	interval := time.Duration(device.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	delay := interval
	for {
		if err := m.sleep(ctx, delay); err != nil {
			return api.Tokens{}, err
		}
		t, err := m.API.Exchange(ctx, d, "urn:ietf:params:oauth:grant-type:device_code", device.DeviceCode)
		if err == nil {
			return t, nil
		}
		if ctx.Err() != nil {
			return api.Tokens{}, ctx.Err()
		}
		var remote *api.Error
		var timeout net.Error
		switch {
		case errors.As(err, &remote) && (remote.Code == "authorization_pending" || remote.Code == "slow_down"):
			if remote.Code == "slow_down" {
				interval += 5 * time.Second
			}
			delay = max(interval, remote.RetryAfter)
		case errors.As(err, &timeout) && timeout.Timeout():
			interval = max(interval, min(interval*2, 5*time.Minute))
			delay = interval
		default:
			return api.Tokens{}, errors.New("pairing did not complete; run `deplexo auth login` to try again")
		}
	}
}

func (m *Manager) Logout(ctx context.Context) error {
	if m.TokenEnvSet {
		return output.Usage("DEPLEXO_TOKEN is supplied by your environment; unset it to stop using it")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return m.Store.WithLock(ctx, func(v credentials.Vault) error {
		s, err := v.Load()
		if errors.Is(err, credentials.ErrNotFound) {
			return nil
		}
		if errors.Is(err, credentials.ErrMalformed) {
			if err := v.Delete(); err != nil {
				return err
			}
			return errors.New("invalid local sign-in removed; remove the CLI device in account settings to revoke its server session")
		}
		if err != nil {
			return err
		}
		var remoteErr error
		if s.Origin != m.API.Origin() || s.Profile != m.Profile {
			if err := v.Delete(); err != nil {
				return err
			}
			return errors.New("local sign-in removed; it belonged to another origin or profile, so its server session was not revoked")
		}
		if s.RefreshToken != "" {
			d, err := m.API.Discover(ctx)
			if err != nil {
				remoteErr = err
			} else {
				remoteErr = m.API.Revoke(ctx, d, s.RefreshToken)
			}
		} else if s.Pending {
			remoteErr = errors.New("refresh outcome is unknown")
		}
		if err := v.Delete(); err != nil {
			return err
		}
		if remoteErr != nil {
			return errors.New("local sign-in removed, but the server session may remain; remove the CLI device in account settings")
		}
		return nil
	})
}

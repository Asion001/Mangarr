package settings

import "context"

// KeySSO holds single sign-on (OpenID Connect) settings.
const KeySSO = "sso"

// SSO configures signing in with an OpenID Connect provider (Authentik,
// Authelia, Keycloak, Google, ...).
type SSO struct {
	Enabled      bool     `json:"enabled"`
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret"`
	Scopes       []string `json:"scopes"`
	// ButtonLabel is shown on the login page ("Sign in with Authentik").
	ButtonLabel string `json:"buttonLabel"`
	// UsernameClaim names new accounts (preferred_username, email, ...).
	UsernameClaim string `json:"usernameClaim"`
	// GroupsClaim lists the provider's groups for GroupMappings.
	GroupsClaim string `json:"groupsClaim"`
	// Groups maps provider groups to mangarr groups (the first match wins);
	// members' groups follow them at every sign-in.
	Groups []SSOGroup `json:"groups"`
	// OnlyMapped turns away people in none of the mapped groups.
	OnlyMapped bool `json:"onlyMapped"`
	// CreateUsers makes an account on first sign-in (in the matched group,
	// or Users); otherwise people need an invite.
	CreateUsers bool `json:"createUsers"`
	// LinkByUsername signs in to an existing account with the same username
	// (only if the provider controls usernames: anyone who can pick one there
	// could take over the account here).
	LinkByUsername bool `json:"linkByUsername"`
	// PasswordLogin keeps passwords working for everyone; off, only
	// administrators may still use theirs (to get back in if the provider
	// is down).
	PasswordLogin bool `json:"passwordLogin"`
}

// SSOGroup maps a provider group to a mangarr group.
type SSOGroup struct {
	Claim   string `json:"claim"`
	GroupID int64  `json:"groupId"`
}

func DefaultSSO() SSO {
	return SSO{Scopes: []string{"openid", "profile", "email"}, ButtonLabel: "Sign in with SSO", UsernameClaim: "preferred_username",
		GroupsClaim: "groups", PasswordLogin: true, Groups: []SSOGroup{}}
}

func (s *Store) SSO(ctx context.Context) (SSO, error) {
	v := DefaultSSO()
	return v, s.Get(ctx, KeySSO, &v)
}

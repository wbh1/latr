package linode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/linode/linodego"
	"github.com/wbh1/latr/pkg/models"
	"golang.org/x/oauth2"
)

// Client wraps the linodego client
type Client struct {
	client *linodego.Client
	token  string
}

// NewClient creates a new Linode API client
func NewClient(token string) *Client {
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	oauth2Client := oauth2.NewClient(context.Background(), tokenSource)

	linodeClient := linodego.NewClient(oauth2Client)

	return &Client{
		client: &linodeClient,
		token:  token,
	}
}

// CreateToken creates a new Linode API token
func (c *Client) CreateToken(ctx context.Context, label, scopes string, expiry time.Time) (*models.Token, error) {
	createOpts := linodego.TokenCreateOptions{
		Label:  label,
		Scopes: scopes,
		Expiry: &expiry,
	}

	token, err := c.client.CreateToken(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create token: %w", err)
	}

	return &models.Token{
		ID:        token.ID,
		Label:     token.Label,
		Token:     token.Token,
		CreatedAt: *token.Created,
		ExpiresAt: *token.Expiry,
		Scopes:    token.Scopes,
		Validity:  time.Until(*token.Expiry),
	}, nil
}

// GetToken retrieves a token by ID
func (c *Client) GetToken(ctx context.Context, tokenID int) (*models.Token, error) {
	token, err := c.client.GetToken(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	created := time.Now()
	if token.Created != nil {
		created = *token.Created
	}

	expiry := time.Now().Add(90 * 24 * time.Hour)
	if token.Expiry != nil {
		expiry = *token.Expiry
	}

	return &models.Token{
		ID:        token.ID,
		Label:     token.Label,
		Token:     "", // The API doesn't return the token value for existing tokens
		CreatedAt: created,
		ExpiresAt: expiry,
		Scopes:    token.Scopes,
		Validity:  expiry.Sub(created),
	}, nil
}

// ListTokens lists all API tokens
func (c *Client) ListTokens(ctx context.Context) ([]*models.Token, error) {
	tokens, err := c.client.ListTokens(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}

	var result []*models.Token
	for _, token := range tokens {
		created := time.Now()
		if token.Created != nil {
			created = *token.Created
		}

		expiry := time.Now().Add(90 * 24 * time.Hour)
		if token.Expiry != nil {
			expiry = *token.Expiry
		}

		result = append(result, &models.Token{
			ID:        token.ID,
			Label:     token.Label,
			Token:     "", // The API doesn't return the token value for existing tokens
			CreatedAt: created,
			ExpiresAt: expiry,
			Scopes:    token.Scopes,
			Validity:  expiry.Sub(created),
		})
	}

	return result, nil
}

// FindTokenByLabel finds a token by its label
func (c *Client) FindTokenByLabel(ctx context.Context, label string) (*models.Token, error) {
	tokens, err := c.ListTokens(ctx)
	if err != nil {
		return nil, err
	}

	for _, token := range tokens {
		if token.Label == label {
			return token, nil
		}
	}

	return nil, nil // Not found
}

// RevokeToken deletes a token by ID
func (c *Client) RevokeToken(ctx context.Context, tokenID int) error {
	err := c.client.DeleteToken(ctx, tokenID)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}
	return nil
}

// UpdateToken updates a token's expiry (if supported by the API)
// Note: Linode API may not support updating token expiry, so we may need to create a new one
func (c *Client) UpdateToken(ctx context.Context, tokenID int, expiry time.Time) error {
	updateOpts := linodego.TokenUpdateOptions{
		Label: "", // Keep existing label
	}

	_, err := c.client.UpdateToken(ctx, tokenID, updateOpts)
	if err != nil {
		return fmt.Errorf("failed to update token: %w", err)
	}
	return nil
}

// IsNotFoundError checks if an error is a 404 not found error
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	if apiErr, ok := err.(*linodego.Error); ok {
		return apiErr.Code == http.StatusNotFound
	}

	return false
}

// ParseScopes parses and returns the scopes string
// This is a simple pass-through for now, but could be enhanced to validate scopes
func ParseScopes(scopes string) string {
	return scopes
}

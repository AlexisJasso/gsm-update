package secretmanager

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// operationTimeout bounds each Secret Manager operation.
const operationTimeout = 30 * time.Second

// Service is the contract that the rest of the application uses to interact
// with Google Secret Manager. The UI depends on this interface (rather than
// on *Client) so that tests can substitute a fake implementation.
type Service interface {
	// ListSecrets returns the short names of all secrets in the project.
	ListSecrets(projectID string) ([]string, error)
	// GetSecretVersion returns the payload and the resource name of the most
	// recently created enabled version of a secret. It returns a "no enabled
	// version" error if the secret has no enabled version.
	GetSecretVersion(projectID, secretName string) (payload, version string, err error)
	// CreateSecretVersion adds a new version of a secret and disables all
	// other enabled versions of that secret.
	CreateSecretVersion(projectID, secretName, payload string) error
	// Close releases any resources held by the service.
	Close() error
}

// secretVersion is a Secret Manager version name with its lifecycle state.
type secretVersion struct {
	name    string
	enabled bool
}

// apiClient is the set of primitive GCP Secret Manager operations this
// package needs. It is unexported so that only this package can implement
// it; tests use it to exercise the orchestration logic without GCP.
type apiClient interface {
	listSecrets(ctx context.Context, parent string) ([]string, error)
	accessSecretVersion(ctx context.Context, name string) ([]byte, error)
	addSecretVersion(ctx context.Context, parent string, data []byte) (string, error)
	listSecretVersions(ctx context.Context, parent string) ([]secretVersion, error)
	disableSecretVersion(ctx context.Context, name string) error
}

// Client implements Service on top of the GCP Secret Manager API.
type Client struct {
	api apiClient
	ctx context.Context
}

var _ Service = (*Client)(nil)

// NewClient creates a Client connected to GCP using application default
// credentials.
func NewClient(ctx context.Context) (*Client, error) {
	gcp, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	return &Client{
		api: &gcpAPI{client: gcp},
		ctx: ctx,
	}, nil
}

// newClientWithAPI creates a Client backed by the provided apiClient. It is
// intended for tests.
func newClientWithAPI(api apiClient, ctx context.Context) *Client {
	return &Client{api: api, ctx: ctx}
}

// Close closes the underlying connection, if any.
func (c *Client) Close() error {
	if closer, ok := c.api.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// ListSecrets lists the short names of all secrets in a project.
func (c *Client) ListSecrets(projectID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(c.ctx, operationTimeout)
	defer cancel()

	names, err := c.api.listSecrets(ctx, "projects/"+projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}
	return names, nil
}

// GetSecretVersion retrieves the payload of the most recently created enabled
// version of a secret. It lists the secret's versions, picks the enabled one
// with the highest ordinal, and accesses it. If the secret has no enabled
// version it returns a "no enabled version" error. The version's resource
// name is returned so the caller can report which version a payload belongs
// to (e.g. when the payload is binary and cannot be edited).
func (c *Client) GetSecretVersion(projectID, secretName string) (string, string, error) {
	ctx, cancel := context.WithTimeout(c.ctx, operationTimeout)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/secrets/%s", projectID, secretName)

	versions, err := c.api.listSecretVersions(ctx, parent)
	if err != nil {
		return "", "", fmt.Errorf("failed to list secret versions: %w", err)
	}

	latest := ""
	latestNum := -1
	for _, v := range versions {
		if !v.enabled {
			continue
		}
		n, ok := versionNumber(v.name)
		if !ok {
			continue
		}
		if n > latestNum {
			latestNum = n
			latest = v.name
		}
	}

	if latest == "" {
		return "", "", fmt.Errorf("secret %q has no enabled version", secretName)
	}

	data, err := c.api.accessSecretVersion(ctx, latest)
	if err != nil {
		return "", "", fmt.Errorf("failed to access secret version: %w", err)
	}
	return string(data), latest, nil
}

// versionNumber extracts the numeric version ordinal from a version resource
// name, e.g. "projects/p/secrets/s/versions/3" -> 3. It reports false if the
// name has no numeric ordinal (e.g. "versions/latest").
func versionNumber(name string) (int, bool) {
	n, err := strconv.Atoi(baseName(name))
	if err != nil {
		return 0, false
	}
	return n, true
}

// CreateSecretVersion creates a new version of a secret and disables all
// other enabled versions of that secret; versions that are already disabled
// are left as-is. If disabling fails partway through, the new version is
// already in place and the error is returned so the caller can inspect the
// secret's state.
func (c *Client) CreateSecretVersion(projectID, secretName, payload string) error {
	ctx, cancel := context.WithTimeout(c.ctx, operationTimeout)
	defer cancel()

	name := fmt.Sprintf("projects/%s/secrets/%s", projectID, secretName)

	newVersion, err := c.api.addSecretVersion(ctx, name, []byte(payload))
	if err != nil {
		return fmt.Errorf("failed to create secret version: %w", err)
	}

	versions, err := c.api.listSecretVersions(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to list secret versions: %w", err)
	}

	for _, version := range versions {
		if version.name == newVersion || !version.enabled {
			continue
		}
		if err := c.api.disableSecretVersion(ctx, version.name); err != nil {
			return fmt.Errorf("failed to disable secret version %s: %w", version.name, err)
		}
	}

	return nil
}

// gcpAPI adapts the real GCP Secret Manager client to the apiClient
// interface.
type gcpAPI struct {
	client *secretmanager.Client
}

// Close closes the underlying GCP client connection.
func (a *gcpAPI) Close() error {
	return a.client.Close()
}

func (a *gcpAPI) listSecrets(ctx context.Context, parent string) ([]string, error) {
	var names []string
	it := a.client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{Parent: parent})
	for secret, err := range it.All() {
		if err != nil {
			return nil, err
		}
		names = append(names, baseName(secret.GetName()))
	}
	return names, nil
}

func (a *gcpAPI) accessSecretVersion(ctx context.Context, name string) ([]byte, error) {
	resp, err := a.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: name})
	if err != nil {
		return nil, err
	}
	return resp.Payload.Data, nil
}

func (a *gcpAPI) addSecretVersion(ctx context.Context, parent string, data []byte) (string, error) {
	resp, err := a.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  parent,
		Payload: &secretmanagerpb.SecretPayload{Data: data},
	})
	if err != nil {
		return "", err
	}
	return resp.GetName(), nil
}

func (a *gcpAPI) listSecretVersions(ctx context.Context, parent string) ([]secretVersion, error) {
	var versions []secretVersion
	it := a.client.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{Parent: parent})
	for version, err := range it.All() {
		if err != nil {
			return nil, err
		}
		versions = append(versions, secretVersion{
			name:    version.GetName(),
			enabled: version.GetState() == secretmanagerpb.SecretVersion_ENABLED,
		})
	}
	return versions, nil
}

func (a *gcpAPI) disableSecretVersion(ctx context.Context, name string) error {
	_, err := a.client.DisableSecretVersion(ctx, &secretmanagerpb.DisableSecretVersionRequest{Name: name})
	return err
}

// baseName returns the last path segment of a GCP resource name, e.g.
// "projects/p/secrets/s" -> "s".
func baseName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

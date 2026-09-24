package secretmanager

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	// NewClient requires a valid GCP context and credentials.
	// We test that it returns an error when no credentials are available,
	// rather than panicking or returning a non-nil client.
	ctx := &testContext{}
	client, err := NewClient(ctx)
	if client != nil {
		t.Error("NewClient() with invalid context should return nil client")
	}
	if err == nil {
		t.Error("NewClient() with invalid context should return an error")
	}
}

// fakeAPI implements apiClient and records the calls it receives so tests can
// assert on the orchestration performed by Client.
type fakeAPI struct {
	listedNames     []string
	listErr         error
	payload         []byte
	accessErr       error
	accessedName    string
	addedPayload    []byte
	newVersionName  string
	addErr          error
	versions        []secretVersion
	listVersionsErr error
	disabled        []string
	disableErrs     map[string]error

	// ctxs records the context of every call, in order.
	ctxs []context.Context
}

func (f *fakeAPI) record(ctx context.Context) {
	f.ctxs = append(f.ctxs, ctx)
}

func (f *fakeAPI) listSecrets(ctx context.Context, _ string) ([]string, error) {
	f.record(ctx)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listedNames, nil
}

func (f *fakeAPI) accessSecretVersion(ctx context.Context, name string) ([]byte, error) {
	f.record(ctx)
	if f.accessErr != nil {
		return nil, f.accessErr
	}
	f.accessedName = name
	return f.payload, nil
}

func (f *fakeAPI) addSecretVersion(ctx context.Context, _ string, data []byte) (string, error) {
	f.record(ctx)
	if f.addErr != nil {
		return "", f.addErr
	}
	f.addedPayload = data
	return f.newVersionName, nil
}

func (f *fakeAPI) listSecretVersions(ctx context.Context, _ string) ([]secretVersion, error) {
	f.record(ctx)
	if f.listVersionsErr != nil {
		return nil, f.listVersionsErr
	}
	return f.versions, nil
}

func (f *fakeAPI) disableSecretVersion(ctx context.Context, name string) error {
	f.record(ctx)
	f.disabled = append(f.disabled, name)
	return f.disableErrs[name]
}

func TestListSecrets(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		api := &fakeAPI{listedNames: []string{"a", "b"}}
		c := newClientWithAPI(api, context.Background())
		got, err := c.ListSecrets("p")
		if err != nil {
			t.Fatalf("ListSecrets() unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("ListSecrets() = %v, want [a b]", got)
		}
	})

	t.Run("error", func(t *testing.T) {
		api := &fakeAPI{listErr: errors.New("boom")}
		c := newClientWithAPI(api, context.Background())
		if _, err := c.ListSecrets("p"); err == nil {
			t.Error("ListSecrets() expected error")
		}
	})
}

func TestGetSecretVersion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		api := &fakeAPI{
			payload:  []byte("hello"),
			versions: []secretVersion{{name: "projects/p/secrets/s/versions/1", enabled: true}},
		}
		c := newClientWithAPI(api, context.Background())
		got, version, err := c.GetSecretVersion("p", "s")
		if err != nil {
			t.Fatalf("GetSecretVersion() unexpected error: %v", err)
		}
		if got != "hello" {
			t.Errorf("GetSecretVersion() payload = %q, want %q", got, "hello")
		}
		if version != "projects/p/secrets/s/versions/1" {
			t.Errorf("GetSecretVersion() version = %q, want %q", version, "projects/p/secrets/s/versions/1")
		}
		if api.accessedName != "projects/p/secrets/s/versions/1" {
			t.Errorf("accessed = %q, want %q", api.accessedName, "projects/p/secrets/s/versions/1")
		}
	})

	t.Run("picks the most recently created enabled version", func(t *testing.T) {
		api := &fakeAPI{
			payload: []byte("hello"),
			versions: []secretVersion{
				{name: "projects/p/secrets/s/versions/1", enabled: true},
				{name: "projects/p/secrets/s/versions/2", enabled: true},
				{name: "projects/p/secrets/s/versions/3", enabled: false},
			},
		}
		c := newClientWithAPI(api, context.Background())
		_, version, err := c.GetSecretVersion("p", "s")
		if err != nil {
			t.Fatalf("GetSecretVersion() unexpected error: %v", err)
		}
		if api.accessedName != "projects/p/secrets/s/versions/2" {
			t.Errorf("accessed = %q, want %q", api.accessedName, "projects/p/secrets/s/versions/2")
		}
		if version != "projects/p/secrets/s/versions/2" {
			t.Errorf("version = %q, want %q", version, "projects/p/secrets/s/versions/2")
		}
	})

	t.Run("no enabled version", func(t *testing.T) {
		api := &fakeAPI{
			versions: []secretVersion{{name: "projects/p/secrets/s/versions/1", enabled: false}},
		}
		c := newClientWithAPI(api, context.Background())
		_, _, err := c.GetSecretVersion("p", "s")
		if err == nil {
			t.Fatal("GetSecretVersion() expected error")
		}
		if !strings.Contains(err.Error(), "no enabled version") {
			t.Errorf("error = %q, want to contain %q", err, "no enabled version")
		}
		if len(api.ctxs) != 1 {
			t.Errorf("got %d API calls, want 1 (list only)", len(api.ctxs))
		}
	})

	t.Run("list versions error", func(t *testing.T) {
		api := &fakeAPI{listVersionsErr: errors.New("boom")}
		c := newClientWithAPI(api, context.Background())
		if _, _, err := c.GetSecretVersion("p", "s"); err == nil {
			t.Error("GetSecretVersion() expected error")
		}
	})

	t.Run("access error", func(t *testing.T) {
		api := &fakeAPI{
			accessErr: errors.New("boom"),
			versions:  []secretVersion{{name: "projects/p/secrets/s/versions/1", enabled: true}},
		}
		c := newClientWithAPI(api, context.Background())
		if _, _, err := c.GetSecretVersion("p", "s"); err == nil {
			t.Error("GetSecretVersion() expected error")
		}
	})
}

func TestCreateSecretVersion(t *testing.T) {
	t.Run("adds new version and disables all others", func(t *testing.T) {
		api := &fakeAPI{
			newVersionName: "projects/p/secrets/s/versions/3",
			versions: []secretVersion{
				{name: "projects/p/secrets/s/versions/1", enabled: true},
				{name: "projects/p/secrets/s/versions/2", enabled: true},
				{name: "projects/p/secrets/s/versions/3", enabled: true},
			},
		}
		c := newClientWithAPI(api, context.Background())

		if err := c.CreateSecretVersion("p", "s", "new data"); err != nil {
			t.Fatalf("CreateSecretVersion() unexpected error: %v", err)
		}
		if string(api.addedPayload) != "new data" {
			t.Errorf("added payload = %q, want %q", api.addedPayload, "new data")
		}
		want := []string{"projects/p/secrets/s/versions/1", "projects/p/secrets/s/versions/2"}
		if !reflect.DeepEqual(api.disabled, want) {
			t.Errorf("disabled = %v, want %v", api.disabled, want)
		}
	})

	t.Run("does not disable when there is only one version", func(t *testing.T) {
		api := &fakeAPI{
			newVersionName: "projects/p/secrets/s/versions/1",
			versions:       []secretVersion{{name: "projects/p/secrets/s/versions/1", enabled: true}},
		}
		c := newClientWithAPI(api, context.Background())

		if err := c.CreateSecretVersion("p", "s", "x"); err != nil {
			t.Fatalf("CreateSecretVersion() unexpected error: %v", err)
		}
		if len(api.disabled) != 0 {
			t.Errorf("disabled = %v, want none", api.disabled)
		}
	})

	t.Run("add failure disables nothing", func(t *testing.T) {
		api := &fakeAPI{addErr: errors.New("boom")}
		c := newClientWithAPI(api, context.Background())

		if err := c.CreateSecretVersion("p", "s", "x"); err == nil {
			t.Fatal("CreateSecretVersion() expected error")
		}
		if len(api.disabled) != 0 {
			t.Errorf("disabled = %v, want none", api.disabled)
		}
	})

	t.Run("list failure disables nothing", func(t *testing.T) {
		api := &fakeAPI{newVersionName: "v3", listVersionsErr: errors.New("boom")}
		c := newClientWithAPI(api, context.Background())

		if err := c.CreateSecretVersion("p", "s", "x"); err == nil {
			t.Fatal("CreateSecretVersion() expected error")
		}
		if len(api.disabled) != 0 {
			t.Errorf("disabled = %v, want none", api.disabled)
		}
	})

	t.Run("does not disable already-disabled versions", func(t *testing.T) {
		api := &fakeAPI{
			newVersionName: "v3",
			versions: []secretVersion{
				{name: "v1", enabled: false},
				{name: "v2", enabled: true},
				{name: "v3", enabled: true},
			},
		}
		c := newClientWithAPI(api, context.Background())

		if err := c.CreateSecretVersion("p", "s", "x"); err != nil {
			t.Fatalf("CreateSecretVersion() unexpected error: %v", err)
		}
		if !reflect.DeepEqual(api.disabled, []string{"v2"}) {
			t.Errorf("disabled = %v, want [v2]", api.disabled)
		}
	})

	t.Run("disable failure stops and reports error", func(t *testing.T) {
		api := &fakeAPI{
			newVersionName: "v3",
			versions: []secretVersion{
				{name: "v1", enabled: true},
				{name: "v2", enabled: true},
				{name: "v3", enabled: true},
			},
			disableErrs: map[string]error{"v1": errors.New("boom")},
		}
		c := newClientWithAPI(api, context.Background())

		if err := c.CreateSecretVersion("p", "s", "x"); err == nil {
			t.Fatal("CreateSecretVersion() expected error")
		}
		// v1 failed; v2 must not have been attempted.
		if !reflect.DeepEqual(api.disabled, []string{"v1"}) {
			t.Errorf("disabled = %v, want [v1]", api.disabled)
		}
	})
}

// assertTimeout verifies ctx carries a deadline roughly 30s away, i.e. the
// operation wrapped it in context.WithTimeout(operationTimeout).
func assertTimeout(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline; expected a 30s timeout")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > operationTimeout {
		t.Fatalf("deadline is %v away, want between 0 and %v", remaining, operationTimeout)
	}
	// The deadline must be close to 30s, allowing a little slack for the
	// time the test itself takes to run.
	if slack := operationTimeout - remaining; slack > 2*time.Second {
		t.Fatalf("deadline is %v away, want within 2s of %v", remaining, operationTimeout)
	}
}

func TestOperationsUseTimeout(t *testing.T) {
	t.Run("ListSecrets", func(t *testing.T) {
		api := &fakeAPI{listedNames: []string{"a"}}
		c := newClientWithAPI(api, context.Background())
		if _, err := c.ListSecrets("p"); err != nil {
			t.Fatalf("ListSecrets() unexpected error: %v", err)
		}
		if len(api.ctxs) != 1 {
			t.Fatalf("got %d API calls, want 1", len(api.ctxs))
		}
		assertTimeout(t, api.ctxs[0])
	})

	t.Run("GetSecretVersion", func(t *testing.T) {
		api := &fakeAPI{
			payload:  []byte("x"),
			versions: []secretVersion{{name: "projects/p/secrets/s/versions/1", enabled: true}},
		}
		c := newClientWithAPI(api, context.Background())
		if _, _, err := c.GetSecretVersion("p", "s"); err != nil {
			t.Fatalf("GetSecretVersion() unexpected error: %v", err)
		}
		// list + access.
		if len(api.ctxs) != 2 {
			t.Fatalf("got %d API calls, want 2", len(api.ctxs))
		}
		for _, ctx := range api.ctxs {
			assertTimeout(t, ctx)
		}
	})

	t.Run("CreateSecretVersion", func(t *testing.T) {
		api := &fakeAPI{
			newVersionName: "v3",
			versions: []secretVersion{
				{name: "v1", enabled: true},
				{name: "v3", enabled: true},
			},
		}
		c := newClientWithAPI(api, context.Background())
		if err := c.CreateSecretVersion("p", "s", "x"); err != nil {
			t.Fatalf("CreateSecretVersion() unexpected error: %v", err)
		}
		// add + list + one disable.
		if len(api.ctxs) != 3 {
			t.Fatalf("got %d API calls, want 3", len(api.ctxs))
		}
		for _, ctx := range api.ctxs {
			assertTimeout(t, ctx)
		}
	})
}

// testContext is a minimal context implementation for testing
type testContext struct{}

func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Value(key any) any           { return nil }
func (testContext) Err() error                  { return nil }

var _ context.Context = (*testContext)(nil)

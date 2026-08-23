package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// AuthMethod describes where a provider keeps the credential used by
// lazykoder. Subscription sessions always remain owned by the provider CLI.
type AuthMethod string

const (
	AuthMethodAPIKey AuthMethod = "api-key"
	AuthMethodCodex  AuthMethod = "codex-login"
	AuthMethodGrok   AuthMethod = "grok-login"
)

// AuthState is the current usability of a provider credential.
type AuthState string

const (
	AuthStateChecking AuthState = "checking"
	AuthStateReady    AuthState = "ready"
	AuthStateRequired AuthState = "required"
	AuthStateMissing  AuthState = "missing"
)

// AuthStatus is safe to render in the TUI. It never contains a secret.
type AuthStatus struct {
	State   AuthState
	Label   string
	Details string
}

// ClientFactory makes a provider client after its authentication state has
// been chosen in the UI.
type ClientFactory func(string) (Client, error)

// AuthChecker verifies a safe provider status without exposing credentials.
type AuthChecker func(context.Context, string) AuthStatus

// LoginCommandFactory creates the provider-owned interactive sign-in command.
type LoginCommandFactory func(string) (*exec.Cmd, error)

// InitialAuthStatus returns an immediate, non-blocking status. CLI-backed
// providers are verified asynchronously because their status commands may
// contact the provider service.
func InitialAuthStatus(id string) AuthStatus {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		return AuthStatus{State: AuthStateMissing, Label: "unavailable"}
	}
	if descriptor.AuthMethod != AuthMethodAPIKey {
		return AuthStatus{State: AuthStateChecking, Label: "checking sign-in"}
	}
	name, configured := CredentialSource(descriptor.ID)
	if configured {
		return AuthStatus{State: AuthStateReady, Label: "key set"}
	}
	return AuthStatus{State: AuthStateRequired, Label: "key missing", Details: name}
}

// CheckAuth verifies a provider without reading or copying credentials. The
// provider CLIs retain their own refreshable sessions outside lazykoder.
func CheckAuth(ctx context.Context, id string) AuthStatus {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		return AuthStatus{State: AuthStateMissing, Label: "unavailable"}
	}
	if descriptor.AuthMethod == AuthMethodAPIKey {
		return InitialAuthStatus(descriptor.ID)
	}
	path, err := exec.LookPath(descriptor.CLI)
	if err != nil {
		return AuthStatus{State: AuthStateMissing, Label: "CLI missing", Details: descriptor.CLI}
	}
	args := []string{"login", "status"}
	if descriptor.AuthMethod == AuthMethodGrok {
		args = []string{"models"}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = withoutCredentialEnv(os.Environ(), descriptor.EnvKey)
	if descriptor.AuthMethod == AuthMethodCodex {
		cmd.Env = withoutCredentialEnv(cmd.Env, "OPENAI_API_KEY")
	}
	if descriptor.AuthMethod == AuthMethodGrok {
		cmd.Env = withoutCredentialEnv(cmd.Env, "XAI_API_KEY")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return AuthStatus{State: AuthStateRequired, Label: "sign in required", Details: commandFailure(output, err)}
	}
	return AuthStatus{State: AuthStateReady, Label: "signed in"}
}

// LoginCommand returns the supported provider-owned login command. Grok uses
// device authentication so the CLI itself prints the URL and user code.
func LoginCommand(id string) (*exec.Cmd, error) {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		return nil, fmt.Errorf("provider: unknown provider %s", id)
	}
	if descriptor.AuthMethod == AuthMethodAPIKey {
		return nil, errors.New("provider: API-key providers do not have a CLI sign-in flow")
	}
	path, err := exec.LookPath(descriptor.CLI)
	if err != nil {
		return nil, fmt.Errorf("provider: %s CLI is not installed", descriptor.CLI)
	}
	args := []string{"login"}
	if descriptor.AuthMethod == AuthMethodGrok {
		args = append(args, "--device-auth")
	}
	cmd := exec.Command(path, args...)
	if descriptor.AuthMethod == AuthMethodCodex {
		cmd.Env = withoutCredentialEnv(os.Environ(), "OPENAI_API_KEY")
	}
	if descriptor.AuthMethod == AuthMethodGrok {
		cmd.Env = withoutCredentialEnv(os.Environ(), "XAI_API_KEY")
	}
	return cmd, nil
}

func commandFailure(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if text != "" {
		return text
	}
	return err.Error()
}

func withoutCredentialEnv(env []string, name string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

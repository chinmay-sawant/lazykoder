package chat

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/provider"
)

const providerAuthTimeout = 10 * time.Second

func (m Model) checkProviderAuth(id string) tea.Cmd {
	checker := m.providerAuth
	if checker == nil {
		checker = provider.CheckAuth
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), providerAuthTimeout)
		defer cancel()
		return providerAuthMsg{id: id, status: checker(ctx, id)}
	}
}

func (m Model) providerStatus(id string) provider.AuthStatus {
	if descriptor, ok := provider.DescriptorFor(id); ok && descriptor.AuthMethod == provider.AuthMethodAPIKey {
		return provider.InitialAuthStatus(id)
	}
	if status, ok := m.providerAuthStatus[id]; ok {
		return status
	}
	return provider.InitialAuthStatus(id)
}

func (m Model) selectProvider(descriptor provider.Descriptor) (Model, tea.Cmd) {
	status := m.providerStatus(descriptor.ID)
	if descriptor.AuthMethod == provider.AuthMethodAPIKey {
		return m.activateProvider(descriptor.ID)
	}
	switch status.State {
	case provider.AuthStateReady:
		return m.activateProvider(descriptor.ID)
	case provider.AuthStateChecking:
		m.err = descriptor.Label + " sign-in is still being checked. Try again in a moment."
		return m, nil
	case provider.AuthStateMissing:
		m.err = descriptor.Label + " cannot sign in because the " + descriptor.CLI + " CLI is not installed."
		return m, nil
	default:
		return m.startProviderLogin(descriptor)
	}
}

func (m Model) startProviderLogin(descriptor provider.Descriptor) (Model, tea.Cmd) {
	starter := m.providerLogin
	if starter == nil {
		starter = provider.LoginCommand
	}
	cmd, err := starter(descriptor.ID)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = ""
	m.providerLoginTarget = descriptor.ID
	return m, tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		return providerLoginMsg{id: descriptor.ID, err: runErr}
	})
}

func (m Model) activateProvider(id string) (Model, tea.Cmd) {
	descriptor, ok := provider.DescriptorFor(id)
	if !ok {
		return m, nil
	}
	m = m.configureProvider(descriptor)
	m = m.persistSettings()
	m = m.finishPickerSelection()
	if m.err != "" {
		return m, nil
	}
	if descriptor.AuthMethod == provider.AuthMethodAPIKey {
		credential, configured := provider.CredentialSource(descriptor.ID)
		if !configured {
			m.err = fmt.Sprintf("%s selected, but %s is not configured. Add it to the environment or .env, then restart lazykoder.", descriptor.Label, credential)
			return m, nil
		}
	}
	m.copyNotice = descriptor.Label + " selected"
	return m, tea.Batch(m.refreshModels, clearCopyNotice())
}

// configureProvider switches the parent client and its persisted defaults but
// does not close a picker or schedule model loading. Model selection uses this
// to route a cross-provider row before applying the selected model.
func (m Model) configureProvider(descriptor provider.Descriptor) Model {
	m.err = ""
	m.projectSettings.Provider.Active = descriptor.ID
	m.projectSettings.Model.Default = provider.DefaultModel(descriptor.ID)
	m.model = m.projectSettings.Model.Default
	m.variant = ""

	if m.newProviderClient != nil {
		client, err := m.newProviderClient(descriptor.ID)
		if client != nil {
			m.client = client
		}
		if err != nil {
			m.err = err.Error()
		}
		childProvider := m.projectSettings.EffectiveOrchestrator().Provider
		m.childClient = m.client
		if childProvider != descriptor.ID {
			child, childErr := m.newProviderClient(childProvider)
			if childErr == nil && child != nil {
				m.childClient = child
			}
		}
		m = m.rebuildSubMgr()
	}
	return m
}

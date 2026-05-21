// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package login

import (
	"strings"

	"pb/pkg/config"
	"pb/pkg/ui"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type step int

const (
	stepChooseType step = iota
	stepCloudSoon
	stepEnterURL
	stepChooseAuth
	stepEnterUsername
	stepEnterPassword
	stepEnterToken
	stepEnterProfileName
	stepConfirmReplace
	stepDone
)

var (
	primaryColor  = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })
	normalColor   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Body })
	dimColor      = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Faint })
	successColor  = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Ok })
	errorColor    = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Err })
	subtitleColor = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Mute })

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(primaryColor)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(primaryColor)
	normalStyle   = lipgloss.NewStyle().Foreground(normalColor)
	dimStyle      = lipgloss.NewStyle().Foreground(dimColor)
	successStyle  = lipgloss.NewStyle().Bold(true).Foreground(successColor)
	hintStyle     = lipgloss.NewStyle().Foreground(dimColor)
	errorStyle    = lipgloss.NewStyle().Foreground(errorColor)
	labelStyle    = lipgloss.NewStyle().Foreground(subtitleColor)
)

// Model is the BubbleTea model for the interactive login wizard.
type Model struct {
	step         step
	typeIndex    int // 0 = self-hosted, 1 = cloud
	authIndex    int // 0 = username+password, 1 = token
	replaceIndex int // 0 = Replace, 1 = Change name

	urlInput         textinput.Model
	usernameInput    textinput.Model
	passwordInput    textinput.Model
	tokenInput       textinput.Model
	profileNameInput textinput.Model

	serverURL string
	errMsg    string

	// Result fields — set when Done is true.
	Done    bool
	Profile config.Profile
	Name    string
}

func newInput(placeholder string, charLimit int) textinput.Model {
	t := textinput.New()
	t.Placeholder = placeholder
	t.CharLimit = charLimit
	t.PromptStyle = lipgloss.NewStyle().Foreground(primaryColor)
	t.TextStyle = lipgloss.NewStyle().Foreground(normalColor)
	t.Cursor.Style = lipgloss.NewStyle().Foreground(primaryColor)
	return t
}

// New returns a fresh login wizard model.
func New() Model {
	urlInput := newInput("http://localhost:8000", 256)

	usernameInput := newInput("admin", 64)

	passwordInput := newInput("password", 64)
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '•'

	tokenInput := newInput("paste API key here", 512)

	profileInput := newInput("e.g. local, staging, prod", 64)
	profileInput.SetValue("default")

	return Model{
		step:             stepChooseType,
		urlInput:         urlInput,
		usernameInput:    usernameInput,
		passwordInput:    passwordInput,
		tokenInput:       tokenInput,
		profileNameInput: profileInput,
	}
}

// Init starts the cursor blink.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles keyboard events and routes to the active text input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.String() == "ctrl+c" {
			return m, tea.Quit
		}

		switch m.step {

		// ── Step 1: choose deployment type ──────────────────────────────────
		case stepChooseType:
			switch key.Type {
			case tea.KeyUp:
				if m.typeIndex > 0 {
					m.typeIndex--
				}
			case tea.KeyDown:
				if m.typeIndex < 1 {
					m.typeIndex++
				}
			case tea.KeyEnter:
				if m.typeIndex == 0 {
					m.errMsg = ""
					m.step = stepEnterURL
					m.urlInput.Focus()
					return m, textinput.Blink
				}
				m.step = stepCloudSoon
			}
			return m, nil

		// ── Coming-soon screen ───────────────────────────────────────────────
		case stepCloudSoon:
			m.step = stepChooseType
			return m, nil

		// ── Step 2: server URL ───────────────────────────────────────────────
		case stepEnterURL:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.step = stepChooseType
				m.urlInput.Blur()
				return m, nil
			case tea.KeyEnter:
				val := strings.TrimSpace(m.urlInput.Value())
				if val == "" {
					m.errMsg = "Server URL is required"
					return m, nil
				}
				m.serverURL = val
				m.errMsg = ""
				m.step = stepChooseAuth
				m.urlInput.Blur()
				return m, nil
			}

		// ── Step 3: auth method ──────────────────────────────────────────────
		case stepChooseAuth:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.step = stepEnterURL
				m.urlInput.Focus()
				return m, textinput.Blink
			case tea.KeyUp:
				if m.authIndex > 0 {
					m.authIndex--
				}
				return m, nil
			case tea.KeyDown:
				if m.authIndex < 1 {
					m.authIndex++
				}
				return m, nil
			case tea.KeyEnter:
				m.errMsg = ""
				if m.authIndex == 0 {
					m.step = stepEnterUsername
					m.usernameInput.Focus()
				} else {
					m.step = stepEnterToken
					m.tokenInput.Focus()
				}
				return m, textinput.Blink
			}
			return m, nil

		// ── Step 4a: username ────────────────────────────────────────────────
		case stepEnterUsername:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.step = stepChooseAuth
				m.usernameInput.Blur()
				return m, nil
			case tea.KeyEnter:
				if strings.TrimSpace(m.usernameInput.Value()) == "" {
					m.errMsg = "Username is required"
					return m, nil
				}
				m.errMsg = ""
				m.step = stepEnterPassword
				m.usernameInput.Blur()
				m.passwordInput.Focus()
				return m, textinput.Blink
			}

		// ── Step 4b: password ────────────────────────────────────────────────
		case stepEnterPassword:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.step = stepEnterUsername
				m.passwordInput.Blur()
				m.usernameInput.Focus()
				return m, textinput.Blink
			case tea.KeyEnter:
				if m.passwordInput.Value() == "" {
					m.errMsg = "Password is required"
					return m, nil
				}
				m.errMsg = ""
				m.step = stepEnterProfileName
				m.passwordInput.Blur()
				m.profileNameInput.Focus()
				return m, textinput.Blink
			}

		// ── Step 4c: token ───────────────────────────────────────────────────
		case stepEnterToken:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.step = stepChooseAuth
				m.tokenInput.Blur()
				return m, nil
			case tea.KeyEnter:
				if strings.TrimSpace(m.tokenInput.Value()) == "" {
					m.errMsg = "API key is required"
					return m, nil
				}
				m.errMsg = ""
				m.step = stepEnterProfileName
				m.tokenInput.Blur()
				m.profileNameInput.Focus()
				return m, textinput.Blink
			}

		// ── Step 5: profile name ─────────────────────────────────────────────
		case stepEnterProfileName:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.profileNameInput.Blur()
				if m.authIndex == 0 {
					m.step = stepEnterPassword
					m.passwordInput.Focus()
				} else {
					m.step = stepEnterToken
					m.tokenInput.Focus()
				}
				return m, textinput.Blink
			case tea.KeyEnter:
				val := strings.TrimSpace(m.profileNameInput.Value())
				if val == "" {
					m.errMsg = "Profile name is required"
					return m, nil
				}
				m.errMsg = ""
				// Check if the profile name already exists.
				if existing, err := config.ReadConfigFromFile(); err == nil {
					if _, exists := existing.Profiles[val]; exists {
						m.Name = val
						m.replaceIndex = 0
						m.step = stepConfirmReplace
						m.profileNameInput.Blur()
						return m, nil
					}
				}
				return m.finalize(val)
			}

		case stepConfirmReplace:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.step = stepEnterProfileName
				m.profileNameInput.Focus()
				return m, textinput.Blink
			case tea.KeyUp:
				if m.replaceIndex > 0 {
					m.replaceIndex--
				}
				return m, nil
			case tea.KeyDown:
				if m.replaceIndex < 1 {
					m.replaceIndex++
				}
				return m, nil
			case tea.KeyEnter:
				if m.replaceIndex == 1 {
					// Change name — go back to profile name input.
					m.errMsg = ""
					m.step = stepEnterProfileName
					m.profileNameInput.Focus()
					return m, textinput.Blink
				}
				// Replace — proceed with save.
				return m.finalize(m.Name)
			}
			return m, nil
		}
	}

	// Forward all other messages (character input, blink ticks) to active input.
	var cmd tea.Cmd
	switch m.step {
	case stepEnterURL:
		m.urlInput, cmd = m.urlInput.Update(msg)
	case stepEnterUsername:
		m.usernameInput, cmd = m.usernameInput.Update(msg)
	case stepEnterPassword:
		m.passwordInput, cmd = m.passwordInput.Update(msg)
	case stepEnterToken:
		m.tokenInput, cmd = m.tokenInput.Update(msg)
	case stepEnterProfileName:
		m.profileNameInput, cmd = m.profileNameInput.Update(msg)
	}
	return m, cmd
}

func (m Model) finalize(name string) (tea.Model, tea.Cmd) {
	m.Name = name
	if m.authIndex == 0 {
		m.Profile = config.Profile{
			URL:      m.serverURL,
			Username: strings.TrimSpace(m.usernameInput.Value()),
			Password: m.passwordInput.Value(),
		}
	} else {
		m.Profile = config.Profile{
			URL:   m.serverURL,
			Token: strings.TrimSpace(m.tokenInput.Value()),
		}
	}
	m.Done = true
	m.step = stepDone
	return m, tea.Quit
}

func sep() string {
	return dimStyle.Render(strings.Repeat("─", 44))
}

func breadcrumb(trail string) string {
	return dimStyle.Render("  "+trail+" ›") + " "
}

// View renders the current wizard step.
func (m Model) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("  Parseable Login"))
	b.WriteString("\n")
	b.WriteString(sep())
	b.WriteString("\n\n")

	switch m.step {

	case stepChooseType:
		b.WriteString(dimStyle.Render("  How would you like to connect?"))
		b.WriteString("\n\n")
		entries := []struct{ label, badge string }{
			{"Self-hosted", ""},
			{"Parseable Cloud", "  (coming soon)"},
		}
		for i, e := range entries {
			if i == m.typeIndex {
				b.WriteString(selectedStyle.Render("  ❯ " + e.label))
				b.WriteString(dimStyle.Render(e.badge))
			} else {
				b.WriteString(normalStyle.Render("    " + e.label))
				b.WriteString(dimStyle.Render(e.badge))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  ↑↓ navigate  ·  Enter select  ·  Ctrl+C quit"))

	case stepCloudSoon:
		b.WriteString(selectedStyle.Render("  Parseable Cloud"))
		b.WriteString("\n\n")
		b.WriteString(normalStyle.Render("  We're working on it!"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Cloud login is coming soon. Stay tuned for updates."))
		b.WriteString("\n\n")
		b.WriteString(hintStyle.Render("  Press any key to go back"))

	case stepEnterURL:
		b.WriteString(breadcrumb("Self-hosted"))
		b.WriteString(labelStyle.Render("Server URL"))
		b.WriteString("\n\n  ")
		b.WriteString(m.urlInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hintStyle.Render("  Esc back  ·  Enter continue"))

	case stepChooseAuth:
		b.WriteString(breadcrumb("Self-hosted"))
		b.WriteString(labelStyle.Render("Authentication"))
		b.WriteString("\n\n")
		authEntries := []string{"Username & Password", "API key"}
		for i, entry := range authEntries {
			if i == m.authIndex {
				b.WriteString(selectedStyle.Render("  ❯ " + entry))
			} else {
				b.WriteString(normalStyle.Render("    " + entry))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  Esc back  ·  ↑↓ navigate  ·  Enter select"))

	case stepEnterUsername:
		b.WriteString(breadcrumb("Self-hosted"))
		b.WriteString(labelStyle.Render("Username"))
		b.WriteString("\n\n  ")
		b.WriteString(m.usernameInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hintStyle.Render("  Esc back  ·  Enter continue"))

	case stepEnterPassword:
		b.WriteString(breadcrumb("Self-hosted"))
		b.WriteString(labelStyle.Render("Password"))
		b.WriteString("\n\n  ")
		b.WriteString(m.passwordInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hintStyle.Render("  Esc back  ·  Enter continue"))

	case stepEnterToken:
		b.WriteString(breadcrumb("Self-hosted"))
		b.WriteString(labelStyle.Render("API key"))
		b.WriteString("\n\n  ")
		b.WriteString(m.tokenInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hintStyle.Render("  Esc back  ·  Enter continue"))

	case stepEnterProfileName:
		b.WriteString(labelStyle.Render("  Profile name"))
		b.WriteString("\n\n  ")
		b.WriteString(m.profileNameInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hintStyle.Render("  Esc back  ·  Enter save"))

	case stepConfirmReplace:
		b.WriteString(errorStyle.Render("  Profile '" + m.Name + "' already exists"))
		b.WriteString("\n\n")
		entries := []string{"Replace it", "Change name"}
		for i, e := range entries {
			if i == m.replaceIndex {
				b.WriteString(selectedStyle.Render("  ❯ " + e))
			} else {
				b.WriteString(normalStyle.Render("    " + e))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  Esc back  ·  ↑↓ navigate  ·  Enter select"))

	case stepDone:
		b.WriteString(successStyle.Render("  ✓ Profile '" + m.Name + "' saved"))
		b.WriteString("\n\n")
		b.WriteString(labelStyle.Render("  URL:   "))
		b.WriteString(normalStyle.Render(m.Profile.URL))
		b.WriteString("\n")
		if m.Profile.Username != "" {
			b.WriteString(labelStyle.Render("  User:  "))
			b.WriteString(normalStyle.Render(m.Profile.Username))
			b.WriteString("\n")
		}
		if m.Profile.Token != "" {
			b.WriteString(labelStyle.Render("  Auth:  "))
			b.WriteString(normalStyle.Render("API key (stored)"))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  To add more profiles:"))
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("  pb profile add <name> <url> [user] [pass]"))
	}

	b.WriteString("\n\n")
	return b.String()
}

func renderErr(msg string) string {
	if msg == "" {
		return ""
	}
	return errorStyle.Render("  ✗ "+msg) + "\n\n"
}

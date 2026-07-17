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

	"github.com/parseablehq/pb/pkg/config"
	"github.com/parseablehq/pb/pkg/ui"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type step int

const (
	stepChooseType step = iota
	stepEnterURL
	stepChooseCloudAuth
	stepChooseAuth
	stepEnterUsername
	stepEnterPassword
	stepEnterAPIKey
	stepEnterProfileName
	stepConfirmReplace
	stepDone
)

var (
	primaryColor  = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })
	activeColor   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Active })
	normalColor   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Body })
	dimColor      = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Faint })
	successColor  = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Ok })
	errorColor    = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Err })
	subtitleColor = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Mute })
	borderColor   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Border })

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(primaryColor)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(activeColor)
	normalStyle   = lipgloss.NewStyle().Foreground(normalColor)
	dimStyle      = lipgloss.NewStyle().Foreground(dimColor)
	successStyle  = lipgloss.NewStyle().Bold(true).Foreground(successColor)
	hintStyle     = lipgloss.NewStyle().Foreground(dimColor)
	errorStyle    = lipgloss.NewStyle().Foreground(errorColor)
	labelStyle    = lipgloss.NewStyle().Foreground(subtitleColor).Bold(true)
	keyStyle      = lipgloss.NewStyle().Bold(true).Foreground(primaryColor)
	railStyle     = lipgloss.NewStyle().Background(activeColor)
)

// Model is the BubbleTea model for the interactive login wizard.
type Model struct {
	step           step
	typeIndex      int // 0 = self-hosted, 1 = cloud
	cloudAuthIndex int // 0 = OAuth browser, 1 = API key
	authIndex      int // 0 = username+password, 1 = api key
	replaceIndex   int // 0 = Replace, 1 = Change name

	urlInput         textinput.Model
	usernameInput    textinput.Model
	passwordInput    textinput.Model
	apiKeyInput      textinput.Model
	profileNameInput textinput.Model

	serverURL string
	errMsg    string

	// Result fields — set when Done is true.
	Done             bool
	Profile          config.Profile
	Name             string
	CloudDeviceLogin bool
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

	apiKeyInput := newInput("paste API key here", 512)
	apiKeyInput.EchoMode = textinput.EchoPassword
	apiKeyInput.EchoCharacter = '•'

	profileInput := newInput("e.g. local, staging, prod", 64)
	profileInput.SetValue("default")

	return Model{
		step:             stepChooseType,
		urlInput:         urlInput,
		usernameInput:    usernameInput,
		passwordInput:    passwordInput,
		apiKeyInput:      apiKeyInput,
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
				m.errMsg = ""
				m.step = stepChooseCloudAuth
				return m, nil
			}
			return m, nil

		// ── Step 2a: cloud auth method ───────────────────────────────────────
		case stepChooseCloudAuth:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.step = stepChooseType
				return m, nil
			case tea.KeyUp:
				if m.cloudAuthIndex > 0 {
					m.cloudAuthIndex--
				}
				return m, nil
			case tea.KeyDown:
				if m.cloudAuthIndex < 1 {
					m.cloudAuthIndex++
				}
				return m, nil
			case tea.KeyEnter:
				m.errMsg = ""
				if m.cloudAuthIndex == 0 {
					m.step = stepEnterProfileName
					m.profileNameInput.Focus()
				} else {
					m.step = stepEnterAPIKey
					m.apiKeyInput.Focus()
				}
				return m, textinput.Blink
			}
			return m, nil

		// ── Step 2b: server URL ──────────────────────────────────────────────
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
					m.step = stepEnterAPIKey
					m.apiKeyInput.Focus()
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

		// ── Step 4c: API key ─────────────────────────────────────────────────
		case stepEnterAPIKey:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				if m.typeIndex == 1 {
					m.step = stepChooseCloudAuth
				} else {
					m.step = stepChooseAuth
				}
				m.apiKeyInput.Blur()
				return m, nil
			case tea.KeyEnter:
				if strings.TrimSpace(m.apiKeyInput.Value()) == "" {
					m.errMsg = "API key is required"
					return m, nil
				}
				m.errMsg = ""
				m.step = stepEnterProfileName
				m.apiKeyInput.Blur()
				m.profileNameInput.Focus()
				return m, textinput.Blink
			}

		// ── Step 5: profile name ─────────────────────────────────────────────
		case stepEnterProfileName:
			switch key.Type {
			case tea.KeyEsc:
				m.errMsg = ""
				m.profileNameInput.Blur()
				if m.typeIndex == 1 {
					if m.cloudAuthIndex == 0 {
						m.step = stepChooseCloudAuth
					} else {
						m.step = stepEnterAPIKey
						m.apiKeyInput.Focus()
					}
				} else if m.authIndex == 0 {
					m.step = stepEnterPassword
					m.passwordInput.Focus()
				} else {
					m.step = stepEnterAPIKey
					m.apiKeyInput.Focus()
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
	case stepEnterAPIKey:
		m.apiKeyInput, cmd = m.apiKeyInput.Update(msg)
	case stepEnterProfileName:
		m.profileNameInput, cmd = m.profileNameInput.Update(msg)
	}
	return m, cmd
}

func (m Model) finalize(name string) (tea.Model, tea.Cmd) {
	m.Name = name
	if m.typeIndex == 1 && m.cloudAuthIndex == 0 {
		m.CloudDeviceLogin = true
		m.Profile = config.Profile{
			Cloud: true,
		}
	} else if m.typeIndex == 1 {
		m.Profile = config.Profile{
			Cloud:  true,
			APIKey: strings.TrimSpace(m.apiKeyInput.Value()),
		}
	} else if m.authIndex == 0 {
		m.Profile = config.Profile{
			Cloud:    false,
			URL:      m.serverURL,
			Username: strings.TrimSpace(m.usernameInput.Value()),
			Password: m.passwordInput.Value(),
		}
	} else {
		m.Profile = config.Profile{
			Cloud:  false,
			URL:    m.serverURL,
			APIKey: strings.TrimSpace(m.apiKeyInput.Value()),
		}
	}
	m.Done = true
	m.step = stepDone
	return m, tea.Quit
}

// rowSelected — Active sky-blue rail + ❯ cursor + bold Active label.
// The arrow makes the active row unambiguous on monochrome terminals
// where bg fills may not render.
func rowSelected(label string) string {
	return railStyle.Render(" ") + " " + selectedStyle.Render("❯ "+label)
}

// rowIdle — 4-space prefix + Body label, matches the arrow indent.
func rowIdle(label string) string {
	return "    " + normalStyle.Render(label)
}

// hint — render "<key> action  <key> action" with consistent styling.
func hint(pairs ...[2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, kv := range pairs {
		parts = append(parts, keyStyle.Render("<"+kv[0]+">")+hintStyle.Render(" "+kv[1]))
	}
	return "  " + strings.Join(parts, hintStyle.Render("    "))
}

// View renders the current wizard step inside a flat NormalBorder
// card with a fixed UPPERCASE title strip. Each step writes its own
// label row + body + hint row, joined into the card.
func (m Model) View() string {
	// Device authorization and persistence happen after the wizard exits.
	if m.step == stepDone && m.CloudDeviceLogin {
		return ""
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("PARSEABLE LOGIN"))
	b.WriteString("\n\n")

	switch m.step {

	case stepChooseType:
		b.WriteString(labelStyle.Render("CONNECT TO"))
		b.WriteString("\n\n")
		entries := []struct{ label, badge string }{
			{"Self-hosted", ""},
			{"Parseable Cloud", ""},
		}
		for i, e := range entries {
			if i == m.typeIndex {
				b.WriteString(rowSelected(e.label))
			} else {
				b.WriteString(rowIdle(e.label))
			}
			b.WriteString(dimStyle.Render(e.badge))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hint([2]string{"↑↓", "navigate"}, [2]string{"enter", "select"}, [2]string{"ctrl-c", "quit"}))

	case stepEnterURL:
		b.WriteString(labelStyle.Render("SERVER URL"))
		b.WriteString("\n\n  ")
		b.WriteString(m.urlInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"enter", "continue"}))

	case stepChooseCloudAuth:
		b.WriteString(labelStyle.Render("CLOUD AUTHENTICATION"))
		b.WriteString("\n\n")
		authEntries := []string{"Device login (browser)", "API key"}
		for i, entry := range authEntries {
			if i == m.cloudAuthIndex {
				b.WriteString(rowSelected(entry))
			} else {
				b.WriteString(rowIdle(entry))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"↑↓", "navigate"}, [2]string{"enter", "select"}))

	case stepChooseAuth:
		b.WriteString(labelStyle.Render("AUTHENTICATION"))
		b.WriteString("\n\n")
		authEntries := []string{"Username & Password", "API key"}
		for i, entry := range authEntries {
			if i == m.authIndex {
				b.WriteString(rowSelected(entry))
			} else {
				b.WriteString(rowIdle(entry))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"↑↓", "navigate"}, [2]string{"enter", "select"}))

	case stepEnterUsername:
		b.WriteString(labelStyle.Render("USERNAME"))
		b.WriteString("\n\n  ")
		b.WriteString(m.usernameInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"enter", "continue"}))

	case stepEnterPassword:
		b.WriteString(labelStyle.Render("PASSWORD"))
		b.WriteString("\n\n  ")
		b.WriteString(m.passwordInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"enter", "continue"}))

	case stepEnterAPIKey:
		if m.typeIndex == 1 {
			b.WriteString(labelStyle.Render("CLOUD API KEY"))
		} else {
			b.WriteString(labelStyle.Render("API KEY"))
		}
		b.WriteString("\n\n  ")
		b.WriteString(m.apiKeyInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"enter", "continue"}))

	case stepEnterProfileName:
		b.WriteString(labelStyle.Render("PROFILE NAME"))
		b.WriteString("\n\n  ")
		b.WriteString(m.profileNameInput.View())
		b.WriteString("\n\n")
		b.WriteString(renderErr(m.errMsg))
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"enter", "save"}))

	case stepConfirmReplace:
		b.WriteString(errorStyle.Render("  Profile '" + m.Name + "' already exists"))
		b.WriteString("\n\n")
		entries := []string{"Replace it", "Change name"}
		for i, e := range entries {
			if i == m.replaceIndex {
				b.WriteString(rowSelected(e))
			} else {
				b.WriteString(rowIdle(e))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hint([2]string{"esc", "back"}, [2]string{"↑↓", "navigate"}, [2]string{"enter", "select"}))

	case stepDone:
		b.WriteString(successStyle.Render("✓ profile '" + m.Name + "' saved"))
		b.WriteString("\n\n")
		if m.Profile.URL != "" {
			b.WriteString("  " + labelStyle.Render("URL  "))
			b.WriteString(normalStyle.Render(m.Profile.URL))
			b.WriteString("\n")
		}
		if m.Profile.Cloud {
			b.WriteString("  " + labelStyle.Render("AUTH "))
			b.WriteString(normalStyle.Render("Cloud API key (stored after validation)"))
			b.WriteString("\n")
		}
		if m.Profile.Username != "" {
			b.WriteString("  " + labelStyle.Render("USER "))
			b.WriteString(normalStyle.Render(m.Profile.Username))
			b.WriteString("\n")
		}
		if m.Profile.APIKey != "" {
			b.WriteString("  " + labelStyle.Render("AUTH "))
			b.WriteString(normalStyle.Render("API key (stored)"))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Next steps:"))
		b.WriteString("\n  ")
		b.WriteString(hintStyle.Render("pb status        Verify active connection"))
		b.WriteString("\n  ")
		b.WriteString(hintStyle.Render("pb dataset list  Explore available datasets"))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(60).
		Render(b.String()) + "\n"
}

func renderErr(msg string) string {
	if msg == "" {
		return ""
	}
	return errorStyle.Render("  ✗ "+msg) + "\n\n"
}

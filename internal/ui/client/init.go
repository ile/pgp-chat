package client

import tea "github.com/charmbracelet/bubbletea"

func (c Client) Init() tea.Cmd {
	if c.incoming != nil {
		return waitForIncoming(c.incoming)
	}
	return nil
}

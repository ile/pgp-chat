package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	log "github.com/charmbracelet/log"
	"github.com/nigel-dev/pgp-chat/internal/chat"
)

type incomingMsg struct {
	event chat.Event
}

type sendResultMsg struct {
	text string
	err  error
}

func waitForIncoming(events <-chan chat.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return incomingMsg{event: chat.Event{Err: fmt.Errorf("peer connection closed")}}
		}
		return incomingMsg{event: event}
	}
}

func (c Client) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	if !c.ready {
		c.viewport.HighPerformanceRendering = useHighPerformanceRenderer
		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(80),
		)
		if err == nil {
			c.messageRender = renderer
			c.refreshMessages()
		}
		c.ready = true
		c.viewport.GotoTop()
	}

	switch message := msg.(type) {
	case tea.MouseMsg:
		log.Debug("mouse event", "key", tea.MouseEvent(message))
	case tea.WindowSizeMsg:
		c.onWindowSizeChanged(message)
	case incomingMsg:
		if message.event.Err != nil {
			c.statusbar.SetContent("ERROR", "peer disconnected", "", "")
			log.Error("peer connection closed", "error", message.event.Err)
		} else {
			c.messages = append(c.messages, "# Peer\n\n"+message.event.Message.Body)
			c.refreshMessages()
			if c.incoming != nil {
				cmds = append(cmds, waitForIncoming(c.incoming))
			}
		}
	case sendResultMsg:
		if message.err != nil {
			c.statusbar.SetContent("ERROR", "send failed", "", "")
			log.Error("message send failed", "error", message.err)
		} else {
			c.messages = append(c.messages, "# Me\n\n"+message.text)
			c.refreshMessages()
			c.statusbar.SetContent("PGP-CHAT", "SENT", "libp2p", "")
		}
	case tea.KeyMsg:
		log.Debug("key pressed", "key", message.String())
		switch {
		case key.Matches(message, c.keys.Quit):
			if !c.InputActive() {
				return c, tea.Quit
			}
		case key.Matches(message, c.keys.SwitchFocus):
			if c.input.Focused() {
				c.input.Blur()
				c.ctx.InputActive = false
			} else {
				c.input.Focus()
				c.input.CursorStart()
				c.ctx.InputActive = true
			}
		case key.Matches(message, c.keys.Help):
			if !c.input.Focused() {
				c.help.ShowAll = !c.help.ShowAll
			}
		case key.Matches(message, c.keys.MultiLineToggle):
			c.multiLineSend = !c.multiLineSend
			if c.multiLineSend {
				c.input.SetHeight(5)
				c.input.KeyMap.InsertNewline.SetEnabled(true)
				c.input.Reset()
			} else {
				c.input.SetHeight(1)
				c.input.Reset()
				c.input.KeyMap.InsertNewline.SetEnabled(false)
			}
		case key.Matches(message, c.keys.Send, c.keys.MultiLineSend):
			if c.input.Focused() {
				if !c.multiLineSend {
					text := c.input.Value()
					cmds = append(cmds, c.sendMessage(text))
					c.input.Reset()
					c.input.SetValue("")
				} else if key.Matches(message, c.keys.MultiLineSend) {
					text := c.input.Value()
					cmds = append(cmds, c.sendMessage(text))
					c.input.SetHeight(1)
					c.multiLineSend = false
					c.input.Reset()
					c.input.SetValue("")
				}
			}
		}
	}

	c.userList, cmd = c.userList.Update(msg)
	cmds = append(cmds, cmd)
	c.keyList, cmd = c.keyList.Update(msg)
	cmds = append(cmds, cmd)
	c.viewport, cmd = c.viewport.Update(msg)
	cmds = append(cmds, cmd)
	c.input, cmd = c.input.Update(msg)
	cmds = append(cmds, cmd)
	c.statusbar, cmd = c.statusbar.Update(msg)
	cmds = append(cmds, cmd)
	c.help, cmd = c.help.Update(msg)
	cmds = append(cmds, cmd)

	return c, tea.Batch(cmds...)
}

func (c *Client) onWindowSizeChanged(msg tea.WindowSizeMsg) {
	c.ctx.ScreenWidth = msg.Width
	c.ctx.ScreenHeight = msg.Height
	c.statusbar.SetSize(msg.Width)
	c.help.Width = msg.Width
	c.keyList.SetHeight(msg.Height/2 - 10)
	c.keyList.SetWidth(msg.Width - lipgloss.Width(c.messageView()) - 20)
	c.input.SetWidth(msg.Width - lipgloss.Width(c.keyListView()) - 12)
}

func (c Client) sendMessage(message string) tea.Cmd {
	return func() tea.Msg {
		if c.session == nil {
			return sendResultMsg{text: message, err: fmt.Errorf("no peer session")}
		}
		return sendResultMsg{text: message, err: c.session.Send(message)}
	}
}

func (c *Client) refreshMessages() {
	if c.messageRender == nil {
		return
	}
	content, err := c.messageRender.Render(strings.Join(c.messages, "\n\n----\n"))
	if err == nil {
		c.viewport.SetContent(content)
	}
}

func (c *Client) InputActive() bool {
	if c.input.Focused() {
		return true
	}
	if c.keyList.FilterState() == list.Filtering {
		c.ctx.InputActive = true
		return true
	}
	c.ctx.InputActive = false
	return false
}

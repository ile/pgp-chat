package client

import (
	bhelp "github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/mistakenelf/teacup/statusbar"
	"github.com/nigel-dev/pgp-chat/internal/chat"
	"github.com/nigel-dev/pgp-chat/internal/ui/context"
	"github.com/nigel-dev/pgp-chat/internal/ui/theme"
	slog "log"
	"os"
	"time"
)

type PublicKey struct {
	key         string
	userName    string
	keyId       string
	fingerprint string
	ownerTrust  string
	validity    string
	active      bool
}

type ChatUser struct {
	userName string
	desc     string
}

type Client struct {
	input         textarea.Model
	username      string
	users         []string
	viewport      viewport.Model
	userList      list.Model
	keyList       list.Model
	publicKeys    []PublicKey
	help          bhelp.Model
	statusbar     statusbar.Model
	keys          KeyMap
	activeBox     int
	ctx           context.ProgramContext
	ready         bool
	messages      []string
	debug         bool
	multiLineSend bool
	messageRender *glamour.TermRenderer
	session       *chat.Session
	incoming      <-chan chat.Event
}

func New(debug bool, session *chat.Session) (Client, *os.File) {
	c := Client{}
	var loggerFile *os.File

	if debug {
		logFile, fileErr := os.OpenFile("debug.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
		if fileErr == nil {
			loggerFile = logFile
			log.SetOutput(logFile)
			log.SetTimeFormat(time.Kitchen)
			log.SetReportCaller(true)
			log.SetLevel(log.DebugLevel)
			log.Debug("logging to debug.log")
		} else {
			loggerFile, _ = tea.LogToFile("debug.log", "debug")
			slog.Print("failed setting up logging", fileErr)
		}
	}

	dataPublicKeys := []list.Item{PublicKey{userName: "no peer connected"}}
	dataUserList := []list.Item{ChatUser{userName: "demo", desc: "no network session"}}
	if session != nil {
		dataPublicKeys = []list.Item{
			PublicKey{userName: "configured peer", fingerprint: session.PeerFingerprint(), active: true},
		}
		dataUserList = []list.Item{
			ChatUser{userName: session.RemotePeer(), desc: "libp2p peer"},
		}
	}

	themeData := theme.GetTheme("default")
	c.ctx = context.ProgramContext{Theme: themeData}

	inputModel := textarea.New()
	inputModel.Focus()
	inputModel.ShowLineNumbers = false
	inputModel.Prompt = "> "
	inputModel.Placeholder = "Send message..."
	inputModel.SetHeight(1)
	inputModel.KeyMap.InsertNewline.SetEnabled(false)
	inputModel.FocusedStyle.CursorLine = lipgloss.NewStyle()

	userListModel := list.New(dataUserList, list.NewDefaultDelegate(), 0, 0)
	userListModel.SetShowHelp(false)
	userListModel.SetFilteringEnabled(false)
	userListModel.SetShowStatusBar(false)
	userListModel.SetShowTitle(false)
	userListModel.KeyMap.Quit.SetEnabled(false)

	keylistModel := list.New(dataPublicKeys, newPublicKeyDelegate(&c.ctx), 0, 3)
	keylistModel.SetShowTitle(false)
	keylistModel.KeyMap.Quit.SetEnabled(false)
	keylistModel.SetShowHelp(false)
	keylistModel.DisableQuitKeybindings()

	statusbarModel := statusbar.New(
		statusbar.ColorConfig{
			Foreground: themeData.StatusBarSelectedFileForegroundColor,
			Background: themeData.StatusBarSelectedFileBackgroundColor,
		},
		statusbar.ColorConfig{
			Foreground: themeData.StatusBarBarForegroundColor,
			Background: themeData.StatusBarBarBackgroundColor,
		},
		statusbar.ColorConfig{
			Foreground: themeData.StatusBarTotalFilesForegroundColor,
			Background: themeData.StatusBarTotalFilesBackgroundColor,
		},
		statusbar.ColorConfig{
			Foreground: themeData.StatusBarLogoForegroundColor,
			Background: themeData.StatusBarLogoBackgroundColor,
		},
	)
	statusbarModel.SetContent("PGP-CHAT", "READY", "libp2p", "")

	c.input = inputModel
	c.username = "local user"
	c.userList = userListModel
	c.keyList = keylistModel
	c.statusbar = statusbarModel
	c.help = bhelp.New()
	c.debug = debug
	c.keys = Keys
	c.session = session
	if session != nil {
		c.incoming = session.Events()
		c.ctx.InputActive = true
	}

	return c, loggerFile
}

func (p PublicKey) Title() string {
	if p.active {
		return "✅ " + p.userName
	}
	return p.userName
}

func (p PublicKey) Description() string { return p.fingerprint }
func (p PublicKey) FilterValue() string { return p.userName }

func (c ChatUser) Title() string       { return c.userName }
func (c ChatUser) Description() string { return c.desc }
func (c ChatUser) FilterValue() string { return c.userName }

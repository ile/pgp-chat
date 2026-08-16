package gui

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/nigel-dev/pgp-chat/internal/chat"
)

const (
	appID      = "io.github.ile.pgpchat"
	windowW    = 1120
	windowH    = 720
	connected  = "Connected • PGP + Noise"
	disconnect = "Peer disconnected"
)

// KeygenOptions contains the values entered in the graphical key generation
// form. Passphrase is only populated while the submit callback is running.
type KeygenOptions struct {
	Name        string
	Email       string
	PrivatePath string
	PublicPath  string
	Passphrase  []byte
	Force       bool
}

// RunKeygenForm opens the graphical key generation form. The callback owns
// the actual key generation and saving so that validation errors can be shown
// in the form and retried without leaving the GUI. Its returned text is shown
// in the GUI after a successful generation.
func RunKeygenForm(defaults KeygenOptions, generate func(KeygenOptions) (string, error)) (string, error) {
	if generate == nil {
		return "", fmt.Errorf("key generation callback is required")
	}

	application := app.NewWithID(appID + ".keygen")
	application.Settings().SetTheme(theme.DarkTheme())
	window := application.NewWindow("Generate OpenPGP keys")

	name := widget.NewEntry()
	name.SetText(defaults.Name)
	name.SetPlaceHolder("Alice")
	email := widget.NewEntry()
	email.SetText(defaults.Email)
	email.SetPlaceHolder("alice@example.com")
	privatePath := widget.NewEntry()
	privatePath.SetText(defaults.PrivatePath)
	publicPath := widget.NewEntry()
	publicPath.SetText(defaults.PublicPath)
	passphrase := widget.NewPasswordEntry()
	passphrase.SetPlaceHolder("Optional, but recommended")
	confirm := widget.NewPasswordEntry()
	confirm.SetPlaceHolder("Repeat passphrase")
	force := widget.NewCheck("Allow overwriting existing files", nil)
	force.SetChecked(defaults.Force)
	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord

	type formResult struct {
		message string
		err     error
	}
	result := make(chan formResult, 1)
	closed := false
	generated := false
	generatedMessage := ""
	finish := func(message string, err error) {
		if closed {
			return
		}
		closed = true
		result <- formResult{message: message, err: err}
		application.Quit()
	}

	var generateButton *widget.Button
	submit := func() {
		enteredPassphrase := []byte(passphrase.Text)
		enteredConfirmation := []byte(confirm.Text)
		passphrase.SetText("")
		confirm.SetText("")

		options := KeygenOptions{
			Name:        name.Text,
			Email:       email.Text,
			PrivatePath: privatePath.Text,
			PublicPath:  publicPath.Text,
			Passphrase:  enteredPassphrase,
			Force:       force.Checked,
		}
		if err := validateKeygenOptions(options, enteredConfirmation); err != nil {
			clearBytes(enteredPassphrase)
			clearBytes(enteredConfirmation)
			errorLabel.SetText(err.Error())
			return
		}
		message, err := generate(options)
		clearBytes(enteredPassphrase)
		clearBytes(enteredConfirmation)
		if err != nil {
			errorLabel.SetText(err.Error())
			return
		}
		generated = true
		generatedMessage = message
		errorLabel.SetText("Keys generated successfully.\n" + message)
		generateButton.SetText("Done")
		generateButton.OnTapped = func() { finish(generatedMessage, nil) }
	}

	entryForm := widget.NewForm(
		widget.NewFormItem("Name", name),
		widget.NewFormItem("Email (optional)", email),
		widget.NewFormItem("Private key", privatePath),
		widget.NewFormItem("Public key", publicPath),
		widget.NewFormItem("Passphrase", passphrase),
		widget.NewFormItem("Confirm", confirm),
	)
	generateButton = widget.NewButton("Generate keys", submit)
	generateButton.Importance = widget.HighImportance
	cancelButton := widget.NewButton("Cancel", func() { finish("", fmt.Errorf("key generation cancelled")) })
	confirm.OnSubmitted = func(string) { submit() }

	window.SetContent(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Generate OpenPGP key pair", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("The private key is saved encrypted when a passphrase is provided."),
		entryForm,
		force,
		errorLabel,
		container.NewHBox(layout.NewSpacer(), cancelButton, generateButton),
	)))
	window.Resize(fyne.NewSize(620, 480))
	window.SetOnClosed(func() {
		if !closed {
			closed = true
			if generated {
				result <- formResult{message: generatedMessage}
			} else {
				result <- formResult{err: fmt.Errorf("key generation cancelled")}
			}
		}
		application.Quit()
	})
	window.Show()
	window.Canvas().Focus(name)
	application.Run()
	finalResult := <-result
	return finalResult.message, finalResult.err
}

func validateKeygenOptions(options KeygenOptions, confirmation []byte) error {
	if strings.TrimSpace(options.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if options.PrivatePath == "" || options.PublicPath == "" {
		return fmt.Errorf("private and public key paths are required")
	}
	if options.PrivatePath == options.PublicPath {
		return fmt.Errorf("private and public key paths must be different")
	}
	if !bytes.Equal(options.Passphrase, confirmation) {
		return fmt.Errorf("passphrases do not match")
	}
	if !options.Force {
		for _, path := range []string{options.PrivatePath, options.PublicPath} {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("refusing to overwrite %q; enable overwrite or choose another path", path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("check key path %q: %w", path, err)
			}
		}
	}
	return nil
}

// PromptPassphrase opens a password-protected dialog and keeps asking until
// validate accepts the entered passphrase or the user closes the dialog.
func PromptPassphrase(validate func([]byte) error) error {
	if validate == nil {
		return fmt.Errorf("passphrase validator is required")
	}

	application := app.NewWithID(appID + ".unlock")
	application.Settings().SetTheme(theme.DarkTheme())
	window := application.NewWindow("Unlock private key")

	entry := widget.NewPasswordEntry()
	entry.SetPlaceHolder("Private key passphrase")
	message := widget.NewLabel("Enter the passphrase for your private OpenPGP key.")
	message.Wrapping = fyne.TextWrapWord
	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord

	result := make(chan error, 1)
	closed := false
	finish := func(err error) {
		if closed {
			return
		}
		closed = true
		result <- err
		application.Quit()
	}

	submit := func() {
		passphrase := []byte(entry.Text)
		err := validate(passphrase)
		clearBytes(passphrase)
		entry.SetText("")
		if err != nil {
			errorLabel.SetText("Unlock failed: " + err.Error())
			window.Canvas().Focus(entry)
			return
		}
		finish(nil)
	}
	entry.OnSubmitted = func(string) { submit() }
	unlock := widget.NewButton("Unlock", submit)
	unlock.Importance = widget.HighImportance
	cancel := widget.NewButton("Cancel", func() { finish(fmt.Errorf("passphrase prompt cancelled")) })

	window.SetContent(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Unlock private key", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		message,
		entry,
		container.NewHBox(layout.NewSpacer(), cancel, unlock),
		errorLabel,
	)))
	window.Resize(fyne.NewSize(480, 220))
	window.SetOnClosed(func() {
		if !closed {
			closed = true
			result <- fmt.Errorf("passphrase prompt cancelled")
		}
		application.Quit()
	})
	window.Show()
	window.Canvas().Focus(entry)
	application.Run()
	return <-result
}

func clearBytes(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

// Run opens the Fyne chat window for an already established chat session.
// The PGP and libp2p layers stay outside the GUI package.
func Run(session *chat.Session) error {
	if session == nil {
		return fmt.Errorf("cannot start GUI without a chat session")
	}

	application := app.NewWithID(appID)
	application.Settings().SetTheme(theme.DarkTheme())
	window := application.NewWindow("PGP Chat")
	model := newModel(session)
	window.SetContent(model.content())
	window.Resize(fyne.NewSize(windowW, windowH))
	window.SetOnClosed(model.close)
	window.Show()

	go model.receiveEvents()
	application.Run()
	return nil
}

type model struct {
	session *chat.Session

	messageList *fyne.Container
	scroll      *container.Scroll
	placeholder fyne.CanvasObject
	input       *widget.Entry
	send        *widget.Button
	status      *widget.Label

	sending  bool
	closed   chan struct{}
	closeOne sync.Once
}

func newModel(session *chat.Session) *model {
	messageList := container.NewVBox()
	placeholder := widget.NewLabel("No messages yet. Start the conversation below.")
	placeholder.Wrapping = fyne.TextWrapWord
	messageList.Add(placeholder)

	input := widget.NewMultiLineEntry()
	input.SetMinRowsVisible(3)
	input.PlaceHolder = "Write an encrypted message..."

	status := widget.NewLabel(connected)
	status.TextStyle = fyne.TextStyle{Bold: true}

	model := &model{
		session:     session,
		messageList: messageList,
		placeholder: placeholder,
		scroll:      container.NewVScroll(messageList),
		input:       input,
		status:      status,
		closed:      make(chan struct{}),
	}
	model.send = widget.NewButton("Send", model.submit)
	model.send.Importance = widget.HighImportance
	input.OnSubmitted = func(text string) { model.submitText(text) }
	return model
}

func (m *model) content() fyne.CanvasObject {
	brand := widget.NewLabel("PGP CHAT")
	brand.TextStyle = fyne.TextStyle{Bold: true}
	header := container.NewBorder(nil, widget.NewSeparator(), brand, m.status, nil)

	composer := container.NewVBox(
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, m.send, m.input),
	)
	chatPane := container.NewBorder(header, composer, nil, nil, m.scroll)

	securityPane := container.NewVBox(
		sectionTitle("SECURITY"),
		widget.NewSeparator(),
		labelTitle("OpenPGP peer fingerprint"),
		wrappedLabel(m.session.PeerFingerprint()),
		widget.NewSeparator(),
		labelTitle("Transport"),
		widget.NewLabel("libp2p / Noise"),
		widget.NewSeparator(),
		labelTitle("Remote PeerID"),
		wrappedLabel(m.session.RemotePeer()),
		layout.NewSpacer(),
		widget.NewLabel("Messages are signed and encrypted before sending."),
	)
	securityPane = container.NewPadded(securityPane)

	conversation := container.NewHSplit(chatPane, securityPane)
	conversation.SetOffset(0.77)

	sidebar := container.NewVBox(
		sectionTitle("PGP CHAT"),
		widget.NewSeparator(),
		widget.NewLabel("Conversation"),
		widget.NewLabel(m.session.RemotePeer()),
		layout.NewSpacer(),
		widget.NewSeparator(),
		widget.NewLabel("Secure session"),
		widget.NewLabel("Noise transport active"),
	)
	sidebar = container.NewPadded(sidebar)

	main := container.NewHSplit(sidebar, conversation)
	main.SetOffset(0.22)
	return main
}

func (m *model) receiveEvents() {
	for event := range m.session.Events() {
		event := event
		select {
		case <-m.closed:
			return
		default:
		}
		fyne.Do(func() {
			select {
			case <-m.closed:
				return
			default:
			}
			if event.Err != nil {
				m.status.SetText(disconnect)
				m.send.Disable()
				return
			}
			m.addMessage("Peer", event.Message.Body, event.Message.SentAt)
			m.status.SetText(connected)
		})
	}
}

func (m *model) submit() {
	m.submitText(m.input.Text)
}

func (m *model) submitText(text string) {
	if m.sending || strings.TrimSpace(text) == "" {
		return
	}
	m.sending = true
	m.send.Disable()
	m.status.SetText("Encrypting and sending...")
	m.input.SetText("")

	go func(body string) {
		err := m.session.Send(body)
		fyne.Do(func() {
			defer func() {
				m.sending = false
				m.send.Enable()
			}()
			if err != nil {
				m.status.SetText("Send failed: " + err.Error())
				m.input.SetText(body)
				return
			}
			m.addMessage("You", body, time.Now())
			m.status.SetText(connected)
		})
	}(text)
}

func (m *model) addMessage(sender, body string, sentAt time.Time) {
	if m.placeholder != nil {
		m.messageList.Remove(m.placeholder)
		m.placeholder = nil
	}

	message := widget.NewLabel(body)
	message.Wrapping = fyne.TextWrapWord
	card := widget.NewCard(sender, sentAt.Local().Format("15:04"), message)
	m.messageList.Add(card)
	m.messageList.Refresh()
	m.scroll.ScrollToBottom()
}

func (m *model) close() {
	m.closeOne.Do(func() {
		close(m.closed)
		_ = m.session.Close()
	})
}

func sectionTitle(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.TextStyle = fyne.TextStyle{Bold: true}
	return label
}

func labelTitle(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.TextStyle = fyne.TextStyle{Bold: true}
	return label
}

func wrappedLabel(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return label
}

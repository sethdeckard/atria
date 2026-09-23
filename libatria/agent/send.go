package agent

import (
	"strings"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

const (
	// SubmitDelay is the pause between a prompt's text and the carriage
	// return that submits it.
	SubmitDelay = 50 * time.Millisecond
	// CopilotRuneDelay is the pause between runes when typing a prompt into
	// GitHub Copilot one rune at a time.
	CopilotRuneDelay = 5 * time.Millisecond
)

// copilotNewlines flattens line breaks: Copilot's input treats Enter as a
// newline rather than submit, and a newline opens its slash-command menu.
var copilotNewlines = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// SendPrompt types text into the agent's session and submits it. The text
// goes in one write, then after SubmitDelay a carriage return goes in a
// second write, because the raw-mode TUIs these agents run drop a trailing
// newline that arrives with the text. Copilot gets its own path: line breaks
// become spaces, the text is trimmed, and each rune is written on its own
// with CopilotRuneDelay between them, because Copilot drops the Enter when
// bulk text lands in a short terminal.
func SendPrompt(b terminal.Backend, sessionID, text string, t Type) error {
	if t == Copilot {
		prompt := strings.TrimSpace(copilotNewlines.Replace(text))
		for _, r := range prompt {
			if err := b.SendText(sessionID, string(r)); err != nil {
				return err
			}
			sleep(CopilotRuneDelay)
		}
		sleep(SubmitDelay)
		return b.SendText(sessionID, "\r")
	}
	if err := b.SendText(sessionID, text); err != nil {
		return err
	}
	sleep(SubmitDelay)
	return b.SendText(sessionID, "\r")
}

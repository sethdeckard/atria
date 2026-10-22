package agent

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

// closeMention adds a space to text whose last whitespace-separated word
// starts with $ or @, unless the text already ends in whitespace. While
// Codex's composer ends in such a word, it shows its skill ($) or file (@)
// popup, and Enter there inserts the highlighted item instead of
// submitting; a space closes the popup. Other agents get the space too,
// where it ends up harmlessly in the prompt. The check is on the first
// character alone, so "costs $100" gets the space as well.
func closeMention(text string) string {
	if r, _ := utf8.DecodeLastRuneInString(text); text == "" || unicode.IsSpace(r) {
		return text
	}
	fields := strings.Fields(text)
	if last := fields[len(fields)-1]; strings.HasPrefix(last, "$") || strings.HasPrefix(last, "@") {
		return text + " "
	}
	return text
}

// SendPrompt types text into the agent's session and submits it. The text
// goes in one write, then after SubmitDelay a carriage return goes in a
// second write, because the raw-mode TUIs these agents run drop a trailing
// newline that arrives with the text. Text whose last word starts with $ or
// @ gets a space first, so the CR submits rather than picking from a
// mention popup (see closeMention). Copilot gets its own path: line breaks
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
	if err := b.SendText(sessionID, closeMention(text)); err != nil {
		return err
	}
	sleep(SubmitDelay)
	return b.SendText(sessionID, "\r")
}

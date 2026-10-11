package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// choice is an answer to one of vuka new's questions.
type choice struct {
	key, label, hint string
}

// prompter asks questions on a terminal: with keys (the terminal in raw
// mode) the arrow keys move between the choices and Enter takes one; else
// a numbered list and a line read back.
type prompter struct {
	in   *bufio.Reader
	out  io.Writer
	keys bool
}

func newPrompter(in io.Reader, out io.Writer, keys bool) *prompter {
	return &prompter{in: bufio.NewReader(in), out: out, keys: keys}
}

// errInterrupted is Ctrl-C or Ctrl-D at a question.
var errInterrupted = errors.New("vuka new: interrupted")

const (
	bold  = "\x1b[1m"
	dim   = "\x1b[2m"
	cyan  = "\x1b[36m"
	reset = "\x1b[0m"
)

// choose asks question and returns the index of the choice taken; def is
// the one Enter takes at once.
func (p *prompter) choose(question string, choices []choice, def int) (int, error) {
	if !p.keys {
		return p.chooseLine(question, choices, def)
	}
	at := def
	draw := func(first bool) {
		if !first {
			fmt.Fprintf(p.out, "\x1b[%dA", len(choices)+1) // back to the question
		}
		fmt.Fprintf(p.out, "\r\x1b[2K%s?%s %s%s%s %s(↑↓, enter)%s\r\n", cyan, reset, bold, question, reset, dim, reset)
		for i, c := range choices {
			mark, label := "  ", c.label
			if i == at {
				mark, label = cyan+"› "+reset, cyan+c.label+reset
			}
			hint := ""
			if c.hint != "" {
				hint = " " + dim + "· " + c.hint + reset
			}
			fmt.Fprintf(p.out, "\r\x1b[2K  %s%s%s\r\n", mark, label, hint)
		}
	}
	draw(true)
	for {
		k, err := p.key()
		if err != nil {
			return 0, err
		}
		switch k {
		case "up", "k":
			at = (at + len(choices) - 1) % len(choices)
		case "down", "j", "tab":
			at = (at + 1) % len(choices)
		case "enter":
			// Fold the list into the answer.
			fmt.Fprintf(p.out, "\x1b[%dA\r\x1b[J%s?%s %s%s%s %s›%s %s\r\n", len(choices)+1, cyan, reset, bold, question, reset, dim, reset, choices[at].label)
			return at, nil
		case "interrupt":
			return 0, errInterrupted
		default:
			if n, err := strconv.Atoi(k); err == nil && n >= 1 && n <= len(choices) {
				at = n - 1
			}
		}
		draw(false)
	}
}

// key reads one key press: "up", "down", "enter", "tab", "interrupt", or
// the character typed.
func (p *prompter) key() (string, error) {
	b, err := p.in.ReadByte()
	if err != nil {
		return "", errInterrupted
	}
	switch b {
	case 3, 4: // Ctrl-C, Ctrl-D
		return "interrupt", nil
	case '\r', '\n':
		return "enter", nil
	case '\t':
		return "tab", nil
	case 0x1b:
		// An arrow is ESC [ A (or ESC O A); a lone ESC is ignored.
		if p.in.Buffered() == 0 {
			return "esc", nil
		}
		b2, _ := p.in.ReadByte()
		if b2 != '[' && b2 != 'O' {
			return "esc", nil
		}
		b3, _ := p.in.ReadByte()
		switch b3 {
		case 'A':
			return "up", nil
		case 'B':
			return "down", nil
		}
		return "esc", nil
	}
	return string(rune(b)), nil
}

// chooseLine asks with a numbered list: a number, a choice's key or
// label, or an empty line for the default.
func (p *prompter) chooseLine(question string, choices []choice, def int) (int, error) {
	for {
		fmt.Fprintf(p.out, "? %s\n", question)
		for i, c := range choices {
			mark := " "
			if i == def {
				mark = "*"
			}
			hint := ""
			if c.hint != "" {
				hint = " (" + c.hint + ")"
			}
			fmt.Fprintf(p.out, "  %s %d. %s%s\n", mark, i+1, c.label, hint)
		}
		fmt.Fprintf(p.out, "  Choose 1-%d [%d]: ", len(choices), def+1)
		line, err := p.in.ReadString('\n')
		if err != nil && line == "" {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(p.out)
				return def, nil // input ran out: the default
			}
			return 0, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" {
			return def, nil
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(choices) {
			return n - 1, nil
		}
		for i, c := range choices {
			if answer == c.key || answer == strings.ToLower(c.label) {
				return i, nil
			}
		}
		fmt.Fprintf(p.out, "  %q isn't one of the choices.\n", answer)
	}
}

// note shows an answer the prompter didn't ask for.
func (p *prompter) note(question, answer string) {
	if p.keys {
		fmt.Fprintf(p.out, "%s?%s %s%s%s %s› %s%s\r\n", cyan, reset, bold, question, reset, dim, answer, reset)
		return
	}
	fmt.Fprintf(p.out, "? %s: %s\n", question, answer)
}

// close ends the questions with a blank line.
func (p *prompter) close() {
	if p.keys {
		fmt.Fprint(p.out, "\r\n")
		return
	}
	fmt.Fprintln(p.out)
}

// interactive reports whether vuka new may ask: stdin and stdout are a
// terminal. Elsewhere (a pipe, CI, tests) it takes the defaults and flags
// and never waits for input.
func interactive(in io.Reader, out io.Writer) bool {
	fi, ok := in.(*os.File)
	fo, ok2 := out.(*os.File)
	return ok && ok2 && isTerminal(fi) && isTerminal(fo)
}

// rawTerminal puts the terminal in raw mode for the arrow keys; restore
// puts it back. An error leaves it as it was (questions are then lines).
func rawTerminal(in io.Reader) (restore func(), err error) {
	f, ok := in.(*os.File)
	if !ok {
		return nil, errors.New("not a terminal")
	}
	return makeRaw(f)
}

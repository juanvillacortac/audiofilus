package ui

import "fmt"

// El teclado del sistema (en el móvil): cuando se escribe (la búsqueda, el
// nombre de una lista, la dirección o la contraseña de un servidor), la app
// abre el teclado de verdad en vez del de pantalla. Swift tiene un campo de
// texto invisible: ShowKeyboard lo enfoca con el texto de ahora, cada cambio
// vuelve con SetKeyboardText, Intro con KeyboardDone y cerrarlo con
// KeyboardClosed.

// ShowKeyboard, si está, abre (show) o cierra el teclado del sistema, con el
// texto que ya hay y si es una contraseña. Lo pone el móvil.
var ShowKeyboard func(show bool, text string, secret bool)

// nativeKeyboard dice si se escribe con el teclado del sistema.
func (a *App) nativeKeyboard() bool { return ShowKeyboard != nil }

// kbEvent es lo que llega del teclado del sistema (desde otro hilo).
type kbEvent struct {
	text   *string // el texto entero, tras un cambio
	done   bool    // Intro
	closed bool    // se cerró (el usuario lo bajó)
}

// SetKeyboardText pone el texto que se escribe (lo llama el teclado del
// sistema, desde otro hilo: se aplica en el siguiente Update).
func (a *App) SetKeyboardText(s string) { a.kbEvents <- kbEvent{text: &s} }

// KeyboardDone es Intro en el teclado del sistema.
func (a *App) KeyboardDone() { a.kbEvents <- kbEvent{done: true} }

// KeyboardClosed es que el usuario bajó el teclado sin Intro.
func (a *App) KeyboardClosed() { a.kbEvents <- kbEvent{closed: true} }

// typingField dice qué se está escribiendo ahora: el texto, si es una
// contraseña y si hay algo que escribir.
func (a *App) typingField() (text string, secret, ok bool) {
	switch {
	case a.prompt != nil:
		return string(a.prompt.text), a.prompt.secret, true
	case a.screen == LibraryScreen && a.libTab == tabSearch && a.search.typing && !a.tabFocus && a.menu == nil:
		return string(a.search.query), false, true
	}
	return "", false, false
}

// updateKeyboard abre o cierra el teclado del sistema según haga falta y
// aplica lo que llega de él. Va en cada Update.
func (a *App) updateKeyboard() {
	if !a.nativeKeyboard() {
		return
	}
	for {
		select {
		case ev := <-a.kbEvents:
			a.applyKeyboard(ev)
			continue
		default:
		}
		break
	}
	text, secret, want := a.typingField()
	field := ""
	if want {
		field = "search"
		if a.prompt != nil {
			field = fmt.Sprintf("prompt %p", a.prompt) // otro diálogo, otro texto
		}
	}
	if field != a.kbField {
		a.kbField = field
		go ShowKeyboard(want, text, secret)
	}
}

// applyKeyboard aplica un evento del teclado al campo que se escribe.
func (a *App) applyKeyboard(ev kbEvent) {
	switch {
	case a.prompt != nil:
		p := a.prompt
		switch {
		case ev.text != nil:
			p.text, p.typed = []rune(*ev.text), true
		case ev.done:
			a.promptAccept = true // updatePrompt lo acepta
		case ev.closed:
			a.prompt = nil
		}
	case a.search.typing:
		s := &a.search
		switch {
		case ev.text != nil:
			s.query = []rune(*ev.text)
			if len(s.query) > 40 {
				s.query = s.query[:40]
			}
		case ev.done:
			a.searchDone()
		case ev.closed:
			s.typing = false
		}
	}
}

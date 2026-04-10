// Package dialog provides a dialog overlay system ported from Crush.
package dialog

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// Action is returned by Dialog.HandleMsg to tell the overlay what to do.
type Action interface {
	isAction()
}

// CloseAction closes the dialog.
type CloseAction struct{}

func (CloseAction) isAction() {}

// NoneAction does nothing.
type NoneAction struct{}

func (NoneAction) isAction() {}

// Dialog is a component displayed on top of the UI.
type Dialog interface {
	ID() string
	HandleMsg(msg tea.Msg) Action
	Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor
}

// Overlay manages a stack of dialogs.
type Overlay struct {
	dialogs []Dialog
}

// NewOverlay creates a new Overlay.
func NewOverlay() *Overlay {
	return &Overlay{}
}

// HasDialogs returns true if any dialogs are open.
func (d *Overlay) HasDialogs() bool {
	return len(d.dialogs) > 0
}

// OpenDialog pushes a dialog onto the stack.
func (d *Overlay) OpenDialog(dlg Dialog) {
	d.dialogs = append(d.dialogs, dlg)
}

// CloseDialog removes a dialog by ID.
func (d *Overlay) CloseDialog(dialogID string) {
	for i, dlg := range d.dialogs {
		if dlg.ID() == dialogID {
			d.dialogs = append(d.dialogs[:i], d.dialogs[i+1:]...)
			return
		}
	}
}

// CloseFrontDialog removes the topmost dialog.
func (d *Overlay) CloseFrontDialog() {
	if len(d.dialogs) == 0 {
		return
	}
	d.dialogs = d.dialogs[:len(d.dialogs)-1]
}

// Front returns the topmost dialog, or nil.
func (d *Overlay) Front() Dialog {
	if len(d.dialogs) == 0 {
		return nil
	}
	return d.dialogs[len(d.dialogs)-1]
}

// Update dispatches a message to the topmost dialog.
func (d *Overlay) Update(msg tea.Msg) Action {
	if len(d.dialogs) == 0 {
		return NoneAction{}
	}
	return d.dialogs[len(d.dialogs)-1].HandleMsg(msg)
}

// Draw renders all dialogs in stack order (bottom to top).
func (d *Overlay) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	var cur *tea.Cursor
	for _, dlg := range d.dialogs {
		cur = dlg.Draw(scr, area)
	}
	return cur
}

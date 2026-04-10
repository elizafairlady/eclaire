package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

type testDialog struct {
	id string
}

func (d *testDialog) ID() string                                      { return d.id }
func (d *testDialog) HandleMsg(msg tea.Msg) Action                    { return NoneAction{} }
func (d *testDialog) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor { return nil }

func TestOverlayEmpty(t *testing.T) {
	o := NewOverlay()
	if o.HasDialogs() {
		t.Error("new overlay should have no dialogs")
	}
	if o.Front() != nil {
		t.Error("Front() should be nil")
	}
}

func TestOverlayOpenClose(t *testing.T) {
	o := NewOverlay()

	o.OpenDialog(&testDialog{id: "dlg-1"})
	if !o.HasDialogs() {
		t.Error("should have dialogs after open")
	}
	if o.Front().ID() != "dlg-1" {
		t.Errorf("Front().ID() = %q", o.Front().ID())
	}

	o.OpenDialog(&testDialog{id: "dlg-2"})
	if o.Front().ID() != "dlg-2" {
		t.Error("front should be most recent")
	}

	o.CloseDialog("dlg-1")
	if o.Front().ID() != "dlg-2" {
		t.Error("closing dlg-1 should leave dlg-2 as front")
	}

	o.CloseFrontDialog()
	if o.HasDialogs() {
		t.Error("all dialogs should be closed")
	}
}

func TestOverlayUpdate(t *testing.T) {
	o := NewOverlay()

	// Update with no dialogs should return NoneAction
	action := o.Update(tea.KeyPressMsg{})
	if _, ok := action.(NoneAction); !ok {
		t.Error("empty overlay Update should return NoneAction")
	}

	o.OpenDialog(&testDialog{id: "dlg"})
	action = o.Update(tea.KeyPressMsg{})
	if _, ok := action.(NoneAction); !ok {
		t.Error("test dialog should return NoneAction")
	}
}

func TestOverlayCloseFrontEmpty(t *testing.T) {
	o := NewOverlay()
	o.CloseFrontDialog() // should not panic
}

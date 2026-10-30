package main

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

var fontDialogScope = tree.ID(shell, "font-dialog-scope")
var writeDialogScope = tree.ID(shell, "write-dialog-scope")

func (m *model) textInput(ctx app.TextInputContext) *app.TextInput {
	input := flex.Fields(&m.query, &m.writeField, &m.fontFilter)(ctx)
	if input != nil {
		input.NavigationKeys = ctx.ID == fontFilterID
	}
	return input
}

func (m *model) composing(id ui.ID) bool {
	input := m.textInput(app.TextInputContext{ID: id})
	if input == nil || input.Editor == nil {
		return false
	}
	_, active := input.Editor.Composition()
	return active
}

func (m *model) chooseFont(name string) {
	if name == "默认字体" {
		name = ""
	}
	m.settings.Appearance.FontFamily = name
	m.fontCache = nil
	m.fontInk = nil
	m.fontUsed = nil
	m.fontWidths = nil
	m.fontBytes = 0
	m.fontListOpen = false
	m.saveSettings()
}

func modalFocus(c *ui.Canvas, scope, initial, dismiss ui.ID) {
	c.PushFocusScope(scope)
	c.TrapFocus(scope)
	c.SetInitialFocus(initial)
	c.SetEscapeTarget(dismiss)
}

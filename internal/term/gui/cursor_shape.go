// Copyright (C) 2017-2026 The Rune Authors
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or (at
// your option) any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package gui

import (
	"fmt"
	"sync"

	ebiten "github.com/hajimehoshi/ebiten/v2"
	"unstable.build/rune/internal/handler"
)

// cursorShapeArbiter has a single winningCursorMessage field that is
// write-locked from multiple callers by mu.
type cursorShapeArbiter struct {
	mu				   sync.Mutex
	winningCursorMessage CursorShapeMessage
}

var (
	cursorShapeArbiterOnce            sync.Once
	cursorShapeArbiterInstance cursorShapeArbiter
)

// CursorShapeArbiter returns the same instance to callers.
func CursorShapeArbiter() *cursorShapeArbiter {
	cursorShapeArbiterOnce.Do(func() {
		cursorShapeArbiterInstance = cursorShapeArbiter{}
	})
	return &cursorShapeArbiterInstance
}

// CursorShapeMessage defines a message that is sent to the cursorShapeArbiter.
type CursorShapeMessage struct {
	object      ObjectUnderCursor
	cursorShape ebiten.CursorShapeType
}

// ObjectUnderCursor indicates what a graphical component believes is under
// the cursor; it may have false beliefs, meaning there may be other objects
// under the cursor that it doesn't know about.
// ObjectUnderCursors are usually represent GUI objects, like links, scrollbars,
// and resize borders. However, there are 3 special values that do not represent
// GUI objects: none, Empty, and NotInWindow.
type ObjectUnderCursor uint8

const (
	// none is a special value only used for a new and empty CursorShapeMessage.
	none ObjectUnderCursor = iota
	// Empty is a special value representing a cell that has no object.
	Empty
	// NotInWindow is a special value used when the cursor is outside a window.
	NotInWindow
	// Link represents a link
	Link
	// ScrollBar represents a scroll bar
	ScrollBar
	// ResizeBorder represents a resize border
	ResizeBorder
	// CloseButton represents a window's close button
	CloseButton
)

// SetCursorShape is used by multiple graphical components to defer the decision
// of which cursor shape to set to the Arbiter.
// The Arbtier reviews the message and decides whether to set the message's
// cursor shape or to set the lastWinningMsg's cursor shape.
// The cursor positions of the lastWinningMsg and of the incoming msg may be
// different.
// The Arbiter assumes that there is only one object under the cursor at a time,
// so actually the arbiter doesn't need to differentiate between the different
// kinds of objects.
func (a *cursorShapeArbiter) SetCursorShape(msg CursorShapeMessage) {
	// Prevent write data corruption
	a.mu.Lock()
	defer a.mu.Unlock()

	// Consider the previously winning message
	switch a.winningCursorMessage.object {
	case none:
		// There is no winning message yet, so the incoming msg auto wins
		a.winningCursorMessage = msg

	case Empty:
		// The previously winning graphical component sent an Empty object because
		// it believes there is truly nothing in its cell. However, it may not know
		// about other components that may have objects at that cell. Those
		// components win instead.
		// We then only need to consider the special objects: none cannot be sent
		// as it is not exported, NotInWindow wins, and Empty wins (arbitrary tie).
		// In every case, the incoming message wins.
		a.winningCursorMessage = msg

	default:
		// Here the previously winning message is an actual object that exists, like a
		// Link, a ResizeBorder, or a ScrollBar.
		// Consider what the incoming object is:
		switch msg.object {
		case Empty:
			// Current Empty object wins against a previous actual object, this is what
			// graphical components use to reset the cursor.
			a.winningCursorMessage = msg

		case NotInWindow:
			// A graphical component sent an NotInWindow object because the mouse went
			// outside a window. There is no contest here because there are no
			// objects outside windows, or at least the arbitrator assumes so.
			// NotInWindow wins.
			a.winningCursorMessage = msg

		default:
			// All other objects overwrite the winner. This is not a problem if there
			// is only one object under the cursor at a time.
			a.winningCursorMessage = msg
		}
	}
	
	fmt.Printf("DEBUG: winning: object=%v, cursorShape=%v\n",
                       a.winningCursorMessage.object, 
                       a.winningCursorMessage.cursorShape)

	ebiten.SetCursorShape(a.winningCursorMessage.cursorShape)
}

// ResizeBorderHandler satisfies handler.ResizeBorderInterface.
type ResizeBorderHandler struct {
	// lastCursorShape tracks what the ResizeBorderHandler's last cursor shape was.
	// This is internal to ResizeBorderHandler, it does not track cursor shapes set by
	// other graphical components.
	lastCursorShape ebiten.CursorShapeType
}

// setIfChanged is a helper function to set the cursor shape only if the
// newShape is different from the lastCursorShape.
func (h *ResizeBorderHandler) setIfChanged(object ObjectUnderCursor, newShape ebiten.CursorShapeType) {
	if newShape != h.lastCursorShape {
		h.lastCursorShape = newShape
		CursorShapeArbiter().SetCursorShape(CursorShapeMessage{object, newShape})
	}
}

// OnMouseover sets the ebiten.CursorShape when the cursor is over
// handler.MouseoverArea.
// It tracks the last cursor shape and only sets the cursor shape when it's different.
// hasWindowBar indicates whether the window has a bar.
func (h *ResizeBorderHandler) OnMouseover(area handler.MouseoverArea, hasWindowBar bool) {
	// If mouse is not over any area, then it must be outside the window.
	// Send a NotInWindow message to the CursorShapeArbiter to reset the cursor shape.
	if area == 0 {
		h.setIfChanged(NotInWindow, ebiten.CursorShapeDefault)
	}
	left := area&handler.FrameLeft != 0
	right := area&handler.FrameRight != 0
	top := area&handler.FrameTop != 0
	bottom := area&handler.FrameBottom != 0
	center := area&handler.FrameCenter != 0
	switch {
	case (left && top) || (right && bottom):
		h.setIfChanged(ResizeBorder, ebiten.CursorShapeNWSEResize)
	case (right && top) || (left && bottom):
		h.setIfChanged(ResizeBorder, ebiten.CursorShapeNESWResize)
	case left || right:
		h.setIfChanged(ResizeBorder, ebiten.CursorShapeEWResize)
	case bottom || (!hasWindowBar && top):
		h.setIfChanged(ResizeBorder, ebiten.CursorShapeNSResize)
	case center:
		// If area has FrameCenter, the cell under the cursor "has an Empty object";
		// there could be other objects in the "Empty" cell, but the ResizeBorderHandler
		// doesn't know about them.
		h.setIfChanged(Empty, ebiten.CursorShapeDefault)
	case area&handler.WindowBarCloseIcon != 0:
		// If area has WindowBarCloseIcon, the cell under the cursor is a close button.
		h.setIfChanged(CloseButton, ebiten.CursorShapeDefault)
	}
}

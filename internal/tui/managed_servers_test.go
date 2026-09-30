package tui

import (
	"context"
	"testing"
)

func TestManagedOperationIsDeferredAndDoesNotReplaceNewOverlay(t *testing.T) {
	a := &App{ctx: context.Background()}
	ran := false
	a.managedOperation("Test status", func(context.Context) (string, error) {
		ran = true
		return "Healthy", nil
	})
	if ran || !a.managedServerBusy {
		t.Fatal("server work must run as an asynchronous command")
	}
	var result managedServerResult
	for _, cmd := range a.pendingCmds {
		if msg, ok := cmd().(managedServerResult); ok {
			result = msg
		}
	}
	if !ran || result.reader == nil {
		t.Fatal("server work was not queued")
	}
	newOverlay := &textReader{title: "Another panel"}
	a.overlay = newOverlay
	a.finishManagedOperation(result)
	if a.managedServerBusy || a.overlay != newOverlay || result.reader.body != "Healthy" {
		t.Fatal("completion stole focus or lost its result")
	}
}

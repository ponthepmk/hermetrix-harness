//go:build windows

package product

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeDesktopListsAndObservesBoundWindow(t *testing.T) {
	service, _, _ := testProductService(t)
	windows, err := service.ListDesktopWindows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) == 0 {
		t.Skip("no visible interactive Windows window in this test session")
	}
	for _, window := range windows {
		if window.Handle == 0 || window.ProcessID < 1 || window.Title == "" {
			t.Fatalf("invalid window identity: %+v", window)
		}
	}
	observation, err := service.ObserveDesktopWindow(context.Background(), windows[0].Handle, windows[0].ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Window.Handle != windows[0].Handle || observation.Window.ProcessID != windows[0].ProcessID {
		t.Fatalf("observed a different target: %+v", observation.Window)
	}
	if _, err := service.ObserveDesktopWindow(context.Background(), windows[0].Handle, windows[0].ProcessID+1); err == nil {
		t.Fatal("changed process identity was accepted")
	}
}

func TestNativeDesktopActionUsesExactApprovalAndDoesNotReplay(t *testing.T) {
	script := `Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$form = New-Object System.Windows.Forms.Form
$form.Text = 'Hermetrix native fixture'
$form.Width = 300
$form.Height = 160
$button = New-Object System.Windows.Forms.Button
$button.Name = 'fixtureButton'
$button.Text = 'Apply fixture'
$button.Width = 130
$button.Height = 40
$button.Left = 20
$button.Top = 20
$button.Add_Click({ $form.Text = 'Hermetrix native fixture clicked' })
$form.Controls.Add($button)
$input = New-Object System.Windows.Forms.TextBox
$input.Name = 'fixtureInput'
$input.AccessibleName = 'Fixture input'
$input.Width = 170
$input.Left = 20
$input.Top = 72
$form.Controls.Add($input)
$form.Show()
[Console]::WriteLine("$PID|$($form.Handle.ToInt64())")
[System.Windows.Forms.Application]::Run($form)`
	path := filepath.Join(t.TempDir(), "desktop-fixture.ps1")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-File", path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	var identityLine string
	select {
	case identityLine = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("fixture window did not start")
	}
	parts := strings.Split(identityLine, "|")
	if len(parts) != 2 {
		t.Fatalf("fixture identity=%q", identityLine)
	}
	processID, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	handle, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	var observation DesktopObservation
	for attempt := 0; attempt < 10; attempt++ {
		observation, err = service.ObserveDesktopWindow(ctx, handle, processID)
		if err == nil && len(observation.Elements) > 0 && observation.ScreenshotArtifactID != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil || observation.ScreenshotArtifactID == "" {
		t.Fatalf("fixture observation=%+v err=%v", observation, err)
	}
	ref := 0
	for _, element := range observation.Elements {
		if element.Name == "Apply fixture" {
			ref = element.Ref
			break
		}
	}
	if ref == 0 {
		t.Fatalf("fixture button missing: %+v", observation.Elements)
	}
	editRef := 0
	for _, element := range observation.Elements {
		// WinForms sizes are scaled by the Windows display DPI. Identify the
		// edit control by its native class instead of a 170-pixel fixture width.
		if element.Ref != ref && strings.HasPrefix(element.NativeClass, "WindowsForms10.EDIT.") {
			editRef = element.Ref
			break
		}
	}
	if editRef == 0 {
		t.Fatalf("fixture input missing: %+v", observation.Elements)
	}
	edit := DesktopActionInput{Handle: handle, ProcessID: processID, Action: "set_value", ElementRef: editRef,
		Text: "Reviewed text", ObservationHash: observation.ObservationHash}
	editPrepared, err := service.PrepareDesktopAction(ctx, edit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmDesktopAction(ctx, editPrepared.ID, editPrepared.SubjectHash, "different text", "tester"); err == nil {
		t.Fatal("changed text was accepted after review")
	}
	editResult, err := service.ConfirmDesktopAction(ctx, editPrepared.ID, editPrepared.SubjectHash, edit.Text, "tester")
	if err != nil || editResult.State != "completed" || editResult.Observation == nil {
		t.Fatalf("set_value result=%+v err=%v", editResult, err)
	}
	observation = *editResult.Observation
	input := DesktopActionInput{Handle: handle, ProcessID: processID, Action: "invoke", ElementRef: ref, ObservationHash: observation.ObservationHash}
	prepared, err := service.PrepareDesktopAction(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmDesktopAction(ctx, prepared.ID, "wrong-hash", "", "tester"); err == nil {
		t.Fatal("wrong approval hash was accepted")
	}
	cancelled, err := service.CancelDesktopAction(ctx, prepared.ID, prepared.SubjectHash)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	if _, err := service.ConfirmDesktopAction(ctx, prepared.ID, prepared.SubjectHash, "", "tester"); err == nil {
		t.Fatal("cancelled approval was replayed")
	}
	prepared, err = service.PrepareDesktopAction(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.ConfirmDesktopAction(ctx, prepared.ID, prepared.SubjectHash, "", "tester")
	if err != nil || completed.State != "completed" || completed.Observation == nil || !strings.Contains(completed.Observation.Window.Title, "clicked") {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if _, err := service.ConfirmDesktopAction(ctx, prepared.ID, prepared.SubjectHash, "", "tester"); err == nil {
		t.Fatal("completed action was replayed")
	}
	if _, err := service.PrepareDesktopAction(ctx, input); err == nil {
		t.Fatal("stale observation allowed a second action")
	}
	if _, err := dataStore.DB.ExecContext(ctx, `UPDATE desktop_actions SET state='dispatched' WHERE id=?`, prepared.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecoverInterruptedJobs(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.GetDesktopAction(ctx, prepared.ID)
	if err != nil || recovered.State != "uncertain" {
		t.Fatalf("interrupted native action was not marked uncertain: %+v err=%v", recovered, err)
	}
	current, err := service.ObserveDesktopWindow(ctx, handle, processID)
	if err != nil || current.ScreenshotArtifactID == "" {
		t.Fatalf("cannot inspect interrupted action: %+v err=%v", current, err)
	}
	if _, err := service.PrepareDesktopAction(ctx, DesktopActionInput{Handle: handle, ProcessID: processID, Action: "invoke", ElementRef: ref, ObservationHash: current.ObservationHash}); err == nil {
		t.Fatal("unresolved Windows action allowed another action on the same window")
	}
	if _, err := service.ReconcileDesktopAction(ctx, prepared.ID, "observed_applied", "stale-hash", "tester", "fixture title is clicked"); err == nil {
		t.Fatal("stale observation was accepted for reconciliation")
	}
	var reconciled DesktopAction
	for attempt := 0; attempt < 5; attempt++ {
		// The focused WinForms input can blink between the two screenshots used
		// for exact observation binding. A changed frame must be inspected again;
		// it must never authorize the uncertain action with the stale hash.
		current, err = service.ObserveDesktopWindow(ctx, handle, processID)
		if err != nil {
			break
		}
		reconciled, err = service.ReconcileDesktopAction(ctx, prepared.ID, "observed_applied", current.ObservationHash, "tester", "fixture title is clicked")
		if err == nil || !strings.Contains(err.Error(), "desktop observation changed or has no screenshot") {
			break
		}
	}
	if err != nil || reconciled.Reconciliation == nil || reconciled.Reconciliation.ScreenshotArtifactID == "" {
		t.Fatalf("interrupted action was not reconciled with a screenshot: %+v err=%v", reconciled, err)
	}
	if _, err := service.ReconcileDesktopAction(ctx, prepared.ID, "observed_not_applied", current.ObservationHash, "tester", "second decision"); err == nil {
		t.Fatal("same uncertain action was reconciled twice")
	}
	again, err := service.PrepareDesktopAction(ctx, DesktopActionInput{Handle: handle, ProcessID: processID, Action: "invoke", ElementRef: ref, ObservationHash: current.ObservationHash})
	if err != nil {
		t.Fatalf("reconciled window remained blocked: %v", err)
	}
	if _, err := service.CancelDesktopAction(ctx, again.ID, again.SubjectHash); err != nil {
		t.Fatal(err)
	}
}

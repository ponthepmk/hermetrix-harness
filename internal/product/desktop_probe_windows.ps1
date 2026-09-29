$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName System.Drawing
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class HermetrixDesktopNative {
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr hWnd, StringBuilder className, int maxCount);
    [DllImport("user32.dll", EntryPoint="GetWindowLongW")] public static extern int GetWindowLong(IntPtr hWnd, int index);
    [DllImport("user32.dll", SetLastError=true)] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdcBlt, uint flags);
    [DllImport("user32.dll", SetLastError=true)] public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint message,
        IntPtr wParam, IntPtr lParam, uint flags, uint timeoutMs, out IntPtr result);
    [DllImport("user32.dll", EntryPoint="SendMessageTimeoutW", CharSet=CharSet.Unicode, SetLastError=true)] public static extern IntPtr SendMessageTimeoutText(IntPtr hWnd, uint message,
        IntPtr wParam, string lParam, uint flags, uint timeoutMs, out IntPtr result);
    [DllImport("user32.dll", EntryPoint="SendMessageTimeoutW", CharSet=CharSet.Unicode, SetLastError=true)] public static extern IntPtr SendMessageTimeoutTextRead(IntPtr hWnd, uint message,
        IntPtr wParam, StringBuilder lParam, uint flags, uint timeoutMs, out IntPtr result);
}
'@

$request = [Console]::In.ReadToEnd() | ConvertFrom-Json
$root = [System.Windows.Automation.AutomationElement]::RootElement

function SafeText($value, [int]$limit = 300) {
    $text = [string]$value
    if ($text.Length -gt $limit) { return $text.Substring(0, $limit) }
    return $text
}

function RectangleData($rectangle) {
    return @{ x = [int]$rectangle.X; y = [int]$rectangle.Y; width = [int]$rectangle.Width; height = [int]$rectangle.Height }
}

function WindowData($element) {
    $current = $element.Current
    return @{ handle = [int64]$current.NativeWindowHandle; process_id = [int]$current.ProcessId;
        title = (SafeText $current.Name 240); bounds = (RectangleData $current.BoundingRectangle) }
}

if ($request.mode -eq 'list') {
    $windows = @()
    $children = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
    foreach ($element in $children) {
        try {
            $item = WindowData $element
            if ($item.handle -ne 0 -and $item.process_id -gt 0 -and $item.title -ne '') { $windows += $item }
        } catch { }
        if ($windows.Count -ge 80) { break }
    }
    @{ windows = $windows } | ConvertTo-Json -Compress -Depth 8
    exit 0
}

if (($request.mode -ne 'observe' -and $request.mode -ne 'action') -or [int64]$request.handle -eq 0) { throw 'unsupported desktop probe request' }
$window = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]([int64]$request.handle))
if ($null -eq $window) { throw 'window is unavailable' }
$identity = WindowData $window
if ($identity.handle -ne [int64]$request.handle -or $identity.process_id -ne [int]$request.process_id) { throw 'window identity changed' }
$elements = @()
$elementObjects = @()
$queue = New-Object System.Collections.Queue
$queue.Enqueue(@($window, 0))
while ($queue.Count -gt 0 -and $elements.Count -lt 120) {
    $pair = $queue.Dequeue()
    $parent = $pair[0]
    $depth = [int]$pair[1]
    if ($depth -ge 4) { continue }
    try { $children = $parent.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition) }
    catch { continue }
    foreach ($element in $children) {
        try {
            $current = $element.Current
            $nativeClass = ''
            if ([int64]$current.NativeWindowHandle -ne 0) {
                $classBuffer = New-Object System.Text.StringBuilder 256
                [void][HermetrixDesktopNative]::GetClassName([IntPtr]([int64]$current.NativeWindowHandle), $classBuffer, 256)
                $nativeClass = SafeText $classBuffer.ToString() 120
            }
            $item = @{ ref = $elements.Count + 1; name = (SafeText $current.Name); handle = [int64]$current.NativeWindowHandle;
                automation_id = (SafeText $current.AutomationId 120); control_type = (SafeText $current.ControlType.ProgrammaticName 100);
                native_class = $nativeClass;
                enabled = [bool]$current.IsEnabled; is_password = [bool]$current.IsPassword;
                bounds = (RectangleData $current.BoundingRectangle); value = '' }
            if (-not $item.is_password) {
                try {
                    $valuePattern = [System.Windows.Automation.ValuePattern]$element.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
                    $item.value = SafeText $valuePattern.Current.Value
                } catch { }
            }
            if ($nativeClass -match '(^|\.)EDIT(\.|$)' -and $item.handle -ne 0) {
                $editHandle = [IntPtr]([int64]$item.handle)
                $style = [HermetrixDesktopNative]::GetWindowLong($editHandle, -16)
                if (($style -band 0x20) -ne 0) { $item.is_password = $true; $item.value = '' }
                else {
                    $buffer = New-Object System.Text.StringBuilder 301
                    $readResult = [IntPtr]::Zero
                    $read = [HermetrixDesktopNative]::SendMessageTimeoutTextRead($editHandle, 0x000D, [IntPtr]301,
                        $buffer, 2, 1000, [ref]$readResult)
                    if ($read -ne [IntPtr]::Zero) { $item.value = SafeText $buffer.ToString() }
                }
            }
            $elements += $item
            $elementObjects += $element
            if ($depth -lt 3) { $queue.Enqueue(@($element, $depth + 1)) }
        } catch { }
        if ($elements.Count -ge 120) { break }
    }
}
if ($request.mode -eq 'action') {
    $ref = [int]$request.element_ref
    if ($ref -lt 1 -or $ref -gt $elements.Count) { throw 'desktop element reference is stale' }
    $actual = $elements[$ref - 1]
    $expected = $request.target
    if ($actual.name -ne [string]$expected.name -or $actual.automation_id -ne [string]$expected.automation_id -or
        $actual.control_type -ne [string]$expected.control_type -or $actual.native_class -ne [string]$expected.native_class -or
        $actual.handle -ne [int64]$expected.handle -or
        $actual.value -ne [string]$expected.value -or $actual.is_password -ne [bool]$expected.is_password -or $actual.bounds.x -ne [int]$expected.bounds.x -or
        $actual.bounds.y -ne [int]$expected.bounds.y -or $actual.bounds.width -ne [int]$expected.bounds.width -or
        $actual.bounds.height -ne [int]$expected.bounds.height -or -not $actual.enabled) { throw 'desktop element changed after review' }
    $target = $elementObjects[$ref - 1]
    if ($request.action -eq 'invoke') {
        try {
            $pattern = [System.Windows.Automation.InvokePattern]$target.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
            $pattern.Invoke()
        } catch {
            if ($actual.handle -eq 0) { throw 'invoke pattern is unavailable and the element has no native handle' }
            $targetHandle = [IntPtr]([int64]$actual.handle)
            $targetPID = [uint32]0
            [void][HermetrixDesktopNative]::GetWindowThreadProcessId($targetHandle, [ref]$targetPID)
            if ($targetPID -ne [uint32]$identity.process_id) { throw 'native element process changed' }
            $nativeResult = [IntPtr]::Zero
            $sent = [HermetrixDesktopNative]::SendMessageTimeout($targetHandle, 0xF5, [IntPtr]::Zero, [IntPtr]::Zero,
                2, 2000, [ref]$nativeResult)
            if ($sent -eq [IntPtr]::Zero) { throw 'native button action timed out or failed' }
        }
    } elseif ($request.action -eq 'set_value') {
        if ($actual.is_password) { throw 'password fields are unavailable to desktop set_value' }
        if ([string]$request.text -match '[\r\n]' -or ([string]$request.text).Length -gt 4096) { throw 'desktop text is invalid or too long' }
        try {
            $pattern = [System.Windows.Automation.ValuePattern]$target.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
            if ($pattern.Current.IsReadOnly) { throw 'desktop element is read-only' }
            $pattern.SetValue([string]$request.text)
        } catch {
            if ($actual.handle -eq 0) { throw 'value pattern is unavailable and the element has no native handle' }
            $targetHandle = [IntPtr]([int64]$actual.handle)
            $targetPID = [uint32]0
            [void][HermetrixDesktopNative]::GetWindowThreadProcessId($targetHandle, [ref]$targetPID)
            if ($targetPID -ne [uint32]$identity.process_id) { throw 'native element process changed' }
            $className = New-Object System.Text.StringBuilder 256
            [void][HermetrixDesktopNative]::GetClassName($targetHandle, $className, 256)
            if ($className.ToString() -notmatch '(^|\.)EDIT(\.|$)') { throw 'native element is not an edit control' }
            $nativeResult = [IntPtr]::Zero
            $sent = [HermetrixDesktopNative]::SendMessageTimeoutText($targetHandle, 0x000C, [IntPtr]::Zero,
                [string]$request.text, 2, 2000, [ref]$nativeResult)
            if ($sent -eq [IntPtr]::Zero) { throw 'native edit action timed out or failed' }
        }
    } else { throw 'unsupported desktop action' }
    @{ action = [string]$request.action; element_ref = $ref; window = $identity } | ConvertTo-Json -Compress -Depth 8
    exit 0
}
$screenshot = ''
$screenshot_error = ''
try {
    $bounds = $identity.bounds
    if ($bounds.width -lt 1 -or $bounds.height -lt 1 -or $bounds.width -gt 4096 -or $bounds.height -gt 4096) { throw 'window screenshot exceeds bounds' }
    $bitmap = New-Object System.Drawing.Bitmap($bounds.width, $bounds.height)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    try {
        $hdc = $graphics.GetHdc()
        try { $printed = [HermetrixDesktopNative]::PrintWindow([IntPtr]([int64]$identity.handle), $hdc, 2) }
        finally { $graphics.ReleaseHdc($hdc) }
        if (-not $printed) { $graphics.CopyFromScreen($bounds.x, $bounds.y, 0, 0, $bitmap.Size) }
        $bitmap.Save([string]$request.screenshot_path, [System.Drawing.Imaging.ImageFormat]::Png)
        $screenshot = [string]$request.screenshot_path
    } finally { $graphics.Dispose(); $bitmap.Dispose() }
} catch { $screenshot_error = SafeText $_.Exception.Message }
@{ window = $identity; elements = $elements; screenshot_path = $screenshot; screenshot_error = $screenshot_error } | ConvertTo-Json -Compress -Depth 8

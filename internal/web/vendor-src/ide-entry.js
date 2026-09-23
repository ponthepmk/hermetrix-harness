import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { basicSetup } from "codemirror";
import { EditorState, EditorSelection, StateEffect, StateField, RangeSet, Compartment } from "@codemirror/state";
import { EditorView, keymap, gutter, GutterMarker, Decoration } from "@codemirror/view";
import { indentWithTab, isolateHistory } from "@codemirror/commands";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags } from "@lezer/highlight";
import { javascript } from "@codemirror/lang-javascript";
import { go } from "@codemirror/lang-go";
import { python } from "@codemirror/lang-python";
import { markdown } from "@codemirror/lang-markdown";
import { json } from "@codemirror/lang-json";
import { html } from "@codemirror/lang-html";
import { css } from "@codemirror/lang-css";

function languageFor(path) {
  const extension = String(path || "").split(".").pop().toLowerCase();
  if (["js", "jsx", "mjs", "cjs", "ts", "tsx"].includes(extension)) return javascript({ jsx: extension.includes("x"), typescript: extension.startsWith("t") });
  if (extension === "go") return go();
  if (extension === "py") return python();
  if (["md", "markdown"].includes(extension)) return markdown();
  if (["json", "jsonl"].includes(extension)) return json();
  if (["html", "htm", "svg", "xml"].includes(extension)) return html();
  if (["css", "scss", "less"].includes(extension)) return css();
  return [];
}

const hermetrixHighlightStyle = HighlightStyle.define([
  { tag: tags.keyword, color: "#c792ea" },
  { tag: [tags.name, tags.deleted, tags.character, tags.propertyName, tags.macroName], color: "#d6d6d6" },
  { tag: [tags.function(tags.variableName), tags.labelName], color: "#82aaff" },
  { tag: [tags.typeName, tags.className, tags.namespace], color: "#ffcb6b" },
  { tag: [tags.string, tags.special(tags.string)], color: "#c3e88d" },
  { tag: [tags.number, tags.bool, tags.null], color: "#f78c6c" },
  { tag: [tags.comment, tags.meta], color: "#7f8c98", fontStyle: "italic" },
  { tag: [tags.operator, tags.punctuation, tags.bracket], color: "#89ddff" },
  { tag: [tags.heading, tags.strong], color: "#75d5d0", fontWeight: "600" },
  { tag: tags.link, color: "#82aaff", textDecoration: "underline" },
  { tag: tags.invalid, color: "#ff5370" }
]);

export function createTerminal(parent, options = {}) {
  const terminal = new Terminal({
    cursorBlink: true,
    cursorStyle: "bar",
    convertEol: false,
    scrollback: 10000,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
    fontSize: 13,
    lineHeight: 1.28,
    theme: {
      background: "#101010", foreground: "#d6d6d6", cursor: "#75d5d0",
      cursorAccent: "#101010", selectionBackground: "#315e5c99",
      black: "#1a1a1a", red: "#e06c75", green: "#98c379", yellow: "#e5c07b",
      blue: "#61afef", magenta: "#c678dd", cyan: "#56b6c2", white: "#d7dae0",
      brightBlack: "#6b6b6b", brightRed: "#ff7b86", brightGreen: "#b3dd8c",
      brightYellow: "#f2d28b", brightBlue: "#75bdff", brightMagenta: "#d995ed",
      brightCyan: "#70d5df", brightWhite: "#ffffff"
    }
  });
  const fit = new FitAddon();
  terminal.loadAddon(fit);
  terminal.open(parent);
  const data = terminal.onData(value => options.onData?.(value));
  let lastSize = "";
  const resize = new ResizeObserver(() => {
    try {
      fit.fit();
      const size = `${terminal.cols}x${terminal.rows}`;
      if (size !== lastSize) {
        lastSize = size;
        options.onResize?.(terminal.cols, terminal.rows);
      }
    } catch {}
  });
  resize.observe(parent);
  requestAnimationFrame(() => { try { fit.fit(); terminal.focus(); } catch {} });
  return {
    write: value => terminal.write(value),
    reset: () => terminal.reset(),
    focus: () => terminal.focus(),
    dispose: () => { resize.disconnect(); data.dispose(); terminal.dispose(); }
  };
}

export function createEditor(parent, options = {}) {
  const onSave = () => { options.onSave?.(); return true; };
  const wrapping = new Compartment();
  const updateBreakpoints = StateEffect.define();
  const updateExecutionLine = StateEffect.define();
  const breakpointPositions = StateField.define({
    create: () => [],
    update(positions, transaction) {
      let next = positions.map(position => transaction.newDoc.lineAt(transaction.changes.mapPos(position, 1)).from);
      for (const effect of transaction.effects) {
        if (effect.is(updateBreakpoints)) next = effect.value.filter(line => Number.isInteger(line) && line >= 1 && line <= transaction.newDoc.lines).map(line => transaction.newDoc.line(line).from);
      }
      return [...new Set(next)].sort((left, right) => left - right);
    }
  });
  const executionLine = StateField.define({
    create: () => Decoration.none,
    update(value, transaction) {
      let next = value.map(transaction.changes);
      for (const effect of transaction.effects) {
        if (effect.is(updateExecutionLine)) {
          const line = effect.value;
          next = Number.isInteger(line) && line >= 1 && line <= transaction.newDoc.lines
            ? Decoration.set([Decoration.line({class:"cm-execution-line"}).range(transaction.newDoc.line(line).from)])
            : Decoration.none;
        }
      }
      return next;
    },
    provide: field => EditorView.decorations.from(field)
  });
  class BreakpointMarker extends GutterMarker {
    toDOM() {
      const marker = parent.ownerDocument.createElement("span");
      marker.textContent = "●";
      marker.title = "Breakpoint — click to remove";
      marker.setAttribute("aria-label", "Breakpoint");
      return marker;
    }
  }
  const breakpointMarker = new BreakpointMarker();
  const view = new EditorView({
    doc: options.doc || "",
    parent,
    extensions: [
      basicSetup,
      EditorView.cspNonce.of(parent.ownerDocument.querySelector('meta[name="hermetrix-style-nonce"]')?.content || ""),
      EditorView.contentAttributes.of({"aria-label":`Code editor: ${options.path || "untitled"}`, spellcheck:"false"}),
      breakpointPositions,
      executionLine,
      gutter({
        class:"cm-breakpoint-gutter",
        renderEmptyElements:true,
        initialSpacer:() => breakpointMarker,
        markers:editor => RangeSet.of(editor.state.field(breakpointPositions).map(position => breakpointMarker.range(position))),
        domEventHandlers:{mousedown(editor, line, event) {
          if (event.button !== 0) return false;
          const number = editor.state.doc.lineAt(line.from).number;
          const lines = editor.state.field(breakpointPositions).map(position => editor.state.doc.lineAt(position).number);
          const enabled = !lines.includes(number);
          editor.dispatch({effects:updateBreakpoints.of(enabled ? [...lines, number] : lines.filter(item => item !== number))});
          options.onBreakpoint?.(number, enabled);
          return true;
        }}
      }),
      wrapping.of(options.wrap ? EditorView.lineWrapping : []),
      keymap.of([{ key: "Mod-s", run: onSave }, indentWithTab]),
      EditorState.tabSize.of(2),
      languageFor(options.path),
      syntaxHighlighting(hermetrixHighlightStyle),
      EditorView.updateListener.of(update => {
        if (update.docChanged) options.onChange?.(update.state.doc.toString());
        if (update.docChanged || update.selectionSet) {
          const line = update.state.doc.lineAt(update.state.selection.main.head);
          options.onCursor?.(line.number, update.state.selection.main.head - line.from + 1);
        }
      }),
      EditorView.theme({
        "&": { height: "100%", backgroundColor: "#141414", color: "#d6d6d6" },
        ".cm-scroller": { overflow: "auto", fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace", fontSize: "13px", lineHeight: "1.65" },
        ".cm-content": { padding: "12px 0", caretColor: "#75d5d0" },
        ".cm-gutters": { backgroundColor: "#141414", color: "#626262", border: "none" },
        ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "#202020" },
        ".cm-selectionBackground, ::selection": { backgroundColor: "#315e5c99 !important" },
        ".cm-cursor": { borderLeftColor: "#75d5d0" },
        ".cm-panels": { backgroundColor: "#202020", color: "#d6d6d6" },
        ".cm-tooltip": { backgroundColor: "#242424", color: "#d6d6d6", border: "1px solid #383838" },
        ".cm-breakpoint-gutter": { width:"20px", color:"#ff6b73", cursor:"pointer" },
        ".cm-breakpoint-gutter .cm-gutterElement": { padding:"0 4px", textAlign:"center" },
        ".cm-execution-line": { backgroundColor:"#494020 !important" }
      }, { dark: true })
    ]
  });
  return {
    getValue: () => view.state.doc.toString(),
    setWrap: enabled => view.dispatch({effects:wrapping.reconfigure(enabled ? EditorView.lineWrapping : [])}),
    setValue: content => {
      const next = String(content ?? "");
      if (next === view.state.doc.toString()) return false;
      const selection = EditorSelection.create(view.state.selection.ranges.map(range => EditorSelection.range(
        Math.min(range.anchor, next.length), Math.min(range.head, next.length))), view.state.selection.mainIndex);
      const breakpoints = view.state.field(breakpointPositions).map(position => view.state.doc.lineAt(position).number);
      const execution = view.state.field(executionLine).iter();
      const executing = execution.value ? view.state.doc.lineAt(execution.from).number : null;
      view.dispatch({changes:{from:0,to:view.state.doc.length,insert:next},selection,
        effects:[updateBreakpoints.of(breakpoints),updateExecutionLine.of(executing)],
        annotations:isolateHistory.of("full"),userEvent:"input.format"});
      return true;
    },
    getSelection: () => {
      const {from,to} = view.state.selection.main;
      return {text:view.state.sliceDoc(from,to),fromLine:view.state.doc.lineAt(from).number,
        toLine:view.state.doc.lineAt(to > from ? to - 1 : to).number};
    },
    setBreakpoints: lines => view.dispatch({effects:updateBreakpoints.of(Array.isArray(lines) ? lines.map(Number) : [])}),
    getBreakpoints: () => view.state.field(breakpointPositions).map(position => view.state.doc.lineAt(position).number),
    setExecutionLine: line => {
      const number = Number(line);
      const valid = Number.isInteger(number) && number >= 1 && number <= view.state.doc.lines;
      view.dispatch({effects:[updateExecutionLine.of(valid ? number : null),
        ...(valid ? [EditorView.scrollIntoView(view.state.doc.line(number).from,{y:"center"})] : [])]});
    },
    goToLine: number => {
      const line = view.state.doc.line(Math.max(1, Math.min(view.state.doc.lines, Number(number) || 1)));
      view.dispatch({ selection: { anchor: line.from }, scrollIntoView: true });
      view.focus();
    },
    focus: () => view.focus(),
    dispose: () => view.destroy()
  };
}

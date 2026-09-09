import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { basicSetup } from "codemirror";
import { EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
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
  const view = new EditorView({
    doc: options.doc || "",
    parent,
    extensions: [
      basicSetup,
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
        ".cm-tooltip": { backgroundColor: "#242424", color: "#d6d6d6", border: "1px solid #383838" }
      }, { dark: true })
    ]
  });
  return {
    getValue: () => view.state.doc.toString(),
    goToLine: number => {
      const line = view.state.doc.line(Math.max(1, Math.min(view.state.doc.lines, Number(number) || 1)));
      view.dispatch({ selection: { anchor: line.from }, scrollIntoView: true });
      view.focus();
    },
    focus: () => view.focus(),
    dispose: () => view.destroy()
  };
}

import { format as prettierFormat } from "prettier/standalone";
import * as babel from "prettier/plugins/babel";
import * as estree from "prettier/plugins/estree";
import * as typescript from "prettier/plugins/typescript";
import * as html from "prettier/plugins/html";
import * as postcss from "prettier/plugins/postcss";
import * as markdown from "prettier/plugins/markdown";
import * as yaml from "prettier/plugins/yaml";

const parsers = {
  js:"babel", jsx:"babel", mjs:"babel", cjs:"babel",
  ts:"typescript", tsx:"typescript", mts:"typescript", cts:"typescript",
  json:"json", jsonc:"json", json5:"json5",
  html:"html", htm:"html", vue:"vue", angular:"angular",
  css:"css", scss:"scss", less:"less",
  md:"markdown", markdown:"markdown", mdx:"mdx", yaml:"yaml", yml:"yaml"
};

export async function format(content, path) {
  const extension = String(path || "").split(".").pop().toLowerCase();
  const parser = parsers[extension];
  if (!parser) throw new Error(`Formatting is not available for .${extension || "unknown"} files.`);
  return prettierFormat(String(content ?? ""), {parser, filepath:path,
    plugins:[babel,estree,typescript,html,postcss,markdown,yaml],tabWidth:2,useTabs:false,endOfLine:"lf"});
}

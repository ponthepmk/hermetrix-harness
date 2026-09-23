import { build } from "esbuild";

await Promise.all([
  ["ide-entry.js", "HermetrixIDE", "ide.js"],
  ["formatter-entry.js", "HermetrixFormatter", "formatter.js"]
].map(([entry, name, output]) => build({entryPoints:[entry],bundle:true,minify:true,
  format:"iife",globalName:name,outfile:`../ui/vendor/${output}`,logLevel:"info"})));

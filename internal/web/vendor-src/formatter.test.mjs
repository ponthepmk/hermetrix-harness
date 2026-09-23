import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import {test} from "node:test";
import vm from "node:vm";

// Exercise the shipped browser bundle, not a different Node formatter entry.
const context = vm.createContext({TextEncoder,TextDecoder,URL});
vm.runInContext(await readFile(new URL("../ui/vendor/formatter.js",import.meta.url),"utf8"),context);
const format = context.HermetrixFormatter.format;

for (const [path,input,expected] of [
  ["app.js","const result={name:'Hermetrix',count:1};","const result = { name: \"Hermetrix\", count: 1 };\n"],
  ["app.ts","const count:number=1","const count: number = 1;\n"],
  ["data.json",'{"enabled":true,"count":2}', '{ "enabled": true, "count": 2 }\n'],
  ["index.html","<div><span>Hello</span></div>","<div><span>Hello</span></div>\n"],
  ["style.css",".item{color:red}",".item {\n  color: red;\n}\n"],
  ["README.md","# Title\n\n-   first\n-  second","# Title\n\n- first\n- second\n"],
  ["config.yml","items:\n    - first\n    - second","items:\n  - first\n  - second\n"]
]) {
  test(`browser formatter formats ${path} and is idempotent`,async () => {
    const output = await format(input,path);
    assert.equal(output,expected);
    assert.equal(await format(output,path),output);
  });
}

test("formatter rejects invalid input instead of returning damaged code",async () => {
  await assert.rejects(format("const =", "app.js"));
  await assert.rejects(format("package main", "main.go"),/not available/);
});

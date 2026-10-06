// Builds the app shell: compiles the JSX in ui/src/index.html and writes the
// embeddable ui/index.html.
//
// Why this exists
// ---------------
// The shell used to be authored as JSX inside a <script type="text/babel"> and
// compiled in the browser by Babel standalone, fetched from a CDN. Measured on
// this machine, that was the single largest cost in starting the app:
//
//   <script src="https://unpkg.com/@babel/standalone/babel.min.js">  3.7s cold
//   Babel transpiling 432 KB of JSX, main thread, every page load    ~6.6s
//
// So DOMContentLoaded sat at about 6.9 seconds even with Babel already in the
// browser cache -- the transpile is not a download cost, it is CPU paid on every
// single page load. The app was paying for it to produce JavaScript that a build
// step can produce once.
//
// esbuild does the same transform in about a second, and does it at build time.
//
// What is committed
// -----------------
// ui/index.html is generated and committed, so `go build` still needs nothing but
// Go. Node is required only to regenerate it, which is what `make ui` is for.
// Editing the app means editing ui/src/index.html and re-running that.
//
// Checks that fail the build, rather than passing quietly:
//   - the output still contains a text/babel block
//   - the output still references unpkg.com
//   - the output references any external http(s) script or stylesheet
//
// The last one is the reason this file is worth having. Both of this app's
// original external dependencies were added without anything noticing that they
// were external, and each one silently put the hotel's network on the critical
// path of first paint. Making that a build error means it cannot happen a third
// time by accident.

import { readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const SRC = join(here, 'src', 'index.html');
const OUT = join(here, 'index.html');

// Pinned so the output does not depend on whatever esbuild is current. Bump it
// deliberately, and the diff of ui/index.html shows what moved.
const ESBUILD = 'esbuild@0.24.2';

const BABEL_BLOCK = /<script type="text\/babel">([\s\S]*?)<\/script>/;

const src = readFileSync(SRC, 'utf8');

const match = src.match(BABEL_BLOCK);
if (!match) {
  console.error('build: no <script type="text/babel"> block in ' + SRC);
  console.error('       That block is the input to this script. If it has moved or');
  console.error('       been renamed, update BABEL_BLOCK to match it.');
  process.exit(1);
}

const jsx = match[1];
console.log(`build: ${jsx.length.toLocaleString()} bytes of JSX`);

// esbuild reads the JSX on stdin. --format=iife keeps the compiled output in its
// own scope: the block defines React components and helpers that nothing outside
// it needs, and a classic script would publish every one of them as a global.
// --minify because the output is generated and never hand-edited, so there is
// nothing to lose and roughly 130 KB of transfer to gain.
//
// npx is invoked through cmd.exe on Windows rather than with execFileSync's shell
// option: a .cmd cannot be spawned directly (EINVAL), and shell:true is
// deprecated precisely because it concatenates arguments into a command line.
// Going through cmd.exe explicitly keeps the same behaviour with neither problem.
const esbuildArgs = ['--yes', ESBUILD,
  '--loader=jsx',
  '--jsx=transform',
  '--jsx-factory=React.createElement',
  '--jsx-fragment=React.Fragment',
  '--target=chrome109',
  '--minify',
  '--log-level=warning',
];

let compiled;
try {
  compiled = process.platform === 'win32'
    ? execFileSync('cmd.exe', ['/d', '/s', '/c', 'npx', ...esbuildArgs],
        { input: jsx, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 })
    : execFileSync('npx', esbuildArgs,
        { input: jsx, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
} catch (err) {
  console.error('build: esbuild failed.');
  console.error(String(err.stderr || err.message));
  process.exit(1);
}

// A literal </script> inside the compiled JavaScript would close the tag early and
// turn the rest of it into markup. Minified output is unlikely to contain one,
// but it costs nothing to be certain and the failure would be silent and baffling.
const safe = compiled.replace(/<\/script/gi, '<\\/script');

// A JavaScript comment, not an HTML one. This text goes inside a <script>, and
// <!-- there is not a comment to a JS parser -- it is an Annex B HTML-like
// comment that ends at the newline, so every following line is parsed as code.
// The second line of an HTML comment banner is therefore not a comment at all:
// "Source: ... -- edit that ..." parses as a labelled statement followed by a
// decrement, and the build produced a file that died with
// "SyntaxError: Unexpected identifier 'edit'".
const banner = `/* GENERATED FILE -- do not edit.
   Source: cmd/api/ui/src/index.html -- edit that, then run \`make ui\`.
   The JSX below is compiled by cmd/api/ui/build.mjs with esbuild. */
`;

let out = src.replace(BABEL_BLOCK, () => '<script>\n' + banner + safe + '\n</script>');

const problems = [];
if (/text="text\/babel"/.test(out)) problems.push('output still contains a text/babel block');
if (/unpkg\.com/.test(out)) problems.push('output still references unpkg.com');
const external = out.match(/<(?:script|link)[^>]+(?:src|href)="https?:\/\/[^"]+"/gi);
if (external) problems.push('output still loads from an external host: ' + external.join(' '));

if (problems.length) {
  console.error('build: refusing to write ' + OUT);
  for (const p of problems) console.error('       - ' + p);
  process.exit(1);
}

writeFileSync(OUT, out, 'utf8');

const kb = n => (n / 1024).toFixed(1) + ' KB';
console.log(`build: wrote cmd/api/ui/index.html`);
console.log(`       source ${kb(src.length)}  ->  output ${kb(out.length)}`);
console.log(`       compiled JS ${kb(safe.length)}`);
console.log(`build: no external scripts or stylesheets. Good.`);
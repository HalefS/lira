// Builds the app shell: compiles the JSX under ui/src and writes the
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
// Where the source lives
// ----------------------
// One output file, many source files. ui/src/index.html is the shell (head,
// fonts, boot script, <div id="root">); everything else is a module:
//
//   ui/src/styles/fonts.css      the @font-face block
//   ui/src/styles/app.css        the application stylesheet
//   ui/src/app/*.jsx             one file per page or subsystem
//
// The JSX files are concatenated IN MANIFEST ORDER below and compiled as one
// unit. They therefore share a single scope, exactly as they did when they were
// one 21,000-line file: no imports, no exports, and no behavioural change from
// the split. Function declarations hoist, so a page may call a helper defined in
// a later file; top-level const initialisers, by contrast, run in order, which
// is why the manifest is ordered rather than alphabetical.
//
// Splitting the source is not cosmetic. A single 1.1 MB file is past the point
// where tooling gives up on it: modern-web-guidance's legacy-scan refuses it
// outright ("Target file exceeds maximum size"), and CodeGraph cannot index it,
// so every question about the UI had to be answered by grepping for line
// numbers. Per-file, both work.
//
// A later step could convert these to real ES modules with imports; the manifest
// is deliberately the only thing that would have to change.
//
// What is committed
// -----------------
// ui/index.html is generated and committed, so `go build` still needs nothing but
// Go. Node is required only to regenerate it, which is what `make ui` is for.
//
// Checks that fail the build, rather than passing quietly:
//   - a source file exists on disk but is missing from the manifest (it would
//     otherwise be silently left out of the bundle)
//   - a manifest entry has no file on disk
//   - the output still contains a text/babel block
//   - the output still references unpkg.com
//   - the output references any external http(s) script or stylesheet
//
// The last one is the reason this file is worth having. Both of this app's
// original external dependencies were added without anything noticing that they
// were external, and each one silently put the hotel's network on the critical
// path of first paint. Making that a build error means it cannot happen a third
// time by accident.

import { readFileSync, writeFileSync, readdirSync, existsSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const SRC = join(here, 'src', 'index.html');
const APP = join(here, 'src', 'app');
const OUT = join(here, 'index.html');

// Pinned so the output does not depend on whatever esbuild is current. Bump it
// deliberately, and the diff of ui/index.html shows what moved.
const ESBUILD = 'esbuild@0.24.2';

// Load order. Everything in src/app/*.jsx must appear here, exactly once, and the
// build fails if the directory and this list disagree in either direction.
const MANIFEST = [
  'prelude',            // React hooks, destructured once for the whole app
  'api',                // API config, offline store, request client, outbox
  'motion',             // GSAP helpers, CountUp, PageShell
  'paging',             // usePagedList and its footer
  'helpers',            // dates, storage, device identity, issue mapping
  'i18n',               // the English and Portuguese copy
  'icons',              // the shared icon set
  'ui',                 // Avatar, badges, TableScroll, Toast, toggles
  'auth',               // sign-in, idle sign-out
  'issue-utils',        // duplicate detection, duration maths
  'consumables-ui',     // consumable icons and the inline strips
  'dialogs',            // useDialog and the pickers that hang off the issue form
  'issue-modal',        // the issue form itself, photos, room picker
  'dashboard',
  'issues-table',
  'history',
  'alerts',
  'notifications',
  'support',
  'room-map',
  'analytics',
  'consumables-page',
  'team',
  'lcu',
  'maintenance',
  'reports',
  'profile',
  'system-status',
  'settings-shared',
  'rota-lib',
  'rota-picker',
  'rota-table',
  'rota-roster',
  'rota-shifts',
  'rota-absence',
  'schedule',
  'settings',
  'capture',
  'app',                // the root component and the mount
];

const die = msg => { console.error('build: ' + msg); process.exit(1); };
const kb = n => (n / 1024).toFixed(1) + ' KB';

// ── The manifest and the directory must agree ───────────────────────────────
const onDisk = readdirSync(APP).filter(f => f.endsWith('.jsx')).map(f => f.slice(0, -4));
const listed = new Set(MANIFEST);

const missing = MANIFEST.filter(n => !existsSync(join(APP, n + '.jsx')));
if (missing.length) die('listed in MANIFEST but not in src/app: ' + missing.join(', '));

const unlisted = onDisk.filter(n => !listed.has(n));
if (unlisted.length) die('in src/app but not in MANIFEST (they would be left out): ' + unlisted.join(', '));

if (new Set(MANIFEST).size !== MANIFEST.length) die('MANIFEST contains a duplicate entry');

// ── Assemble the JSX ────────────────────────────────────────────────────────
// Joined with '' because every file ends in a newline. Joining with '\n' as well
// would leave a blank line between files: harmless to the parser, but it makes
// line numbers in a stack trace drift away from the file they name.
const jsx = MANIFEST.map(n => readFileSync(join(APP, n + '.jsx'), 'utf8')).join('');
console.log(`build: ${MANIFEST.length} files, ${kb(jsx.length)} of JSX`);

// ── Expand the stylesheet includes in the shell ─────────────────────────────
// Only .css paths: "/* @include: app */" marks where the script goes and is
// handled below, and it would otherwise be read as a directory.
const INCLUDE = /\/\* @include: ((?:[\w-]+\/)+[\w-]+\.css) \*\//g;
let src = readFileSync(SRC, 'utf8');

let expanded = 0;
src = src.replace(INCLUDE, (_, rel) => {
  const file = join(here, 'src', rel);
  if (!existsSync(file)) die('shell includes a missing file: ' + rel);
  expanded++;
  // Each file ends in a newline so editors and git do not complain about a
  // missing one. The marker line already contributes the newline that follows
  // it, so the trailing one is dropped here; otherwise every include would add
  // a blank line to the output.
  return readFileSync(file, 'utf8').replace(/\r?\n$/, '');
});
if (!expanded) die('the shell includes no stylesheets; the @include markers are gone');

// The app itself is the one include the shell does not expand here: its position
// marks where the compiled <script> goes, and it is compiled below.
const BABEL_BLOCK = /<script type="text\/babel">([\s\S]*?)<\/script>/;
const match = src.match(BABEL_BLOCK);
if (!match) die('no <script type="text/babel"> block in ' + SRC);
if (!/@include: app\b/.test(match[1])) {
  die('the text/babel block no longer contains "/* @include: app */"; ' +
      'that marker is what tells this script where the app goes');
}

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
   Source: cmd/api/ui/src/index.html and src/app/*.jsx -- edit those, then run \`make ui\`.
   The JSX below is compiled by cmd/api/ui/build.mjs with esbuild. */
`;

const out = src.replace(BABEL_BLOCK, () => '<script>\n' + banner + safe + '\n</script>');

const problems = [];
if (/type="text\/babel"/.test(out)) problems.push('output still contains a text/babel block');
if (/unpkg\.com/.test(out)) problems.push('output still references unpkg.com');
const external = out.match(/<(?:script|link)[^>]+(?:src|href)="https?:\/\/[^"]+"/gi);
if (external) problems.push('output still loads from an external host: ' + external.join(' '));

if (problems.length) {
  console.error('build: refusing to write ' + OUT);
  for (const p of problems) console.error('       - ' + p);
  process.exit(1);
}

writeFileSync(OUT, out, 'utf8');

console.log(`build: wrote cmd/api/ui/index.html`);
console.log(`       source ${kb(src.length)}  ->  output ${kb(out.length)}`);
console.log(`       compiled JS ${kb(safe.length)}`);
console.log(`build: no external scripts or stylesheets. Good.`);
